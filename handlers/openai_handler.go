package handlers

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"openapi-cms/dbop"
	"openapi-cms/middleware"
	"openapi-cms/models"
	"os"
	"path/filepath"
	"strings"
)

// handleChatMessagesStepFun 处理 StepFun 聊天消息的请求
func HandleChatMessagesChatGpt(db *dbop.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload models.RequestPayload
		if err := c.ShouldBindJSON(&payload); err != nil {
			logrus.Printf("Error binding JSON: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
			return
		}
		userName, ok := middleware.GetUserName(c)
		if !ok {
			logrus.Warn("userName not found or invalid")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		fmt.Println("****userName:", userName)
		// 打印收到的请求报文
		reqBodyBytes, err := json.Marshal(payload)
		if err != nil {
			logrus.Printf("Error marshaling request payload: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
			return
		}
		logrus.Printf("请求报文: %s", reqBodyBytes)

		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			logrus.Println("OPENAI_API_KEY is not set")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server configuration error"})
			return
		}
		//如果为高级，或者system_prompt为空，不实用system_prompt
		// 系统消息
		systemMessage := models.StepFunMessage{
			Role:    "system",
			Content: payload.SystemPrompt,
		}

		var userMessage models.StepFunMessage

		// 判断是否为图片消息

		if payload.FileType == "img" && len(payload.FileIDs) > 0 {
			// 处理图片消息
			userMessage, err = processImageMessages(db, payload)
			if err != nil {
				logrus.Printf("处理图片消息时出错: %v", err)
				// 假设 processImageMessages 已经处理了响应
				return
			}
		} else if payload.FileType == "file" && len(payload.FileIDs) > 0 {
			// 处理文件消息，把vector_file_id 放到数组里
			err = processUploadedFiles(db, &payload, apiKey, c)
			if err != nil {
				// processUploadedFiles 已经处理了响应
				return
			}
			// 构建用户消息
			userMessage = models.StepFunMessage{
				Role:    "user",
				Content: payload.Query,
			}

		} else {
			// 构建用户消息
			userMessage = models.StepFunMessage{
				Role:    "user",
				Content: payload.Query,
			}
		}

		// 构建初始消息列表
		messages := []models.StepFunMessage{systemMessage}

		// 如果有 conversation_history，则将其添加到消息列表中
		if len(payload.ConversationHistory) > 0 {
			messages = append(messages, payload.ConversationHistory...)
		}

		// 最后添加 userMessage
		messages = append(messages, userMessage)

		// 如果前端传了 vector_file_id，那么将文件内容解析并存入 messages 里
		//fmt.Println("payload.VectorFileIds", payload.VectorFileIds)
		if len(payload.VectorFileIds) > 0 {
			insertIndex := 1
			for _, vectorFileId := range payload.VectorFileIds {
				fmt.Println("vectorFileId", vectorFileId)
				fileContent := loadFileContent(c, vectorFileId, apiKey)
				if fileContent != "" {
					contentMessage := models.StepFunMessage{
						Role:    "user",
						Content: fileContent,
					}
					// 在指定位置插入 contentMessage
					messages = append(messages[:insertIndex], append([]models.StepFunMessage{contentMessage}, messages[insertIndex:]...)...)
					// 每次插入后，插入位置增加 2
					insertIndex += 2
				}
			}
		}

		model := "gpt-4o-mini"

		stepFunRequest := models.StepFunRequestPayload{
			Model:    model,
			Stream:   true,
			Messages: messages,
			//ToolChoice: "auto",
			ResponseFormat: models.ResponseFormat{
				Type: "text", // 设置默认值为 "text"
			},
		}

		// 构建工具列表
		tools := []models.StepFunTool{}

		// 添加 web_search 工具
		if payload.WebSearch {
			webSearchTool := models.StepFunTool{
				Type: "web_search",
				Function: models.StepFunToolFunction{
					Description: "这个工具web_search可以用来搜索互联网的信息",
				},
			}
			tools = append(tools, webSearchTool)
		}

		// 将工具添加到请求中
		stepFunRequest.Tools = tools
		// 将请求体编码为 JSON
		stepFunReqBodyBytes, err := json.Marshal(stepFunRequest)
		if err != nil {
			logrus.Printf("Error marshaling StepFun request payload: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
			return
		}

		// 打印发送给 StepFun API 的请求报文
		//logrus.Printf("StepFun API 请求报文: %s", stepFunReqBodyBytes)
		fmt.Printf("StepFun API 请求报文: %s", stepFunReqBodyBytes)
		// 创建向 StepFun API 发送的 HTTP 请求
		stepFunReq, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(stepFunReqBodyBytes))
		if err != nil {
			logrus.Printf("Error creating request: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
			return
		}

		// 设置请求头
		stepFunReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
		stepFunReq.Header.Set("Content-Type", "application/json")

		// 发送请求
		client := &http.Client{}
		resp, err := client.Do(stepFunReq)
		if err != nil {
			logrus.Printf("Error sending request to StepFun API: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to communicate with StepFun API"})
			return
		}
		defer resp.Body.Close()

		// 检查响应状态
		if resp.StatusCode != http.StatusOK {
			errorText := ""
			if resp.Body != nil {
				scanner := bufio.NewScanner(resp.Body)
				for scanner.Scan() {
					errorText += scanner.Text()
				}
			}
			logrus.Printf("OpenAI returned status: %s, message: %s", resp.Status, errorText)
			c.JSON(resp.StatusCode, gin.H{"error": "OpenAI error"})
			return
		}

		// 设置响应头以支持流式传输
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")

		// 使用 bufio.Scanner 逐行读取响应体
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue // 跳过空行
			}
			// 打印返回的报文
			logrus.Printf("StepFun 返回报文: %s", line)
			// 发送数据到前端，确保 JSON 字符串未被转义
			fmt.Fprintf(c.Writer, "%s\n\n", line)
			// 刷新缓冲区
			c.Writer.Flush()
		}

		if err := scanner.Err(); err != nil {
			logrus.Printf("Error reading response from StepFun API: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read StepFun response"})
			return
		}

		c.Status(http.StatusOK)
	}
}

