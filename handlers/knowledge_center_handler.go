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
	"openapi-cms/middleware"
	"openapi-cms/models"
	"os"
	"regexp"
	"strings"
	"time"
)

// HandleCreateVectorStore 处理创建向量存储的请求
func HandleCreateVectorStore(c *gin.Context, db models.DatabaseInterface) {
	var payload struct {
		Name        string `json:"name"`         // 知识库标识
		DisplayName string `json:"display_name"` // 知识库名称
		Description string `json:"description"`
		Tags        string `json:"tags"`
		ModelOwner  string `json:"model_owner"` //所属模型：stepfun，zhipu，moonshot，baichuan，自定义（local)
	}
	userName, ok := middleware.GetUserName(c)
	if !ok {
		logrus.Warn("userName not found or invalid")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	fmt.Println("****userName:", userName)

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
	// Step 1: 校验 name 是否已经存在于数据库
	existingKB, err := db.GetKnowledgeBaseByName(payload.Name)
	if err != nil {
		logrus.WithError(err).Error("Error fetching knowledge base from database")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Step 2: 如果存在
	if existingKB != nil {
		// 校验 ID 是否有值,直接跳过
		if existingKB.ID != "" {
			logrus.WithField("name", payload.Name).Error("Knowledge base already exists")
			c.JSON(http.StatusBadRequest, gin.H{"error": "Knowledge base with this name already exists"})
			return
		}

		// 如果 ID 为空，更新知识库的其他字段
		logrus.WithField("name", payload.Name).Info("Knowledge base exists but has no ID, updating existing record")
		err := db.UpdateKnowledgeBaseByName(payload.Name, payload.DisplayName, payload.Description, payload.Tags, payload.ModelOwner)
		if err != nil {
			logrus.WithError(err).Error("Error updating knowledge base")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update knowledge base"})
			return
		}
		// 更新后继续依据 ModelOwner 调用相应的 API
		newID, err := handleModelOwnerAPI(c, db, struct {
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
			Description string `json:"description"`
			Tags        string `json:"tags"`
			ModelOwner  string `json:"model_owner"`
		}{
			Name:        existingKB.Name,
			DisplayName: payload.DisplayName,
			Description: payload.Description,
			Tags:        payload.Tags,
			ModelOwner:  existingKB.ModelOwner,
		}, existingKB.ID)
		if err != nil {
			logrus.WithError(err).Error("Error handling model owner API")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to handle model owner API"})
			return
		}

		// 返回成功响应，使用 payload 和 new name
		c.JSON(http.StatusOK, gin.H{
			"id":           newID, // 新的 ID
			"name":         payload.Name,
			"display_name": payload.DisplayName,
			"description":  payload.Description,
			"tags":         payload.Tags,
			"model_owner":  payload.ModelOwner,
		})
		return
	}

	// Step 3: 如果不存在数据库，则先插入基本信息到数据库
	timeNow := time.Now().Format("20060102150405")
	id := fmt.Sprintf("%s%s", payload.Name, timeNow)
	if err := db.InsertVectorStore(id, payload.Name, payload.DisplayName, payload.Description, payload.Tags, payload.ModelOwner, "admin"); err != nil {
		logrus.WithError(err).Error("Error inserting vector store into database")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Step 4: 依据 ModelOwner 调用相应的 API
	newID, err := handleModelOwnerAPI(c, db, payload, id)
	if err != nil {
		logrus.WithError(err).Error("Error handling model owner API")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to handle model owner API"})
		return
	}
	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"id":           newID,
		"name":         payload.Name,
		"display_name": payload.DisplayName,
		"description":  payload.Description,
		"tags":         payload.Tags,
		"model_owner":  payload.ModelOwner,
	})
}

// handleModelOwnerAPI 根据 ModelOwner 值调用相应的 API 或处理逻辑
func handleModelOwnerAPI(c *gin.Context, db models.DatabaseInterface, payload struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Tags        string `json:"tags"`
	ModelOwner  string `json:"model_owner"`
}, id string) (string, error) { // 返回新的 name（ID）
	switch payload.ModelOwner {
	case "stepfun":
		// 调用 StepFun API 获取 ID 并更新数据库
		idFromAPI, err := callStepFunAPI(c, payload)
		if err != nil {
			return "", err
		}
		// 更新数据库中的 ID
		if err := db.UpdateKnowledgeBaseIDByName(payload.Name, idFromAPI); err != nil {
			logrus.WithError(err).Error("Error updating knowledge base ID in database")
			return "", err
		}
		return idFromAPI, nil
	case "zhipu", "moonshot", "baichuan":
		// 记录警告并跳过 ID 更新
		logrus.WithField("model_owner", payload.ModelOwner).Warn("Model owner API not implemented, skipping ID update")
		return id, nil
	case "local":
		newID, err := localAPI(c, db, payload)
		if err != nil {
			return "", err
		}
		// 本地逻辑已经在数据库插入时处理，不需调用外部 API
		return newID, nil
	default:
		// 无效的 ModelOwner
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid model owner"})
		return "", fmt.Errorf("invalid model owner")
	}
}

