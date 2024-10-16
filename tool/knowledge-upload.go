// uploads.go
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
	"github.com/google/uuid"
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
	// 从表单中获取file_description
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
	// 保存文件到服务器本地
	filePath := fmt.Sprintf("./uploads/%s", header.Filename) // 假设存储路径为 ./uploads/
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
	// 根据 model_owner 的值进行不同处理
	if modelOwner == "local" {
		// 处理 local 模式：仅上传到服务器本地并更新数据库
		err = db.InsertUploadedFile(fileID, header.Filename, filePath, fileType, vectorStoreID, fileDescription)
		if err != nil {
			logrus.Errorf("Error inserting uploaded file record to database: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store file information"})
			return
		}

		// 返回成功响应
		c.JSON(http.StatusOK, gin.H{
			"file_id":   fileID,
			"status":    "File saved successfully to local server",
			"file_path": filePath,
		})
	} else if modelOwner == "stepfun" {
		// 处理 stepfun 模式：上传到服务器本地，更新数据库，然后上传到 StepFun
		err = db.InsertUploadedFile(fileID, header.Filename, filePath, fileType, vectorStoreID, fileDescription)
		if err != nil {
			logrus.Errorf("Error inserting uploaded file record to database: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store file information"})
			return
		}

		// 异步调用外部 API 上传文件，并记录 API 返回的文件信息
		go func() {
			uploadResp, err := uploadFileToStepFun(vectorStoreID, filePath, header.Filename)
			if err != nil {
				logrus.Errorf("Error uploading file to StepFun: %v", err)
				// 更新上传文件的状态为 "failed"
				if updateErr := db.UpdateUploadedFileStatus(fileID, "failed"); updateErr != nil {
					logrus.Errorf("Error updating uploaded file status to 'failed': %v", updateErr)
				}
				return
			}

			// 更新数据库中的 files 表，存储 API 返回的文件信息，增加 fileID
			err = db.InsertFile(uploadResp.ID, vectorStoreID, uploadResp.UsageBytes, fileID)
			if err != nil {
				logrus.Errorf("Error inserting file record to database: %v", err)
				return
			}

			// 更新上传文件的状态为 "completed"
			err = db.UpdateUploadedFileStatus(fileID, "completed")
			if err != nil {
				logrus.Errorf("Error updating uploaded file status: %v", err)
			}
		}()

		// 返回成功响应
		c.JSON(http.StatusOK, gin.H{
			"file_id":   fileID,
			"status":    "File saved successfully to local server and will be uploaded to StepFun",
			"file_path": filePath,
		})
	}
}

// uploadFileToStepFun 调用外部 StepFun API 上传文件
func uploadFileToStepFun(vectorStoreID string, filePath, filename string) (*UploadResponse, error) {
	// StepFun API URL
	url := fmt.Sprintf("https://api.stepfun.com/v1/vector_stores/%s/files", vectorStoreID)

	// 打开本地文件
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()
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
