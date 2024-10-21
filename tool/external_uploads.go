// tool/external_uploads.go
package tool

import (
	"fmt"
	"net/http"
	"openapi-cms/dbop"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type TriggerUploadRequest struct {
	ModelOwner    string `json:"model_owner" binding:"required"`
	FileID        string `json:"file_id" binding:"required"`
	Purpose       string `json:"purpose" binding:"required"`
	VectorStoreID string `json:"vectorStoreID" binding:"required"`
}

// UploadResponse 用于解析外部 API 的响应
type UploadResponse struct {
	ID            string `json:"id"`
	UsageBytes    int    `json:"usage_bytes"`
	VectorStoreID string `json:"vector_store_id"`
}

// HandleTriggerExternalUpload 处理触发外部上传的请求
func HandleTriggerExternalUpload(c *gin.Context, db *dbop.Database) {
	var req TriggerUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logrus.Errorf("Invalid request parameters: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameters"})
		return
	}

	fmt.Println("modelOwner:", req.ModelOwner)

	// 验证必要参数
	if req.ModelOwner == "" || req.FileID == "" || req.Purpose == "" || req.VectorStoreID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_owner, file_id, purpose and vectorStoreID are required"})
		return
	}
	// 从数据库中获取文件信息
	fileRecord, err := db.GetUploadedFileByID(req.FileID)
	if err != nil {
		logrus.Errorf("Error retrieving file record from database: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve file information"})
		return
	}
	if fileRecord == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}
	switch req.ModelOwner {
	case "stepfun":
		if req.Purpose == "retrieval" {
			uploadResp, err := uploadFileToStepFunWithRetrieval(req.VectorStoreID, fileRecord.FilePath, fileRecord.Filename)
			if err != nil {
				logrus.Errorf("Error uploading file to StepFun: %v", err)
				// 更新上传文件的状态为 "failed"
				if updateErr := db.UpdateUploadedFileStatus(req.FileID, "failed"); updateErr != nil {
					logrus.Errorf("Error updating uploaded file status to 'failed': %v", updateErr)
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload file"})
				return
			}

			// 更新数据库中的 files 表，存储 API 返回的文件信息，增加 fileID
			err = db.InsertFile(uploadResp.ID, uploadResp.VectorStoreID, uploadResp.UsageBytes, req.FileID, "retrieval")
			if err != nil {
				logrus.Errorf("Error inserting file record to database: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert file record"})
				return
			}

			// 更新上传文件的状态为 "completed"
			err = db.UpdateUploadedFileStatus(req.FileID, "completed")
			if err != nil {
				logrus.Errorf("Error updating uploaded file status: %v", err)
			}

			// 返回成功响应
			c.JSON(http.StatusOK, gin.H{
				"file_id":         req.FileID,
				"status":          "File will be uploaded to StepFun",
				"vector_store_id": uploadResp.VectorStoreID,
			})
			return
		}
	case "local":
		if req.Purpose == "file-extract" {
			uploadResp, err := UploadFileToStepFunWithExtract(fileRecord.FilePath, fileRecord.Filename)
			if err != nil {
				logrus.Errorf("Error uploading file to local: %v", err)
				// 更新上传文件的状态为 "failed"
				if updateErr := db.UpdateUploadedFileStatus(req.FileID, "failed"); updateErr != nil {
					logrus.Errorf("Error updating uploaded file status to 'failed': %v", updateErr)
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload file"})
				return
			}

			// 更新数据库中的 files 表，存储 API 返回的文件信息，增加 fileID
			err = db.InsertFile(uploadResp.ID, req.VectorStoreID, uploadResp.UsageBytes, req.FileID, "file-extract")
			if err != nil {
				logrus.Errorf("Error inserting file record to database: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert file record"})
				return
			}

			// 更新上传文件的状态为 "completed"
			err = db.UpdateUploadedFileStatus(req.FileID, "completed")
			if err != nil {
				logrus.Errorf("Error updating uploaded file status: %v", err)
			}

			// 返回成功响应
			c.JSON(http.StatusOK, gin.H{
				"file_id":         req.FileID,
				"status":          "File will be uploaded to StepFun",
				"vector_store_id": uploadResp.VectorStoreID,
			})
			return
		}
	case "zhipu", "baichuan", "moonshot":
		c.JSON(http.StatusNotImplemented, gin.H{"error": "This functionality is not yet implemented for the selected model."})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported model_owner"})
		return
	}
	// 如果没有匹配的条件，返回默认响应
	c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid model_owner or purpose"})
}
