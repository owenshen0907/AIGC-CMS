package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"openapi-cms/models"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// 其他结构体定义保持不变，集中在 models.go 中

// handleChatMessagesStepFun 处理 StepFun 聊天消息的请求
func HandleChatMessagesStepFun(c *gin.Context) {
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
	logrus.Printf("StepFun 请求报文: %s", string(reqBodyBytes))

	apiKey := os.Getenv("STEPFUN_API_KEY")
	if apiKey == "" {
		logrus.Println("STEPFUN_API_KEY is not set")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server configuration error"})
		return
	}

	// 构建 StepFunRequestPayload
	stepFunRequest := models.StepFunRequestPayload{
		Model:  "step-1-128k", // 根据需要选择模型
		Stream: true,
	}

	// 系统消息
	systemMessage := models.StepFunMessage{
		Role:    "system",
		Content: "你是由阶跃星辰为微信对话框提供的AI图像分析师，善于图片分析，可以分析图片中的文字，地址，建筑，人物，动物，食物，植物等结构清晰的物品。在输出结果的时候请将内容排版的美观，使其在微信中显示时易于阅读。请使用适当的换行和空行，不要包含 `\\n` 等符号。示例格式：",
	}

	// 用户消息
	userMessage := models.StepFunMessage{
		Role:    "user",
		Content: payload.Query,
	}

	//如果前端传了vector_file_id,那么就将文件内容进行解析
	//解析的方式是
	//curl https://api.stepfun.com/v1/files/file-abc123/content \
	//-H "Authorization: Bearer $STEP_API_KEY"
	//file-abc123可以用payload.vector_file_id替代
	//返回的值用fileContent进行存储
	if strings.TrimSpace(payload.VectorFileId) != "" {
		fileContent := looadFileContent(c, payload.VectorFileId, apiKey)
		// 用户content消息
		contentMessage := models.StepFunMessage{
			Role:    "user",
			Content: fileContent,
		}
		// 如果文件内容不为空，则添加contentMessage到将消息添加到stepFunRequest.Messages中
		if fileContent != "" {
			stepFunRequest.Messages = append(stepFunRequest.Messages, systemMessage, userMessage, contentMessage)
		} else {
			stepFunRequest.Messages = []models.StepFunMessage{systemMessage, userMessage}
		}
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
