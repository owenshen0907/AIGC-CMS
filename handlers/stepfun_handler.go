// handlers/stepfun_handler.go
package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"openapi-cms/dbop"
	"openapi-cms/middleware"
	"openapi-cms/models"
	"openapi-cms/tool"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// HandleChatMessagesStepFun 处理 StepFun 聊天消息的请求
func HandleChatMessagesStepFun(db *dbop.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload models.RequestPayload
		if err := c.ShouldBindJSON(&payload); err != nil {
			logrus.Printf("绑定 JSON 失败: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求负载"})
			return
		}

		userName, ok := middleware.GetUserName(c)
		if !ok {
			logrus.Warn("未找到或无效的 userName")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
			return
		}
		logrus.Printf("****userName: %s", userName)

		// 打印收到的请求报文
		reqBodyBytes, err := json.Marshal(payload)
		if err != nil {
			logrus.Printf("序列化请求负载失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
			return
		}
		logrus.Printf("StepFun 请求报文: %s", reqBodyBytes)

		apiKey := os.Getenv("STEPFUN_API_KEY")
		if apiKey == "" {
			logrus.Println("未设置 STEPFUN_API_KEY")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器配置错误"})
			return
		}

		// 构建系统消息
		systemMessage := models.StepFunMessage{
			Role:    "system",
			Content: payload.SystemPrompt,
		}

		// 构建用户消息
		userMessage := payload.UserPrompt

		// 构建初始消息列表
		messages := []models.StepFunMessage{systemMessage}

		// 如果有 conversation_history，则将其添加到消息列表中
		if len(payload.ConversationHistory) > 0 {
			messages = append(messages, payload.ConversationHistory...)
		}

		// 如果 file_type 为 "file" 且有 file_ids，则处理上传的文件并添加文件内容
		if payload.FileType == "file" && len(payload.FileIDs) > 0 {
			err = processUploadedFiles(db, &payload, apiKey, c)
			if err != nil {
				// processUploadedFiles 已经处理了响应
				return
			}

			// 遍历 VectorFileIds，加载文件内容并添加到 messages 中
			for _, vectorFileId := range payload.VectorFileIds {
				fileContent := loadFileContent(c, vectorFileId, apiKey)
				if fileContent != "" {
					contentMessage := models.StepFunMessage{
						Role: "user",
						Content: []models.StepFunMessageContent{
							{
								Type: "text",
								Text: fileContent,
							},
						},
					}
					messages = append(messages, contentMessage)
				}
			}
		}

		// 最后添加 userMessage
		messages = append(messages, userMessage)

		// 确认模型
		model, err := getModelName(apiKey, messages, payload.FileType, payload.PerformanceLevel)
		if err != nil {
			logrus.Printf("获取模型名称失败: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		stepFunRequest := models.StepFunRequestPayload{
			Model:          model,
			Stream:         true,
			Messages:       messages,
			ToolChoice:     "auto",
			ResponseFormat: models.ResponseFormat{Type: "text"}, // 默认值为 "text"
		}

		// 构建工具列表
		tools := []models.StepFunTool{}

		// 添加 web_search 工具
		if payload.WebSearch {
			webSearchTool := models.StepFunTool{
				Type: "web_search",
				Function: models.StepFunToolFunction{
					Description: "这个工具 web_search 可以用来搜索互联网的信息",
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
						"vector_store_id": payload.VectorStoreID,
						"prompt_template": "从文档 {{knowledge}} 中找到问题 {{query}} 的答案。根据文档内容中的语句找到答案，如果文档中没有答案则告诉用户找不到相关信息；",
					},
				},
			}
			tools = append(tools, retrievalTool)
		} else {
			logrus.Println("未提供 vector_store_id，跳过 retrieval 工具")
		}

		// 将工具添加到请求中
		stepFunRequest.Tools = tools

		// 将请求体编码为 JSON
		stepFunReqBodyBytes, err := json.Marshal(stepFunRequest)
		if err != nil {
			logrus.Printf("序列化 StepFun 请求负载失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
			return
		}

		// 打印发送给 StepFun API 的请求报文
		logrus.Printf("StepFun API 请求报文: %s", stepFunReqBodyBytes)

		// 创建向 StepFun API 发送的 HTTP 请求
		stepFunReq, err := http.NewRequest("POST", "https://api.stepfun.com/v1/chat/completions", bytes.NewBuffer(stepFunReqBodyBytes))
		if err != nil {
			logrus.Printf("创建请求失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
			return
		}

		// 设置请求头
		stepFunReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
		stepFunReq.Header.Set("Content-Type", "application/json")

		// 发送请求
		client := &http.Client{}
		resp, err := client.Do(stepFunReq)
		if err != nil {
			logrus.Printf("发送请求到 StepFun API 失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法与 StepFun API 通信"})
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
			logrus.Printf("StepFun API 返回状态: %s, 信息: %s", resp.Status, errorText)
			c.JSON(resp.StatusCode, gin.H{"error": "StepFun API 错误"})
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
			logrus.Printf("读取 StepFun API 响应时出错: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取 StepFun 响应失败"})
			return
		}

		c.Status(http.StatusOK)
	}
}

