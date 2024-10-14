// database.go
package main

import (
	"database/sql"
	"fmt"
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
	insertStmt, err := database.db.Prepare("INSERT INTO vector_stores (id, name,display_name, description, tags) VALUES (?, ?,?, ?, ?)")
	if err != nil {
		return nil, fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	database.insertVectorStoreStmt = insertStmt
	// 准备插入文件的语句
	fileInsertStmt, err := database.db.Prepare("INSERT INTO files (id, vector_store_id, usage_bytes) VALUES (?, ?, ?)")
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
    name VARCHAR(255) NOT NULL,          -- 知识库标识
    display_name VARCHAR(255) NOT NULL,  -- 知识库名称
    description TEXT,
    tags VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`
	_, err := d.db.Exec(createVectorStoresTable)
	if err != nil {
		return fmt.Errorf("failed to create table vector_stores: %w", err)
	}

	createFilesTable := `
	CREATE TABLE IF NOT EXISTS files (
		id VARCHAR(255) PRIMARY KEY,
		vector_store_id VARCHAR(255) NOT NULL,
		usage_bytes INT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (vector_store_id) REFERENCES vector_stores(id) ON DELETE CASCADE
	)`
	_, err = d.db.Exec(createFilesTable)
	if err != nil {
		return fmt.Errorf("failed to create table files: %w", err)
	}

	return nil
}

// InsertVectorStore 插入 vector_store 记录
func (d *Database) InsertVectorStore(id, name, display_name, description, tags string) error {
	_, err := d.insertVectorStoreStmt.Exec(id, name, display_name, description, tags)
	if err != nil {
		return fmt.Errorf("failed to insert vector store: %w", err)
	}
	return nil
}

// InsertFile 插入文件记录
func (d *Database) InsertFile(id, vectorStoreID string, usageBytes int) error {
	_, err := d.insertFileStmt.Exec(id, vectorStoreID, usageBytes)
	if err != nil {
		return fmt.Errorf("failed to insert file: %w", err)
	}
	return nil
}

// Close 关闭数据库连接
func (d *Database) Close() error {
	if d.insertVectorStoreStmt != nil {
		d.insertVectorStoreStmt.Close()
	}
	return d.db.Close()
}
