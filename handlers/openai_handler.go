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
		logrus.Printf("StepFun 请求报文: %s", reqBodyBytes)

		apiKey := os.Getenv("STEPFUN_API_KEY")
		if apiKey == "" {
			logrus.Println("STEPFUN_API_KEY is not set")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server configuration error"})
			return
		}

		// 系统消息
		systemMessage := models.StepFunMessage{
			Role:    "system",
			Content: payload.SystemPrompt,
		}

		var userMessage models.StepFunMessage

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

		//确认model
		//payload.FileType="img" 输入token小于4k等于4k就选step-1v-8k。输入token大于4k小于等于31k就选step-1v-32k,大于31k就报错
		//file_type不等于img,检查输入token,小于等于2k,就选step-1-flash。大于2k小于等于6k,就选step-1-8k。超过6k小于等于20k,就选step-1-32k。超过20k小于等于100k就选step-1-128k。超过100k小于等于200k就选step-1-256k。大于200k就报错。
		// 确认model
		model, err := getModelName(apiKey, messages, payload.FileType, payload.PerformanceLevel)
		if err != nil {
			logrus.Printf("Error getting model name: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		stepFunRequest := models.StepFunRequestPayload{
			Model:      model,
			Stream:     true,
			Messages:   messages,
			ToolChoice: "auto",
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

		// 如果前端传递了 vector_store_id，则添加 retrieval 工具
		if strings.TrimSpace(payload.VectorStoreID) != "" {
			retrievalTool := models.StepFunTool{
				Type: "retrieval",
				Function: models.StepFunToolFunction{
					Description: payload.Description,
					Options: map[string]string{
						"vector_store_id": payload.VectorStoreID, // 从前端传递的 vector_store_id
						"prompt_template": "从文档 {{knowledge}} 中找到问题 {{query}} 的答案。根据文档内容中的语句找到答案，如果文档中没用答案则告诉用户找不到相关信息；",
					},
				},
			}
			tools = append(tools, retrievalTool)
		} else {
			logrus.Println("vector_store_id not provided, skipping retrieval tool")
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
		stepFunReq, err := http.NewRequest("POST", "https://api.stepfun.com/v1/chat/completions", bytes.NewBuffer(stepFunReqBodyBytes))
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
			logrus.Printf("StepFun API returned status: %s, message: %s", resp.Status, errorText)
			c.JSON(resp.StatusCode, gin.H{"error": "StepFun API error"})
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

// getModelName 根据 FileType 和消息内容选择合适的模型
//func getModelName(apiKey string, messages []models.StepFunMessage, fileType, PerformanceLevel string) (string, error) {
//	//有图片的话不用判断技术模式
//	if fileType == "img" {
//		// 尝试使用 step-1v-8k
//		tokenCount, err := countTokens(apiKey, messages, "step-1v-8k")
//		if err != nil {
//			return "", fmt.Errorf("failed to count tokens with step-1v-8k: %w", err)
//		}
//		if tokenCount <= 5000 {
//			return "step-1v-8k", nil
//		} else if tokenCount > 25000 {
//			return "", fmt.Errorf("多摸分析，token count %d 超过了系统设定的step-1v-32k最大25K的输入限制", tokenCount)
//		}
//		// 尝试使用 step-1v-32k
//		tokenCount, err = countTokens(apiKey, messages, "step-1v-32k")
//		if err != nil {
//			return "", fmt.Errorf("failed to count tokens with step-1v-32k: %w", err)
//		}
//		if tokenCount <= 25000 {
//			return "step-1v-32k", nil
//		}
//		return "", fmt.Errorf("多摸分析，token count %d 超过了系统设定的step-1v-32k最大25K的输入限制", tokenCount)
//	} else {
//		tokenCount, err := countTokens(apiKey, messages, "step-1-flash")
//		if err != nil {
//			return "", fmt.Errorf("failed to count tokens with step-1-flash: %w", err)
//		}
//		// 极速模式优先判断是否可以使用 step-1-flash
//		if PerformanceLevel == "fast" && tokenCount <= 10000 {
//			// 尝试使用 step-1-flash
//			return "step-1-flash", nil
//
//		}
//		// 高级模式优先判断是否可以使用 step-2-16k
//		if PerformanceLevel == "advanced" && tokenCount <= 12000 {
//			tokenCount, err = countTokens(apiKey, messages, "step-2-16k")
//			if err != nil {
//				return "", fmt.Errorf("failed to count tokens with step-1v-32k: %w", err)
//			}
//			if tokenCount <= 12000 {
//				return "step-2-16k", nil
//			}
//		}
//		// 均衡模式优先判断是否可以使用 step-1-8k
//		if PerformanceLevel == "balanced" {
//			// 使用flash计算的tokenCount初步判断是否可以使用 step-1-8k
//			if tokenCount <= 6000 {
//				//确认基本符号后再精确计算tokens
//				tokenCount, err = countTokens(apiKey, messages, "step-1-8k")
//				if err != nil {
//					return "", fmt.Errorf("failed to count tokens with step-1-8k: %w", err)
//				}
//				//最终判断是否可用step-1-8k
//				if tokenCount <= 6000 {
//					return "step-1-8k", nil
//				}
//			}
//			// 使用flash计算的tokenCount初步判断是否可以使用 step-1-32k
//			if tokenCount <= 25000 {
//				//确认基本符号后再精确计算tokens
//				tokenCount, err = countTokens(apiKey, messages, "step-1-32k")
//				if err != nil {
//					return "", fmt.Errorf("failed to count tokens with step-1-32k: %w", err)
//				}
//				//最终判断是否可用step-1-32k
//				if tokenCount <= 25000 {
//					return "step-1-32k", nil
//				}
//			}
//			// 使用flash计算的tokenCount初步判断是否可以使用 step-1-128k
//			if tokenCount <= 80000 {
//				//确认基本符号后再精确计算tokens
//				tokenCount, err = countTokens(apiKey, messages, "step-1-128k")
//				if err != nil {
//					return "", fmt.Errorf("failed to count tokens with step-1-128k: %w", err)
//				}
//				//最终判断是否可用step-1-128k
//				if tokenCount <= 80000 {
//					return "step-1-128k", nil
//				}
//			}
//			// 使用flash计算的tokenCount初步判断是否可以使用 step-1-256k
//			if tokenCount <= 180000 {
//				//确认基本符号后再精确计算tokens
//				tokenCount, err = countTokens(apiKey, messages, "step-1-256k")
//				if err != nil {
//					return "", fmt.Errorf("failed to count tokens with step-1-256k: %w", err)
//				}
//				//最终判断是否可用step-1-256k
//				if tokenCount <= 180000 {
//					return "step-1-256k", nil
//				}
//			}
//
//		}
//		return "", fmt.Errorf("文本会话会话补全，token count %d  查过了设定的180K的上限", tokenCount)
//	}
//}

// countTokens 通过调用 StepFun 的 Token 计数 API 计算消息的 Token 数量
// 现在接收一个额外的参数 model，用于指定使用哪个模型来计数 tokens
//func countTokens(apiKey string, messages []models.StepFunMessage, model string) (int, error) {
//	// 定义请求负载
//	requestPayload := models.TokenCountRequest{
//		Model:    model, // 指定模型
//		Messages: messages,
//	}
//
//	// 编码为 JSON
//	reqBodyBytes, err := json.Marshal(requestPayload)
//	if err != nil {
//		return 0, fmt.Errorf("failed to marshal token count request: %w", err)
//	}
//
//	// 设置请求头
//	headers := map[string]string{
//		"Authorization": fmt.Sprintf("Bearer %s", apiKey),
//		"Content-Type":  "application/json",
//	}
//
//	// 使用 SendStepFunRequest 发送请求
//	resp, err := SendStepFunRequest("POST", "https://api.stepfun.com/v1/token/count", headers, bytes.NewBuffer(reqBodyBytes))
//	if err != nil {
//		return 0, fmt.Errorf("failed to send token count request: %w", err)
//	}
//	defer resp.Body.Close()
//
//	// 检查响应状态
//	if resp.StatusCode != http.StatusOK {
//		// 读取错误信息
//		bodyBytes, _ := ioutil.ReadAll(resp.Body)
//		return 0, fmt.Errorf("token count API error: %s", string(bodyBytes))
//	}
//
//	// 解码响应
//	var tokenCountResp models.TokenCountResponse
//	err = json.NewDecoder(resp.Body).Decode(&tokenCountResp)
//	if err != nil {
//		return 0, fmt.Errorf("failed to decode token count response: %w", err)
//	}
//	fmt.Printf("Model: %s", model)
//	fmt.Printf(";Token count: %d\n", tokenCountResp.Data.TotalTokens)
//
//	return tokenCountResp.Data.TotalTokens, nil
//}
//
//func loadFileContent(c *gin.Context, VectorFileId, apiKey string) string {
//	fileContentURL := fmt.Sprintf("https://api.stepfun.com/v1/files/%s/content", VectorFileId)
//	req, err := http.NewRequest("GET", fileContentURL, nil)
//	if err != nil {
//		logrus.Printf("Error creating file content request: %v", err)
//		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
//		return ""
//	}
//	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
//	// Send the request
//	client := &http.Client{}
//	resp, err := client.Do(req)
//	if err != nil {
//		logrus.Printf("Error sending file content request: %v", err)
//		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve file content"})
//		return ""
//	}
//	defer resp.Body.Close()
//
//	// Check response status
//	if resp.StatusCode != http.StatusOK {
//		logrus.Printf("Failed to retrieve file content, status code: %d", resp.StatusCode)
//		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve file content"})
//		return ""
//	}
//
//	// Read the response body
//	fileContentBytes, err := ioutil.ReadAll(resp.Body)
//	if err != nil {
//		logrus.Printf("Error reading file content response: %v", err)
//		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file content"})
//		return ""
//	}
//	// Store the file content
//	return string(fileContentBytes)
//}

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

		// 读取并进行 Base64 编码
		imageData, err := ioutil.ReadFile(uploadedFile.FilePath)
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

// processUploadedFiles 处理 FileType 为 "file" 且提供了 FileIDs 的文件上传逻辑。
// 它包括从数据库检索文件记录、上传文件到 StepFun、更新文件状态，以及将 VectorFileId 添加到 payload 中。
//func processUploadedFiles(db *dbop.Database, payload *models.RequestPayload, apiKey string, c *gin.Context) error {
//	for _, fileID := range payload.FileIDs {
//		// 通过 FileID 获取上传的文件记录
//		fileRecord, err := db.GetUploadedFileByID(fileID)
//		if err != nil {
//			logrus.Errorf("从数据库检索文件记录时出错: %v", err)
//			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法检索文件信息"})
//			return err
//		}
//		if fileRecord == nil {
//			logrus.Printf("未找到 FileID 为 %s 的上传文件", fileID)
//			c.JSON(http.StatusBadRequest, gin.H{"error": "上传的文件未找到"})
//			return fmt.Errorf("未找到 FileID 为 %s 的上传文件", fileID)
//		}
//
//		// 将文件上传到 StepFun 并进行提取
//		uploadResp, err := tool.UploadFileToStepFunWithExtract(fileRecord.FilePath, fileRecord.Filename)
//		if err != nil {
//			logrus.Errorf("上传文件到 StepFun 时出错: %v", err)
//			// 更新文件状态为 "failed"
//			if updateErr := db.UpdateUploadedFileStatus(fileID, "failed"); updateErr != nil {
//				logrus.Errorf("更新文件状态为 'failed' 时出错: %v", updateErr)
//			}
//			c.JSON(http.StatusInternalServerError, gin.H{"error": "文件上传失败"})
//			return err
//		}
//
//		// 开始轮询文件状态，每隔1秒查询一次，最多等待15秒
//		status, err := tool.PollFileStatus(uploadResp.ID, 15*time.Second)
//		if err != nil {
//			logrus.Errorf("查询文件解析状态时出错: %v", err)
//			return err
//		}
//		fmt.Printf("文件解析完成，状态: %s\n", status)
//
//		// 将上传后的文件信息插入到数据库中
//		err = db.InsertFile(uploadResp.ID, "local20241015145535", uploadResp.Bytes, fileID, status, "file-extract")
//		if err != nil {
//			logrus.Errorf("将文件记录插入数据库时出错: %v", err)
//			c.JSON(http.StatusInternalServerError, gin.H{"error": "插入文件记录失败"})
//			return err
//		}
//
//		// 更新文件状态为 "completed"表示上传的文件以及发给大模型结构进行解析
//		err = db.UpdateUploadedFileStatus(fileID, "completed")
//		if err != nil {
//			logrus.Errorf("更新文件状态为 'completed' 时出错: %v", err)
//			// 不返回错误，因为主流程可能仍需继续
//		}
//		//fmt.Print("uploadResp.VectorStoreID", uploadResp.ID)
//		// 将 VectorStoreID 添加到 payload 的 VectorFileIds 中
//		payload.VectorFileIds = append(payload.VectorFileIds, uploadResp.ID)
//	}
//	return nil
//}

//// sendStepFunRequest 通用的 HTTP 请求发送函数
//func SendStepFunRequest(method, url string, headers map[string]string, body *bytes.Buffer) (*http.Response, error) {
//	client := &http.Client{}
//	req, err := http.NewRequest(method, url, body)
//	if err != nil {
//		return nil, fmt.Errorf("failed to create request: %w", err)
//	}
//
//	for key, value := range headers {
//		req.Header.Set(key, value)
//	}
//
//	resp, err := client.Do(req)
//	if err != nil {
//		return nil, fmt.Errorf("failed to send request: %w", err)
//	}
//
//	return resp, nil
//}