// getModelName 根据 FileType 和消息内容选择合适的模型
func getModelName(apiKey string, messages []models.StepFunMessage, fileType, performanceLevel string) (string, error) {
	if fileType == "img" {
		// 尝试使用 step-1v-8k
		tokenCount, err := countTokens(apiKey, messages, "step-1v-8k")
		if err != nil {
			return "", fmt.Errorf("计算 step-1v-8k 的 token 数量失败: %w", err)
		}
		if tokenCount <= 5000 {
			return "step-1v-8k", nil
		} else if tokenCount > 25000 {
			return "", fmt.Errorf("token 数量 %d 超过了 step-1v-32k 的最大限制 25K", tokenCount)
		}

		// 尝试使用 step-1v-32k
		tokenCount, err = countTokens(apiKey, messages, "step-1v-32k")
		if err != nil {
			return "", fmt.Errorf("计算 step-1v-32k 的 token 数量失败: %w", err)
		}
		if tokenCount <= 25000 {
			return "step-1v-32k", nil
		}
		return "", fmt.Errorf("token 数量 %d 超过了 step-1v-32k 的最大限制 25K", tokenCount)
	}
	if fileType == "video" {
		return "step-1.5v-mini", nil
	} else {
		tokenCount, err := countTokens(apiKey, messages, "step-1-flash")
		if err != nil {
			return "", fmt.Errorf("failed to count tokens with step-1-flash: %w", err)
		}
		// 极速模式优先判断是否可以使用 step-1-flash
		if performanceLevel == "fast" && tokenCount <= 10000 {
			// 尝试使用 step-1-flash
			return "step-1-flash", nil

		}
		// 高级模式优先判断是否可以使用 step-2-16k
		if performanceLevel == "advanced" && tokenCount <= 12000 {
			tokenCount, err = countTokens(apiKey, messages, "step-2-16k")
			if err != nil {
				return "", fmt.Errorf("failed to count tokens with step-1v-32k: %w", err)
			}
			if tokenCount <= 12000 {
				return "step-2-16k", nil
			}
		}
		// 均衡模式优先判断是否可以使用 step-1-8k
		if performanceLevel == "balanced" {
			// 使用flash计算的tokenCount初步判断是否可以使用 step-1-8k
			if tokenCount <= 6000 {
				//确认基本符号后再精确计算tokens
				tokenCount, err = countTokens(apiKey, messages, "step-1-8k")
				if err != nil {
					return "", fmt.Errorf("failed to count tokens with step-1-8k: %w", err)
				}
				//最终判断是否可用step-1-8k
				if tokenCount <= 6000 {
					return "step-1-8k", nil
				}
			}
			// 使用flash计算的tokenCount初步判断是否可以使用 step-1-32k
			if tokenCount <= 25000 {
				//确认基本符号后再精确计算tokens
				tokenCount, err = countTokens(apiKey, messages, "step-1-32k")
				if err != nil {
					return "", fmt.Errorf("failed to count tokens with step-1-32k: %w", err)
				}
				//最终判断是否可用step-1-32k
				if tokenCount <= 25000 {
					return "step-1-32k", nil
				}
			}
			// 使用flash计算的tokenCount初步判断是否可以使用 step-1-128k
			if tokenCount <= 80000 {
				//确认基本符号后再精确计算tokens
				tokenCount, err = countTokens(apiKey, messages, "step-1-128k")
				if err != nil {
					return "", fmt.Errorf("failed to count tokens with step-1-128k: %w", err)
				}
				//最终判断是否可用step-1-128k
				if tokenCount <= 80000 {
					return "step-1-128k", nil
				}
			}
			// 使用flash计算的tokenCount初步判断是否可以使用 step-1-256k
			if tokenCount <= 180000 {
				//确认基本符号后再精确计算tokens
				tokenCount, err = countTokens(apiKey, messages, "step-1-256k")
				if err != nil {
					return "", fmt.Errorf("failed to count tokens with step-1-256k: %w", err)
				}
				//最终判断是否可用step-1-256k
				if tokenCount <= 180000 {
					return "step-1-256k", nil
				}
			}

		}
		return "", fmt.Errorf("文本会话会话补全，token count %d  查过了设定的180K的上限", tokenCount)
	}
}

