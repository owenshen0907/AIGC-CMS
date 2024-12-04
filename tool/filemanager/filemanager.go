// tool/filemanager/filemanager.go

package filemanager

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// FileManager 结构体封装 MinIO 客户端和桶名称
type FileManager struct {
	Client     *minio.Client
	BucketName string
}

// NewFileManager 初始化并返回一个 FileManager 实例
func NewFileManager() (*FileManager, error) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	accessKeyID := os.Getenv("MINIO_ACCESS_KEY")
	secretAccessKey := os.Getenv("MINIO_SECRET_KEY")
	useSSL := os.Getenv("MINIO_USE_SSL") == "true"
	bucketName := os.Getenv("MINIO_BUCKET")
	// 添加日志以验证 endpoint
	log.Printf("Initializing MinIO client with endpoint: %s", endpoint)
	log.Printf("MinIO Use SSL: %v", useSSL)
	log.Printf("MinIO Bucket: %s", bucketName)

	// 创建 MinIO 客户端
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %v", err)
	}

	// 检查桶是否存在，如果不存在则创建
	ctx := context.Background()
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check if bucket exists: %v", err)
	}
	if !exists {
		err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket: %v", err)
		}
		log.Printf("Bucket %s created successfully", bucketName)
	}

	return &FileManager{
		Client:     minioClient,
		BucketName: bucketName,
	}, nil
}

// UploadFile 上传文件到 MinIO，存储路径为 username/YYYY-MM-DD/uuid_filename
func (fm *FileManager) UploadFile(c *gin.Context) {
	// 从请求中获取文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未接收到文件"})
		return
	}
	defer file.Close()

	// 从上下文中获取用户名
	username, exists := c.Get("userName")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证的用户"})
		return
	}
	usernameStr, ok := username.(string)
	if !ok || usernameStr == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取用户名失败"})
		return
	}

	// 获取当前日期
	currentDate := time.Now().Format("2006-01-02")

	// 生成唯一的文件名，避免冲突
	uniqueID := uuid.New().String()
	cleanFilename := filepath.Base(header.Filename) // 防止路径遍历
	objectName := filepath.Join(usernameStr, currentDate, fmt.Sprintf("%s_%s", uniqueID, cleanFilename))

	// 获取文件大小和类型
	fileSize := header.Size
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 上传文件
	_, err = fm.Client.PutObject(context.Background(), fm.BucketName, objectName, file, fileSize, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		log.Printf("上传文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "上传文件失败"})
		return
	}

	log.Printf("用户 %s 上传了文件 %s 于 %s", usernameStr, objectName, time.Now().Format(time.RFC3339))

	c.JSON(http.StatusOK, gin.H{"message": "文件上传成功", "filePath": objectName})
}

// DownloadFile 从 MinIO 下载文件
func (fm *FileManager) DownloadFile(c *gin.Context) {
	// 捕获通配符参数，并去除前导斜杠
	objectPath := strings.TrimPrefix(c.Param("filename"), "/")
	log.Printf("Download request for objectPath: %s", objectPath)

	if objectPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件名为空"})
		return
	}

	// 防止路径遍历攻击
	if strings.Contains(objectPath, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的文件路径"})
		return
	}

	// 从上下文中获取用户名
	username, exists := c.Get("userName")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证的用户"})
		return
	}
	usernameStr, ok := username.(string)
	if !ok || usernameStr == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取用户名失败"})
		return
	}

	// 构建完整的文件路径
	fullPath := filepath.Join(usernameStr, objectPath)
	log.Printf("Full object path: %s", fullPath)

	// 获取对象
	object, err := fm.Client.GetObject(context.Background(), fm.BucketName, fullPath, minio.GetObjectOptions{})
	if err != nil {
		log.Printf("获取文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取文件失败"})
		return
	}
	defer object.Close()

	// 获取文件信息以设置响应头
	info, err := object.Stat()
	if err != nil {
		log.Printf("获取文件信息失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取文件信息失败"})
		return
	}

	// 设置响应头
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(fullPath)))
	c.Header("Content-Type", info.ContentType)
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size))

	// 将文件内容写入响应
	if _, err := io.Copy(c.Writer, object); err != nil {
		log.Printf("发送文件失败: %v", err)
		// 注意：此处不再尝试发送 JSON 响应，因为已经开始写入文件内容
		return
	}

	log.Printf("用户 %s 下载了文件 %s 于 %s", usernameStr, fullPath, time.Now().Format(time.RFC3339))
}

// DeleteFile 从 MinIO 删除文件
func (fm *FileManager) DeleteFile(c *gin.Context) {
	// 捕获通配符参数，并去除前导斜杠
	objectPath := strings.TrimPrefix(c.Param("filename"), "/")
	log.Printf("Delete request for objectPath: %s", objectPath)

	if objectPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件名为空"})
		return
	}

	// 防止路径遍历攻击
	if strings.Contains(objectPath, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的文件路径"})
		return
	}

	// 从上下文中获取用户名
	username, exists := c.Get("userName")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证的用户"})
		return
	}
	usernameStr, ok := username.(string)
	if !ok || usernameStr == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取用户名失败"})
		return
	}

	// 构建完整的文件路径
	fullPath := filepath.Join(usernameStr, objectPath)
	log.Printf("Full object path: %s", fullPath)

	// 删除对象
	err := fm.Client.RemoveObject(context.Background(), fm.BucketName, fullPath, minio.RemoveObjectOptions{})
	if err != nil {
		log.Printf("删除文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除文件失败"})
		return
	}

	log.Printf("用户 %s 删除了文件 %s 于 %s", usernameStr, fullPath, time.Now().Format(time.RFC3339))

	c.JSON(http.StatusOK, gin.H{"message": "文件删除成功"})
}
