// upload.go
package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"openapi-cms/dbop"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// UploadResponse 用于解析外部 API 的响应
type UploadResponse struct {
	ID            string `json:"id"`
	UsageBytes    int    `json:"usage_bytes"`
	VectorStoreID string `json:"vector_store_id"`
}

// handleUploadFile 处理上传文件的请求
func HandleUploadFile(c *gin.Context, db *dbop.Database) {
	// 从表单中获取 vector_store_id
	vectorStoreID := c.PostForm("vector_store_id")
	if vectorStoreID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "vector_store_id is required"})
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

	// 调用外部 API 上传文件
	uploadResp, err := uploadFileToStepFun(vectorStoreID, file, header.Filename)
	if err != nil {
		logrus.Errorf("Error uploading file to StepFun: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload file"})
		return
	}

	// 将响应数据存储到数据库
	err = db.InsertFile(uploadResp.ID, uploadResp.VectorStoreID, uploadResp.UsageBytes)
	if err != nil {
		logrus.Errorf("Error inserting file record to database: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store file information"})
		return
	}

	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"id":              uploadResp.ID,
		"usage_bytes":     uploadResp.UsageBytes,
		"vector_store_id": uploadResp.VectorStoreID,
	})
}

// uploadFileToStepFun 调用外部 StepFun API 上传文件
func uploadFileToStepFun(vectorStoreID string, file multipart.File, filename string) (*UploadResponse, error) {
	// StepFun API URL
	url := fmt.Sprintf("https://api.stepfun.com/v1/vector_stores/%s/files", vectorStoreID)

	// 创建缓冲区和 multipart 写入器
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// 添加文件字段
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return nil, fmt.Errorf("failed to copy file data: %w", err)
	}

	// 关闭写入器以设置结束边界
	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", url, &requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// 设置必要的头部
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+os.Getenv("STEPFUN_API_KEY"))

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("received non-200 response: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	// 解析响应 JSON
	var uploadResp UploadResponse
	err = json.NewDecoder(resp.Body).Decode(&uploadResp)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response JSON: %w", err)
	}

	return &uploadResp, nil
}
