// data_reader.go
package dbop

import (
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"net/http"
	"openapi-cms/models"
)

// handleGetData 统一处理获取不同类型数据的请求
func HandleGetData(db *Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从查询参数获取数据类型
		dataType := c.Query("type")

		switch dataType {
		case "knowledge_bases":
			getKnowledgeBases(c, db)
		case "other_data":
			getOtherData(c)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data type"})
		}
	}
}

// getKnowledgeBases 获取知识库数据
func getKnowledgeBases(c *gin.Context, db *Database) {
	query := "SELECT id, name,COALESCE(display_name,''), COALESCE(description,''), COALESCE(tags,''), created_at,model_owner,creator_id FROM vector_stores ORDER BY CASE WHEN model_owner = 'local' THEN 0 ELSE 1  END ASC, id ASC;"
	rows, err := db.Query(query)
	if err != nil {
		logrus.Printf("查询知识库失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
		return
	}
	defer rows.Close()

	var knowledgeBases []models.KnowledgeBase

	for rows.Next() {
		var kb models.KnowledgeBase
		if err := rows.Scan(&kb.ID, &kb.Name, &kb.DisplayName, &kb.Description, &kb.Tags, &kb.CreatedAt, &kb.ModelOwner, &kb.CreatorID); err != nil {
			logrus.Printf("扫描知识库数据失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		knowledgeBases = append(knowledgeBases, kb)
	}

	if err := rows.Err(); err != nil {
		logrus.Printf("遍历查询结果时出错: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, knowledgeBases)
}

// GetFilesByKnowledgeBaseID 返回处理知识库下文件查询的处理器
func GetFilesByKnowledgeBaseID(db *Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取知识库 ID
		knowledgeBaseID := c.Param("id")

		// SQL 查询，获取文件信息及其相关的向量存储信息
		query := `
			SELECT 
				uf.file_id,
			    uf.file_name, 
				uf.file_path, 
				uf.file_type, 
				COALESCE(uf.file_description, '') AS file_description, 
				uf.upload_time, 
				COALESCE(f.id, '') AS vector_file_id,  -- 如果没有数据则返回空字符串
				COALESCE(f.usage_bytes, 0) AS usage_bytes, 
				COALESCE(f.created_at, '') AS vector_file_created_at, 
				COALESCE(f.status, '') AS status  -- 如果没有状态则返回空字符串
			FROM uploaded_files uf
			LEFT JOIN fileKnowledgeRelations fkr ON uf.file_id = fkr.file_id  -- 通过 fileKnowledgeRelations 关联文件
			LEFT JOIN vector_stores vs ON fkr.knowledge_base_id = vs.id  -- 关联到 vector_stores 表
			LEFT JOIN files f ON uf.file_id = f.file_id  -- 使用 LEFT JOIN 获取可能为空的 files 表数据
			WHERE uf.file_type NOT LIKE 'image%' and vs.id = ?
		`

		rows, err := db.Query(query, knowledgeBaseID)
		if err != nil {
			logrus.Printf("查询知识库下文件失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}
		defer rows.Close()

		// 定义用于存储返回数据的结构体
		type FileInfo struct {
			FileId              string `json:"file_id"`
			FileName            string `json:"file_name"`
			FilePath            string `json:"file_path"`
			FileType            string `json:"file_type"`
			FileDescription     string `json:"file_description"`
			UploadTime          string `json:"upload_time"`
			VectorFileID        string `json:"vector_file_id"`
			UsageBytes          int    `json:"usage_bytes"`
			VectorFileCreatedAt string `json:"vector_file_created_at"`
			Status              string `json:"status"`
		}

		var files []FileInfo

		// 处理查询结果
		for rows.Next() {
			var file FileInfo
			if err := rows.Scan(&file.FileId, &file.FileName, &file.FilePath, &file.FileType, &file.FileDescription, &file.UploadTime, &file.VectorFileID, &file.UsageBytes, &file.VectorFileCreatedAt, &file.Status); err != nil {
				logrus.Printf("扫描文件数据失败: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
				return
			}
			files = append(files, file)
		}

		if err := rows.Err(); err != nil {
			logrus.Printf("遍历文件查询结果时出错: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "details": err.Error()})
			return
		}

		// 返回文件数据
		c.JSON(http.StatusOK, files)
	}
}

// getOtherData 获取其他数据
func getOtherData(c *gin.Context) {
	// 这里可以根据需要扩展其他数据库表的数据获取逻辑
	c.JSON(http.StatusOK, gin.H{"message": "其他数据获取接口"})
}
