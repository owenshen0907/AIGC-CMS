// knowledge_center_handler.go
package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"openapi-cms/models"
	"os"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// handleCreateVectorStore 处理创建向量存储的请求
func HandleCreateVectorStore(c *gin.Context, db models.DatabaseInterface) {
	var payload struct {
		Name        string `json:"name"`         // 知识库标识
		DisplayName string `json:"display_name"` // 知识库名称
		Description string `json:"description"`
		Tags        string `json:"tags"`
	}

	// 绑定 JSON 请求体到结构体
	if err := c.ShouldBindJSON(&payload); err != nil {
		logrus.WithError(err).Error("Error binding JSON")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	// 验证 name 字段：只能包含字母、数字、下划线，且不能以下划线开头
	validNameRegex := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_]*$`)
	if !validNameRegex.MatchString(payload.Name) {
		logrus.WithField("name", payload.Name).Error("Invalid name format")
		c.JSON(http.StatusBadRequest, gin.H{"error": "The name can only contain letters, numbers, and underscores, and cannot start with an underscore."})
		return
	}
	// 验证 display_name 为必填
	if strings.TrimSpace(payload.DisplayName) == "" {
		logrus.Error("Display name is required")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Display name is required"})
		return
	}

	// 验证 description 和 tags 的长度
	if len(payload.Description) > 500 {
		logrus.WithField("description_length", len(payload.Description)).Error("Description too long")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Description is too long"})
		return
	}

	if len(payload.Tags) > 200 {
		logrus.WithField("tags_length", len(payload.Tags)).Error("Tags too long")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tags are too long"})
		return
	}

	// 准备发送给 StepFun API 的请求体，仅包含 name 字段
	reqBody := map[string]string{
		"name": payload.Name,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		logrus.WithError(err).Error("Error marshaling request payload")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
		return
	}

	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", os.Getenv("STEPFUN_API_KEY")),
		"Content-Type":  "application/json",
	}

	// 发送请求给 StepFun API
	resp, err := SendStepFunRequest("POST", "https://api.stepfun.com/v1/vector_stores", headers, bytes.NewBuffer(jsonData))
	if err != nil {
		logrus.WithError(err).Error("Error sending request to StepFun API")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to communicate with StepFun API"})
		return
	}
	defer resp.Body.Close()

	// 读取响应体
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.WithError(err).Error("Error reading StepFun API response body")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read StepFun API response"})
		return
	}

	// 处理非 200 状态码的响应
	if resp.StatusCode != http.StatusOK {
		var responseBody map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &responseBody); err != nil {
			logrus.WithError(err).Error("Error unmarshaling StepFun API error response")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse StepFun API error response"})
			return
		}

		logrus.WithFields(logrus.Fields{
			"status":   resp.Status,
			"response": responseBody,
		}).Error("StepFun API returned error status")

		// 提取错误信息
		errorDetails := ""
		if errMap, ok := responseBody["error"].(map[string]interface{}); ok {
			if msg, ok := errMap["message"].(string); ok {
				errorDetails = msg
			}
		}

		c.JSON(resp.StatusCode, gin.H{
			"error":   "StepFun API error",
			"details": errorDetails,
		})
		return
	}

	// 解析成功的响应
	var response models.StepFunResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		logrus.WithError(err).Error("Error unmarshaling StepFun API response")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse StepFun API response"})
		return
	}

	logrus.WithField("response", response).Info("StepFun API returned successfully")

	// 使用预处理语句将 StepFun API 返回的 ID 存入数据库
	if err := db.InsertVectorStore(response.ID, payload.Name, payload.DisplayName, payload.Description, payload.Tags); err != nil {
		logrus.WithError(err).Error("Error inserting vector store into database")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"id":           response.ID,
		"name":         payload.Name,
		"display_name": payload.DisplayName,
		"description":  payload.Description,
		"tags":         payload.Tags,
	})
}