// processImageMessages 处理图片消息，通过读取和编码图片文件来构建消息内容。
func processImageMessages(db *dbop.Database, payload models.RequestPayload) (models.StepFunMessage, error) {
	var content []models.StepFunMessageContent
	for _, fileID := range payload.FileIDs {
		// 通过 FileID 获取上传的文件记录
		uploadedFile, err := db.GetUploadedFileByID(fileID)
		if err != nil {
			logrus.Printf("检索上传文件时出错: %v", err)
			return models.StepFunMessage{}, fmt.Errorf("无法检索上传文件")
		}
		if uploadedFile == nil {
			logrus.Printf("未找到 FileID 为 %s 的上传文件", fileID)
			return models.StepFunMessage{}, fmt.Errorf("未找到 FileID 为 %s 的上传文件", fileID)
		}
		// 从环境变量中读取文件保存路径
		uploadDir := os.Getenv("FILE_PATH")
		if uploadDir == "" {
			uploadDir = "./uploads" // 默认值（可选）
			logrus.Warn("环境变量 'FILE_PATH' 未设置，使用默认路径 './uploads'")
		}
		// 构建新的目标目录路径：uploadDir/username/currentDate
		targetDir := filepath.Join(uploadDir, uploadedFile.FilePath)

		// 读取并进行 Base64 编码
		imageData, err := ioutil.ReadFile(targetDir)
		if err != nil {
			logrus.Printf("读取图片文件时出错: %v", err)
			return models.StepFunMessage{}, fmt.Errorf("读取图片文件失败")
		}
		encodedImage := base64.StdEncoding.EncodeToString(imageData)
		imageURL := fmt.Sprintf("data:%s;base64,%s", uploadedFile.FileType, encodedImage)

		// 构建图片消息内容
		imageContent := models.StepFunMessageContent{
			Type: "image_url",
			ImageURL: &models.StepFunMessageImageURL{
				URL:    imageURL,
				Detail: "high",
			},
		}
		content = append(content, imageContent)
	}

	// 添加来自 payload 查询的文本内容
	textContent := models.StepFunMessageContent{
		Type: "text",
		Text: payload.Query,
	}
	content = append(content, textContent)

	return models.StepFunMessage{
		Role:    "user",
		Content: content,
	}, nil
}
