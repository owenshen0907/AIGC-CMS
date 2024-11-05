// tool/knowledge-uploads.go
package tool

import (
	"fmt"
	"github.com/joho/godotenv"
	"io"
	"net/http"
	"openapi-cms/dbop"
	"openapi-cms/middleware"
	"os"
	"path/filepath"
	"time"

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
	file_web_host := os.Getenv("FILE_WEB_HOST")
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
	// 获取文件大小
	fileSize := header.Size
	defer file.Close()
	// 从上下文中获取用户名
	userName, exists := middleware.GetUserName(c)
	if !exists || userName == "" {
		logrus.Warn("未能获取用户名")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: username not found"})
		return
	}
	//判断是否文件已存在，如果已存在则直接返回已存在的文件信息，否则继续上传文件
	uploadedFile, err := db.GetUploadedFileByFileNameSizeUsername(header.Filename, userName, fileSize)
	if err != nil {
		logrus.Errorf("Error retrieving the file: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "File is required"})

	}
	if uploadedFile == nil {
		logrus.Info("文件不存在，继续处理上传")
	} else {
		// 返回成功响应
		logrus.Info("此文件该用户已经上传，直接使用历史文件。")
		file_web_path := fmt.Sprintf("%s%s", file_web_host, uploadedFile.FilePath)
		c.JSON(http.StatusOK, gin.H{
			"file_id":       uploadedFile.FileID,
			"status":        "文件已成功保存并与知识库关联",
			"file_path":     uploadedFile.FilePath,
			"file_web_path": file_web_path,
		})
		return
	}
	// 获取当前日期，格式为 YYYY-MM-DD
	currentDate := time.Now().Format("2006-01-02")

	// 从环境变量中读取文件保存路径
	uploadDir := os.Getenv("FILE_PATH")
	if uploadDir == "" {
		uploadDir = "./uploads" // 默认值（可选）
		logrus.Warn("环境变量 'FILE_PATH' 未设置，使用默认路径 './uploads'")
	}
	// 构建新的目标目录路径：uploadDir/username/currentDate
	targetDir := filepath.Join(uploadDir, userName, currentDate)

	// 确保上传目录存在
	err = os.MkdirAll(targetDir, os.ModePerm)
	if err != nil {
		logrus.Errorf("创建上传目录失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法创建上传目录"})
		return
	}
	// 确保文件名安全，防止路径遍历
	fileName := filepath.Base(header.Filename)
	filePath := filepath.Join(targetDir, fileName)

	out, err := os.Create(filePath)
	if err != nil {
		logrus.Errorf("Error creating the file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to creating file"})
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

	// 计算相对于 uploadDir 的相对路径，例如 "username/2024-04-27/filename.ext"
	relativeFilePath, err := filepath.Rel(uploadDir, filePath)
	if err != nil {
		logrus.Errorf("计算相对文件路径时出错: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "路径处理错误"})
		return
	}

	// 插入上传的文件信息到 uploaded_files 表中
	err = db.InsertUploadedFileTx(tx, fileID, fileName, relativeFilePath, fileType, fileDescription, userName, fileSize)
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

	file_web_path := fmt.Sprintf("%s%s", file_web_host, relativeFilePath)

	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"file_id":       fileID,
		"status":        "文件已成功保存并与知识库关联",
		"file_path":     relativeFilePath,
		"file_web_path": file_web_path,
	})
}
