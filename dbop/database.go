// database.go
package dbop

import (
	"database/sql"
	"fmt"
	"log"
	"openapi-cms/models"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

type Database struct {
	db                    *sql.DB
	insertVectorStoreStmt *sql.Stmt
	insertFileStmt        *sql.Stmt
}

// NewDatabase 初始化数据库连接
func NewDatabase() (*Database, error) {
	host := os.Getenv("DB_HOST")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	port := os.Getenv("DB_PORT")
	dbname := os.Getenv("DB_NAME")

	if host == "" || user == "" || password == "" || port == "" || dbname == "" {
		return nil, fmt.Errorf("database configuration is not set properly in environment variables")
	}

	// 确保数据库名称不包含非法字符
	dbname = strings.ReplaceAll(dbname, "-", "_")

	// 不带数据库名的 DSN
	dsnWithoutDB := fmt.Sprintf("%s:%s@tcp(%s:%s)/", user, password, host, port)

	// 连接数据库服务器
	db, err := sql.Open("mysql", dsnWithoutDB)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database server: %w", err)
	}
	defer db.Close()

	// 尝试创建数据库
	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", dbname))
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	// 使用带数据库名的 DSN 连接
	dsnWithDB := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true", user, password, host, port, dbname)

	dbWithDB, err := sql.Open("mysql", dsnWithDB)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 测试数据库连接
	if err = dbWithDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	database := &Database{db: dbWithDB}

	// 创建表
	if err := database.createTables(); err != nil {
		return nil, err
	}

	// 预准备语句
	insertStmt, err := database.db.Prepare("INSERT INTO vector_stores (id, name,display_name, description, tags,model_owner,creator_id) VALUES (?, ?,?, ?, ?, ?, ?)")
	if err != nil {
		return nil, fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	database.insertVectorStoreStmt = insertStmt
	// 准备插入文件的语句
	fileInsertStmt, err := database.db.Prepare("INSERT INTO files (id, vector_store_id, usage_bytes, file_id,status,purpose) VALUES (?, ?, ?, ?,?,?)")
	if err != nil {
		return nil, fmt.Errorf("failed to prepare file insert statement: %w", err)
	}
	database.insertFileStmt = fileInsertStmt

	return database, nil
}

// createTables 创建必要的数据库表
func (d *Database) createTables() error {
	createVectorStoresTable := `
	CREATE TABLE IF NOT EXISTS vector_stores (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,          -- 知识库标识，用于唯一标识知识库
    display_name VARCHAR(255) NOT NULL,  -- 知识库名称
    description TEXT,
    tags VARCHAR(255),
    model_owner VARCHAR(255) NOT NULL,   -- 归属模型
    creator_id VARCHAR(255) NOT NULL,    -- 创建人ID
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`
	_, err := d.db.Exec(createVectorStoresTable)
	if err != nil {
		return fmt.Errorf("failed to create table vector_stores: %w", err)
	}

	createUploadFilesTable := `
	CREATE TABLE IF NOT EXISTS uploaded_files (
		file_id VARCHAR(255) PRIMARY KEY,           -- 文件ID
		file_name VARCHAR(255) NOT NULL,            -- 文件名
		file_path VARCHAR(512) NOT NULL,            -- 存储路径
		file_type VARCHAR(50) NOT NULL,             -- 文件类型
		file_description TEXT,                      -- 文件描述
		upload_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP, -- 上传时间
	    status VARCHAR(50) DEFAULT 'uploaded'        -- 文件状态
	)`
	_, err = d.db.Exec(createUploadFilesTable)
	if err != nil {
		return fmt.Errorf("failed to create table files: %w", err)
	}

	createFileKnowledgeRelationsTable := `
	CREATE TABLE IF NOT EXISTS fileKnowledgeRelations (
    id INT AUTO_INCREMENT PRIMARY KEY,              -- 自动生成的自增主键
    file_id VARCHAR(255) NOT NULL,              -- 文件ID
    knowledge_base_id VARCHAR(255) NOT NULL,    -- 知识库ID
    FOREIGN KEY (file_id) REFERENCES uploaded_files(file_id) ON DELETE CASCADE,  -- 关联到上传的文件
    FOREIGN KEY (knowledge_base_id) REFERENCES vector_stores(id) ON DELETE CASCADE  -- 关联到知识库
)`
	_, err = d.db.Exec(createFileKnowledgeRelationsTable)
	if err != nil {
		return fmt.Errorf("failed to create table files: %w", err)
	}
	createFilesTable := `
	CREATE TABLE IF NOT EXISTS files (
		id VARCHAR(255) PRIMARY KEY,           -- 主键ID
		vector_store_id VARCHAR(255) NOT NULL, -- 向量存储ID
		usage_bytes INT NOT NULL,              -- 使用的字节数
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, -- 创建时间
		file_id VARCHAR(255),                  -- 与文件关联的ID
	    purpose VARCHAR(255) DEFAULT NULL,
		status VARCHAR(50) NOT NULL DEFAULT 'processing', -- 状态字段：处理中的状态
		FOREIGN KEY (file_id) REFERENCES uploaded_files(file_id) ON DELETE SET NULL, -- 外键关联 uploaded_files
		FOREIGN KEY (vector_store_id) REFERENCES vector_stores(id) ON DELETE CASCADE -- 外键关联 vector_stores
	)`
	_, err = d.db.Exec(createFilesTable)
	if err != nil {
		return fmt.Errorf("failed to create table files: %w", err)
	}

	return nil
}

// InsertVectorStore 插入 vector_store 记录
func (d *Database) InsertVectorStore(id, name, display_name, description, tags, model_owner, creator_id string) error {
	_, err := d.insertVectorStoreStmt.Exec(id, name, display_name, description, tags, model_owner, creator_id)
	if err != nil {
		return fmt.Errorf("failed to insert vector store: %w", err)
	}
	return nil
}

// GetKnowledgeBaseByID 获取指定 ID 的知识库记录
func (d *Database) GetKnowledgeBaseByID(id string) (*models.KnowledgeBase, error) {
	query := "SELECT id, name, display_name, description, tags, model_owner, created_at, creator_id FROM vector_stores WHERE id = ?"
	row := d.db.QueryRow(query, id)
	var kb models.KnowledgeBase
	if err := row.Scan(&kb.ID, &kb.Name, &kb.DisplayName, &kb.Description, &kb.Tags, &kb.ModelOwner, &kb.CreatedAt, &kb.CreatorID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // 未找到记录
		}
		return nil, err
	}
	return &kb, nil
}