// HandleUpdateKnowledgeBase 处理更新知识库的请求
func HandleUpdateKnowledgeBase(c *gin.Context, db models.DatabaseInterface) {
	// 从 URL 参数获取知识库 name（已改为使用 name 而非 id）
	name := c.Param("name")
	if name == "" {
		logrus.Error("Missing knowledge base name in URL")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Knowledge base name is required"})
		return
	}

	var payload struct {
		DisplayName string `json:"display_name"` // 知识库名称
		Description string `json:"description"`
		Tags        string `json:"tags"`
		// 如果需要，可以添加 ModelOwner 以便在更新时修改归属模型
		// ModelOwner  string `json:"model_owner,omitempty"`
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
	existingKB, err := db.GetKnowledgeBaseByName(name)
	if err != nil {
		logrus.WithError(err).Error("Error fetching knowledge base from database")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if existingKB == nil {
		logrus.WithField("name", name).Error("Knowledge base not found")
		c.JSON(http.StatusNotFound, gin.H{"error": "Knowledge base not found"})
		return
	}

	// 开始事务
	tx, err := db.BeginTransaction()
	if err != nil {
		logrus.WithError(err).Error("Error starting transaction")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	defer func() {
		if err != nil {
			db.RollbackTransaction(tx)
		}
	}()

	// 更新数据库中的记录
	if err := db.UpdateKnowledgeBase(name, payload.DisplayName, payload.Description, payload.Tags); err != nil {
		logrus.WithError(err).Error("Error updating knowledge base in database")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// 检查是否需要获取并设置ID
	if existingKB.ID == "" {
		// 调用 handleModelOwnerAPI 来获取并设置ID，直接赋值给 name
		newID, err := handleModelOwnerAPI(c, db, struct {
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
			Description string `json:"description"`
			Tags        string `json:"tags"`
			ModelOwner  string `json:"model_owner"`
		}{
			Name:        existingKB.Name,
			DisplayName: payload.DisplayName,
			Description: payload.Description,
			Tags:        payload.Tags,
			ModelOwner:  existingKB.ModelOwner,
		}, existingKB.ID)
		if err != nil {
			logrus.WithError(err).Error("Error handling model owner API")
			// 返回具体的错误信息
			if err.Error() == "This functionality is not yet implemented for the selected model." {
				c.JSON(http.StatusNotImplemented, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to handle model owner API"})
			}
			return
		}
		// 更新后的ID已经在 handleModelOwnerAPI 中处理，并返回了新的ID
		existingKB.ID = newID
	}

	// 提交事务
	if err := db.CommitTransaction(tx); err != nil {
		logrus.WithError(err).Error("Error committing transaction")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// 获取更新后的知识库记录以返回最新信息
	updatedKB, err := db.GetKnowledgeBaseByName(existingKB.Name)
	if err != nil {
		logrus.WithError(err).Error("Error fetching updated knowledge base from database")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if updatedKB == nil {
		logrus.Error("Updated knowledge base not found after update")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Knowledge base not found after update"})
		return
	}

	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"id":           updatedKB.ID,
		"name":         updatedKB.Name, // 保持原有的 name（只读）
		"display_name": updatedKB.DisplayName,
		"description":  updatedKB.Description,
		"tags":         updatedKB.Tags,
		"model_owner":  updatedKB.ModelOwner, // 保持原有的 model_owner
	})
}

// callStepFunAPI 封装了对 StepFun API 的调用逻辑-创建知识库
func callStepFunAPI(c *gin.Context, payload struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Tags        string `json:"tags"`
	ModelOwner  string `json:"model_owner"`
}) (string, error) {
	reqBody := map[string]string{
		"name": payload.Name,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		logrus.WithError(err).Error("Error marshaling request payload")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
		return "", err
	}

	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", os.Getenv("STEPFUN_API_KEY")),
		"Content-Type":  "application/json",
	}

	// 调用 StepFun API
	resp, err := SendStepFunRequest("POST", "https://api.stepfun.com/v1/vector_stores", headers, bytes.NewBuffer(jsonData))
	if err != nil {
		logrus.WithError(err).Error("Error sending request to StepFun API")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to communicate with StepFun API"})
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.WithError(err).Error("Error reading StepFun API response body")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read StepFun API response"})
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		logrus.WithField("status", resp.Status).Error("StepFun API returned non-200 status")
		c.JSON(resp.StatusCode, gin.H{"error": "StepFun API error"})
		return "", fmt.Errorf("StepFun API error")
	}

	var response models.StepFunResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		logrus.WithError(err).Error("Error unmarshaling StepFun API response")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse StepFun API response"})
		return "", err
	}

	return response.ID, nil
}

func zhipuAPI() error {
	// 返回尚未实现的提示
	return fmt.Errorf("This functionality is not yet implemented for the selected model.")
}

func moonshotAPI() error {
	// 返回尚未实现的提示
	return fmt.Errorf("This functionality is not yet implemented for the selected model.")
}

func baichuanAPI() error {
	// 返回尚未实现的提示
	return fmt.Errorf("This functionality is not yet implemented for the selected model.")
}

// localAPI 修改为返回生成的ID和error
func localAPI(c *gin.Context, db models.DatabaseInterface, payload struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Tags        string `json:"tags"`
	ModelOwner  string `json:"model_owner"`
}) (string, error) {
	// 生成基于 name 和时间的 ID，时间精确到秒
	timeNow := time.Now().Format("20060102150405")
	id := fmt.Sprintf("%s%s", payload.Name, timeNow)

	// 插入数据库
	if err := db.InsertVectorStore(id, payload.Name, payload.DisplayName, payload.Description, payload.Tags, payload.ModelOwner, "admin"); err != nil {
		logrus.WithError(err).Error("Error inserting vector store into database")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return "", err
	}
	// 返回生成的ID
	return id, nil
}