// countTokens 通过调用 StepFun 的 Token 计数 API 计算消息的 Token 数量
func countTokens(apiKey string, messages []models.StepFunMessage, model string) (int, error) {
	requestPayload := models.TokenCountRequest{
		Model:    model,
		Messages: messages,
	}

	reqBodyBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return 0, fmt.Errorf("序列化 token 计数请求失败: %w", err)
	}

	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", apiKey),
		"Content-Type":  "application/json",
	}

	resp, err := SendStepFunRequest("POST", "https://api.stepfun.com/v1/token/count", headers, bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		return 0, fmt.Errorf("发送 token 计数请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return 0, fmt.Errorf("token 计数 API 错误: %s", string(bodyBytes))
	}

	var tokenCountResp models.TokenCountResponse
	err = json.NewDecoder(resp.Body).Decode(&tokenCountResp)
	if err != nil {
		return 0, fmt.Errorf("解析 token 计数响应失败: %w", err)
	}
	logrus.Printf("模型: %s; Token 数量: %d", model, tokenCountResp.Data.TotalTokens)

	return tokenCountResp.Data.TotalTokens, nil
}

// processUploadedFiles 处理 FileType 为 "file" 且提供了 FileIDs 的文件上传逻辑。
func processUploadedFiles(db *dbop.Database, payload *models.RequestPayload, apiKey string, c *gin.Context) error {
	for _, fileID := range payload.FileIDs {
		// 获取上传的文件记录
		fileRecord, err := db.GetUploadedFileByID(fileID)
		if err != nil {
			logrus.Errorf("检索文件记录失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法检索文件信息"})
			return err
		}
		if fileRecord == nil {
			logrus.Printf("未找到 FileID 为 %s 的上传文件", fileID)
			c.JSON(http.StatusBadRequest, gin.H{"error": "上传的文件未找到"})
			return fmt.Errorf("未找到 FileID 为 %s 的上传文件", fileID)
		}
		//将文件根目录，文件路径拼接起来
		filePath := fmt.Sprintf("%s/%s", os.Getenv("file_path"), fileRecord.FilePath)
		// 上传文件到 StepFun 并进行提取
		uploadResp, err := tool.UploadFileToStepFunWithExtract(filePath, fileRecord.Filename)
		if err != nil {
			logrus.Errorf("上传文件到 StepFun 失败: %v", err)
			// 更新文件状态为 "failed"
			if updateErr := db.UpdateUploadedFileStatus(fileID, "failed"); updateErr != nil {
				logrus.Errorf("更新文件状态为 'failed' 失败: %v", updateErr)
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "文件上传失败"})
			return err
		}

		// 轮询文件状态
		status, err := tool.PollFileStatus(uploadResp.ID, 15*time.Second)
		if err != nil {
			logrus.Errorf("查询文件解析状态失败: %v", err)
			return err
		}
		logrus.Printf("文件解析完成，状态: %s", status)

		// 插入文件信息到数据库
		err = db.InsertFile(uploadResp.ID, "local20241015145535", uploadResp.Bytes, fileID, status, "file-extract")
		if err != nil {
			logrus.Errorf("插入文件记录到数据库失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "插入文件记录失败"})
			return err
		}

		// 更新文件状态为 "completed"
		err = db.UpdateUploadedFileStatus(fileID, "completed")
		if err != nil {
			logrus.Errorf("更新文件状态为 'completed' 失败: %v", err)
			// 不返回错误，因为主流程可能仍需继续
		}

		// 将 VectorStoreID 添加到 payload 的 VectorFileIds 中
		payload.VectorFileIds = append(payload.VectorFileIds, uploadResp.ID)
	}
	return nil
}

// loadFileContent 加载文件内容
func loadFileContent(c *gin.Context, vectorFileId, apiKey string) string {
	fileContentURL := fmt.Sprintf("https://api.stepfun.com/v1/files/%s/content", vectorFileId)
	req, err := http.NewRequest("GET", fileContentURL, nil)
	if err != nil {
		logrus.Printf("创建文件内容请求失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return ""
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logrus.Printf("发送文件内容请求失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法检索文件内容"})
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.Printf("检索文件内容失败，状态码: %d", resp.StatusCode)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法检索文件内容"})
		return ""
	}

	fileContentBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logrus.Printf("读取文件内容响应失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取文件内容失败"})
		return ""
	}

	return string(fileContentBytes)
}

// SendStepFunRequest 通用的 HTTP 请求发送函数
func SendStepFunRequest(method, url string, headers map[string]string, body *bytes.Buffer) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}

	return resp, nil
}