// GetKnowledgeBaseByName 获取指定 name 的知识库记录
func (d *Database) GetKnowledgeBaseByName(name string) (*models.KnowledgeBase, error) {
	query := "SELECT id, name, display_name, description, tags, model_owner, created_at, creator_id FROM vector_stores WHERE name = ?"
	row := d.db.QueryRow(query, name)
	var kb models.KnowledgeBase
	if err := row.Scan(&kb.ID, &kb.Name, &kb.DisplayName, &kb.Description, &kb.Tags, &kb.ModelOwner, &kb.CreatedAt, &kb.CreatorID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // 未找到记录
		}
		return nil, err
	}
	return &kb, nil
}

// UpdateKnowledgeBaseByName 当知识库name一样时，表面是前端再重新发起请求，将有个可能更新的内容进行调整
func (d *Database) UpdateKnowledgeBaseByName(name, displayName, description, tags, modelOwner string) error {
	query := "UPDATE vector_stores SET display_name = ?, description = ?, tags = ?, model_owner = ? WHERE name = ?"
	_, err := d.db.Exec(query, displayName, description, tags, modelOwner, name)
	if err != nil {
		return fmt.Errorf("failed to update knowledge base by name: %w", err)
	}
	return nil
}

// UpdateKnowledgeBaseIDByName 更新指定 name 的知识库记录
func (d *Database) UpdateKnowledgeBaseIDByName(name string, id string) error {
	query := "UPDATE vector_stores SET id = ? WHERE name = ?"
	_, err := d.db.Exec(query, id, name)
	return err
}

