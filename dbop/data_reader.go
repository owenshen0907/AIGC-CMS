// data_reader.go
package dbop

import (
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"net/http"
	"openapi-cms/models"
)

// KnowledgeBase 定义知识库结构体
//
//	type KnowledgeBase struct {
//		ID          string `json:"id"`
//		Name        string `json:"name"`         // 知识库标识
//		DisplayName string `json:"display_name"` // 知识库名称
//		Description string `json:"description"`
//		Tags        string `json:"tags"`
//		CreatedAt   string `json:"created_at"`
//		model_owner string `json:"model_owner"` //归属模型：stepfun，zhipu,moonshot,baichuan
//		creator_id  string `json:"creator_id"`
//	}

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
	query := "SELECT id, name,COALESCE(display_name,''), COALESCE(description,''), COALESCE(tags,''), created_at,model_owner,creator_id FROM vector_stores"
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

// getOtherData 获取其他数据
func getOtherData(c *gin.Context) {
	// 这里可以根据需要扩展其他数据库表的数据获取逻辑
	c.JSON(http.StatusOK, gin.H{"message": "其他数据获取接口"})
}
