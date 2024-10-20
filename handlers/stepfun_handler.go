package handlers

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"openapi-cms/dbop"
	"openapi-cms/models"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// 其他结构体定义保持不变，集中在 models.go 中

// handleChatMessagesStepFun 处理 StepFun 聊天消息的请求
func HandleChatMessagesStepFun(db *dbop.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload models.RequestPayload
		if err := c.ShouldBindJSON(&payload); err != nil {
			logrus.Printf("Error binding JSON: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
			return
		}

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
			Content: "你是由阶跃星辰为微信对话框提供的AI图像分析师，善于图片分析，可以分析图片中的文字，地址，建筑，人物，动物，食物，植物等结构清晰的物品。在输出结果的时候请将内容排版的美观，使其在微信中显示时易于阅读。请使用适当的换行和空行，不要包含 `\\n` 等符号。示例格式：",
		}

		// 用户文本消息。如果payload.FileType=""那么就userMessage就用下面的内容
		//userMessage := models.StepFunMessage{
		//	Role:    "user",
		//	Content: payload.Query,
		//}
		//用户图片消息。如果payload.FileType="img"那么就userMessage就用下面的内容结构
		//这里面image/jpg是图片的格式，bstring1和bstring2是图片的base64编码。
		//payload.FileIDs数组里存的是图片的id,具体会传多少图片，由前端来控制，后端不用管。
		//你可以通过这个脚本查到图片的路径和格式。SELECT file_path,file_type FROM uploaded_files where file_id = '38f0f230-d731-4049-b460-162a8a0ff782';
		//{"role": "user", "content":
		//              [
		//                  {"type": "image_url", "image_url": {"url": "data:image/jpg;base64,%s" % (bstring1),"detail": "high"}},
		//                  {"type": "image_url", "image_url": {"url": "data:image/jpg;base64,%s" % (bstring2),"detail": "high"}},
		//                  {"type": "text", "text": payload.Query}
		//              ]
		//           }

		var userMessage models.StepFunMessage

		if payload.FileType == "img" && len(payload.FileIDs) > 0 {
			// 处理图片消息
			var content []models.StepFunMessageContent
			for _, fileID := range payload.FileIDs {
				// 获取上传的文件记录
				uploadedFile, err := db.GetUploadedFileByID(fileID)
				if err != nil {
					logrus.Printf("Error retrieving uploaded file: %v", err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve uploaded file"})
					return
				}
				if uploadedFile == nil {
					logrus.Printf("Uploaded file not found for ID: %s", fileID)
					c.JSON(http.StatusBadRequest, gin.H{"error": "Uploaded file not found"})
					return
				}

				// 读取图片文件并进行 Base64 编码
				imageData, err := ioutil.ReadFile(uploadedFile.FilePath)
				if err != nil {
					logrus.Printf("Error reading image file: %v", err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read image file"})
					return
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

			// 添加文本内容
			textContent := models.StepFunMessageContent{
				Type: "text",
				Text: payload.Query,
			}
			content = append(content, textContent)

			userMessage = models.StepFunMessage{
				Role:    "user",
				Content: content,
			}
		} else {
			// 处理文本消息
			userMessage = models.StepFunMessage{
				Role:    "user",
				Content: payload.Query,
			}
		}

		// 构建初始消息列表
		messages := []models.StepFunMessage{systemMessage, userMessage}

		// 如果前端传了 vector_file_id，那么将文件内容解析并存入 messages 里
		if strings.TrimSpace(payload.VectorFileId) != "" {
			fileContent := looadFileContent(c, payload.VectorFileId, apiKey)
			if fileContent != "" {
				contentMessage := models.StepFunMessage{
					Role:    "user",
					Content: fileContent,
				}
				// 在索引 1 处插入 contentMessage
				messages = append(messages[:1], append([]models.StepFunMessage{contentMessage}, messages[1:]...)...)
			}
		}

		//确认model
		//payload.FileType="img" 输入token小于4k等于4k就选step-1v-8k。输入token大于4k小于等于31k就选step-1v-32k,大于31k就报错
		//file_type不等于img,检查输入token,小于等于2k,就选step-1-flash。大于2k小于等于6k,就选step-1-8k。超过6k小于等于20k,就选step-1-32k。超过20k小于等于100k就选step-1-128k。超过100k小于等于200k就选step-1-256k。大于200k就报错。

		// 计算 Token 数量
		tokenCount, err := countTokens(apiKey, messages)
		if err != nil {
			logrus.Printf("Error counting tokens: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count tokens"})
			return
		}
		//tokenCount := 1000

		logrus.Printf("Token count: %d", tokenCount)

		// 根据 FileType 和 Token 数量选择模型
		var model string
		if payload.FileType == "img" {
			if tokenCount <= 4000 {
				model = "step-1v-8k"
			} else if tokenCount <= 31000 {
				model = "step-1v-32k"
			} else {
				logrus.Printf("Token count %d exceeds maximum for img FileType", tokenCount)
				c.JSON(http.StatusBadRequest, gin.H{"error": "Token count exceeds maximum allowed for image file type"})
				return
			}
		} else {
			if tokenCount <= 2000 {
				model = "step-1-flash"
			} else if tokenCount <= 6000 {
				model = "step-1-8k"
			} else if tokenCount <= 20000 {
				model = "step-1-32k"
			} else if tokenCount <= 100000 {
				model = "step-1-128k"
			} else if tokenCount <= 200000 {
				model = "step-1-256k"
			} else {
				logrus.Printf("Token count %d exceeds maximum for non-img FileType", tokenCount)
				c.JSON(http.StatusBadRequest, gin.H{"error": "Token count exceeds maximum allowed"})
				return
			}
		}

		logrus.Printf("Selected model: %s", model)
		// 构建 StepFunRequestPayload
		stepFunRequest := models.StepFunRequestPayload{
			Model:    model,
			Stream:   true,
			Messages: messages,
		}

		// 构建工具列表
		tools := []models.StepFunTool{}

		// 添加 web_search 工具
		if payload.WebSearch {
			webSearchTool := models.StepFunTool{
				Type: "web_search",
				Function: models.StepFunToolFunction{
					Description: "这个工具可以用来搜索互联网的信息",
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
			//logrus.Printf("StepFun 返回报文: %s", line)
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

// countTokens 通过调用 StepFun 的 Token 计数 API 计算消息的 Token 数量
func countTokens(apiKey string, messages []models.StepFunMessage) (int, error) {
	// 定义请求负载
	requestPayload := models.TokenCountRequest{
		Model:    "step-1v-8k", // 初始模型，可以是任意默认模型
		Messages: messages,
	}

	// 编码为 JSON
	reqBodyBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal token count request: %w", err)
	}

	// 设置请求头
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", apiKey),
		"Content-Type":  "application/json",
	}

	// 使用 SendStepFunRequest 发送请求
	resp, err := SendStepFunRequest("POST", "https://api.stepfun.com/v1/token/count", headers, bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		return 0, fmt.Errorf("failed to send token count request: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		// 读取错误信息
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return 0, fmt.Errorf("token count API error: %s", string(bodyBytes))
	}

	// 解码响应
	var tokenCountResp models.TokenCountResponse
	err = json.NewDecoder(resp.Body).Decode(&tokenCountResp)
	if err != nil {
		return 0, fmt.Errorf("failed to decode token count response: %w", err)
	}

	return tokenCountResp.Data.TotalTokens, nil
}

func looadFileContent(c *gin.Context, VectorFileId, apiKey string) string {
	fileContentURL := fmt.Sprintf("https://api.stepfun.com/v1/files/%s/content", VectorFileId)
	req, err := http.NewRequest("GET", fileContentURL, nil)
	if err != nil {
		logrus.Printf("Error creating file content request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
		return ""
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logrus.Printf("Error sending file content request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve file content"})
		return ""
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		logrus.Printf("Failed to retrieve file content, status code: %d", resp.StatusCode)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve file content"})
		return ""
	}

	// Read the response body
	fileContentBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logrus.Printf("Error reading file content response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file content"})
		return ""
	}
	// Store the file content
	return string(fileContentBytes)
}

// sendStepFunRequest 通用的 HTTP 请求发送函数
func SendStepFunRequest(method, url string, headers map[string]string, body *bytes.Buffer) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	return resp, nil
}