// UpdateKnowledgeBase 更新指定 name 的知识库记录
func (d *Database) UpdateKnowledgeBase(name, displayName, description, tags string) error {
	query := "UPDATE vector_stores SET display_name = ?, description = ?, tags = ? WHERE name = ?"
	_, err := d.db.Exec(query, displayName, description, tags, name)
	return err
}

// InsertUploadedFileTx 在事务中向 uploaded_files 表插入一条记录
func (d *Database) InsertUploadedFileTx(tx *sql.Tx, fileID, fileName, filePath, fileType, fileDescription string) error {
	query := `
		INSERT INTO uploaded_files (file_id, file_name, file_path, file_type, file_description, upload_time, status)
		VALUES (?, ?, ?, ?, ?, NOW(), 'uploaded')
	`
	_, err := tx.Exec(query, fileID, fileName, filePath, fileType, fileDescription)
	if err != nil {
		return fmt.Errorf("InsertUploadedFileTx: %w", err)
	}
	return nil
}

// InsertFileKnowledgeRelationTx 在事务中向 fileKnowledgeRelations 表插入一条关联记录
func (d *Database) InsertFileKnowledgeRelationTx(tx *sql.Tx, fileID, knowledgeBaseID string) error {
	query := `
		INSERT INTO fileKnowledgeRelations (file_id, knowledge_base_id)
		VALUES (?, ?)
	`
	_, err := tx.Exec(query, fileID, knowledgeBaseID)
	if err != nil {
		return fmt.Errorf("InsertFileKnowledgeRelationTx: %w", err)
	}
	return nil
}

// GetUploadedFileByID 根据 fileID 获取上传文件记录
func (d *Database) GetUploadedFileByID(fileID string) (*models.UploadedFile, error) {
	query := "SELECT file_id, file_name, file_path,file_type FROM uploaded_files WHERE file_id = ?"
	row := d.db.QueryRow(query, fileID)
	var uf models.UploadedFile
	if err := row.Scan(&uf.FileID, &uf.Filename, &uf.FilePath, &uf.FileType); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // 未找到记录
		}
		return nil, err
	}
	return &uf, nil
}

// UpdateUploadedFileStatus 更新上传的文件状态status 状态：默认NULL，failed-上传到知识库处理失败，-completed已处理。success-知识库已向量化完成
func (d *Database) UpdateUploadedFileStatus(fileID, status string) error {
	query := "UPDATE uploaded_files SET status = ? WHERE file_id = ?"
	_, err := d.db.Exec(query, status, fileID)
	if err != nil {
		return fmt.Errorf("failed to update file status: %w", err)
	}
	return nil
}

// InsertFile 插入文件记录
func (d *Database) InsertFile(id, vectorStoreID string, usageBytes int, fileID, status, purpose string) error {
	//query := "INSERT INTO files (id, vector_store_id, usage_bytes, file_id) VALUES (?, ?, ?, ?)"
	_, err := d.insertFileStmt.Exec(id, vectorStoreID, usageBytes, fileID, status, purpose)
	if err != nil {
		return fmt.Errorf("failed to insert file: %w", err)
	}
	return nil
}

// Query 封装查询操作
func (db *Database) Query(query string, args ...interface{}) (*sql.Rows, error) {
	if db.db == nil {
		log.Println("数据库连接未初始化")
		return nil, sql.ErrConnDone
	}
	return db.db.Query(query, args...)
}

// Close 关闭数据库连接
func (d *Database) Close() error {
	if d.insertVectorStoreStmt != nil {
		d.insertVectorStoreStmt.Close()
	}
	return d.db.Close()
}

// BeginTransaction 开始一个事务
func (d *Database) BeginTransaction() (*sql.Tx, error) {
	if d.db == nil {
		return nil, fmt.Errorf("database connection is not initialized")
	}
	return d.db.Begin()
}

// CommitTransaction 提交事务
func (d *Database) CommitTransaction(tx *sql.Tx) error {
	if tx == nil {
		return fmt.Errorf("transaction is nil")
	}
	return tx.Commit()
}

// RollbackTransaction 回滚事务
func (d *Database) RollbackTransaction(tx *sql.Tx) error {
	if tx == nil {
		return fmt.Errorf("transaction is nil")
	}
	return tx.Rollback()
}
