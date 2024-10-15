// knowledge_center_handler.go
package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
	"openapi-cms/models"
	"os"
	"regexp"
	"strings"
	"time"
)

// handleCreateVectorStore 处理创建向量存储的请求
func HandleCreateVectorStore(c *gin.Context, db models.DatabaseInterface) {
	var payload struct {
		Name        string `json:"name"`         // 知识库标识
		DisplayName string `json:"display_name"` // 知识库名称
		Description string `json:"description"`
		Tags        string `json:"tags"`
		ModelOwner  string `json:"model_owner"` //所属模型：stepfun，zhipu，moonshot，baichuan，自定义（local)
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
	// 验证 model_owner 为必填
	if strings.TrimSpace(payload.ModelOwner) == "" {
		logrus.Error("Model owner is required")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Model owner is required"})
		return
	}
	// 根据 ModelOwner 处理不同逻辑
	switch payload.ModelOwner {
	case "stepfun":
		stepfunAPI(c, payload)
	case "zhipu":
		zhipuAPI(c)
	case "moonshot":
		moonshotAPI(c)
	case "baichuan":
		baichuanAPI(c)
	case "local":
		localAPI(c, db, payload)
	default:
		// 如果传入的 model_owner 不在支持范围内
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid model owner"})
	}
}

// HandleUpdateKnowledgeBase 处理更新知识库的请求
func HandleUpdateKnowledgeBase(c *gin.Context, db models.DatabaseInterface) {
	// 从 URL 参数获取知识库 ID
	id := c.Param("id")
	if id == "" {
		logrus.Error("Missing knowledge base ID in URL")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Knowledge base ID is required"})
		return
	}

	var payload struct {
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

	// 获取现有的知识库记录
	existingKB, err := db.GetKnowledgeBaseByID(id)
	if err != nil {
		logrus.WithError(err).Error("Error fetching knowledge base from database")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if existingKB == nil {
		logrus.WithField("id", id).Error("Knowledge base not found")
		c.JSON(http.StatusNotFound, gin.H{"error": "Knowledge base not found"})
		return
	}

	// 更新数据库中的记录
	if err := db.UpdateKnowledgeBase(id, payload.DisplayName, payload.Description, payload.Tags); err != nil {
		logrus.WithError(err).Error("Error updating knowledge base in database")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"id":           existingKB.ID,
		"name":         existingKB.Name, // 保持原有的 name（只读）
		"display_name": payload.DisplayName,
		"description":  payload.Description,
		"tags":         payload.Tags,
		"model_owner":  existingKB.ModelOwner, // 保持原有的 model_owner
	})
}

func stepfunAPI(c *gin.Context, payload struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Tags        string `json:"tags"`
	ModelOwner  string `json:"model_owner"`
}) {
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
}
func zhipuAPI(c *gin.Context) {
	// 返回尚未实现的提示
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "This functionality is not yet implemented for the selected model.",
	})
}
func moonshotAPI(c *gin.Context) {
	// 返回尚未实现的提示
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "This functionality is not yet implemented for the selected model.",
	})
}
func baichuanAPI(c *gin.Context) {
	// 返回尚未实现的提示
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "This functionality is not yet implemented for the selected model.",
	})
}

func localAPI(c *gin.Context, db models.DatabaseInterface, payload struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Tags        string `json:"tags"`
	ModelOwner  string `json:"model_owner"`
}) {
	// 生成基于 name 和时间的 ID，时间精确到秒
	timeNow := time.Now().Format("20060102150405")
	id := fmt.Sprintf("%s%s", payload.Name, timeNow)

	// 插入数据库
	if err := db.InsertVectorStore(id, payload.Name, payload.DisplayName, payload.Description, payload.Tags, payload.ModelOwner, "admin"); err != nil {
		logrus.WithError(err).Error("Error inserting vector store into database")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"id":           id,
		"name":         payload.Name,
		"display_name": payload.DisplayName,
		"description":  payload.Description,
		"tags":         payload.Tags,
		"model_owner":  payload.ModelOwner,
	})
}
