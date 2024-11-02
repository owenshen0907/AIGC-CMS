// tool/uploads.go
package tool

import (
	"fmt"
	"github.com/joho/godotenv"
	"io"
	"net/http"
	"openapi-cms/dbop"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// 初始化加载环境变量（如果尚未在应用程序其他部分加载）
func init() {
	err := godotenv.Load()
	if err != nil {
		logrus.Warn("未能加载 .env 文件，使用系统环境变量")
	}
}

// HandleUploadFile 处理上传文件的请求
func HandleUploadFile(c *gin.Context, db *dbop.Database) {
	// 从表单中获取，知识库id vector_store_id
	vectorStoreID := c.PostForm("vector_store_id")
	if vectorStoreID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "vector_store_id is required"})
		return
	}

	// 从表单中获取 file_description
	fileDescription := c.PostForm("file_description")

	// 从表单中获取 model_owner
	modelOwner := c.PostForm("model_owner")
	if modelOwner == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_owner is required"})
		return
	}

	// 检查 model_owner 的值
	switch modelOwner {
	case "local", "stepfun":
		// 允许的 model_owner，继续处理
	default:
		// 不支持的 model_owner，返回错误
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("暂不支持 model_owner 为 '%s' 的知识库文件上传，待接入后再实现", modelOwner),
		})
		return
	}

	// 从表单中获取文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		logrus.Errorf("Error retrieving the file: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "File is required"})
		return
	}
	defer file.Close()
	// 从环境变量中读取文件保存路径
	uploadDir := os.Getenv("file_path")
	if uploadDir == "" {
		uploadDir = "./uploads" // 默认值（可选）
		logrus.Warn("环境变量 'file_path' 未设置，使用默认路径 './uploads'")
	}

	// 确保上传目录存在
	err = os.MkdirAll(uploadDir, os.ModePerm)
	if err != nil {
		logrus.Errorf("创建上传目录失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法创建上传目录"})
		return
	}
	// 使用 filepath.Join 组合文件路径，确保跨平台兼容性
	filePath := filepath.Join(uploadDir, header.Filename)
	out, err := os.Create(filePath)
	if err != nil {
		logrus.Errorf("Error saving the file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}
	defer out.Close()

	_, err = io.Copy(out, file)
	if err != nil {
		logrus.Errorf("Error writing the file to disk: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write file"})
		return
	}

	// 将文件信息存储到数据库中的 uploaded_files 表，生成唯一的 fileID
	fileID := uuid.New().String()
	fileType := header.Header.Get("Content-Type")
	// 开始数据库事务
	tx, err := db.BeginTransaction()
	if err != nil {
		logrus.WithError(err).Error("启动事务时出错")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库错误"})
		return
	}

	// 确保事务在函数结束时正确处理
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			logrus.Errorf("捕获到 panic: %v", p)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "内部服务器错误"})
		} else if err != nil {
			tx.Rollback()
		} else {
			err = db.CommitTransaction(tx)
			if err != nil {
				logrus.WithError(err).Error("提交事务时出错")
				c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库提交错误"})
			}
		}
	}()
	// 插入上传的文件信息到 uploaded_files 表中
	err = db.InsertUploadedFileTx(tx, fileID, header.Filename, filePath, fileType, fileDescription)
	if err != nil {
		logrus.Errorf("将上传文件记录插入数据库时出错: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法存储文件信息"})
		return
	}
	// 插入文件与知识库的关联关系到 fileKnowledgeRelations 表中
	err = db.InsertFileKnowledgeRelationTx(tx, fileID, vectorStoreID)
	if err != nil {
		logrus.Errorf("插入文件与知识库关联关系时出错: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法关联文件与知识库"})
		return
	}

	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"file_id":   fileID,
		"status":    "文件已成功保存并与知识库关联",
		"file_path": filePath,
	})
}
