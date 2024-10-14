// dify_handler.go
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// handleChatMessagesDify 处理 Dify 聊天消息的请求
func handleChatMessagesDify(c *gin.Context) {
	var payload RequestPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		logrus.Printf("Error binding JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	// 将请求体编码为 JSON
	reqBodyBytes, err := json.Marshal(payload)
	if err != nil {
		logrus.Printf("Error marshaling JSON: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
		return
	}

	// 打印请求报文
	logrus.Printf("Dify 请求报文: %s", string(reqBodyBytes))

	apiKey := os.Getenv("DIFY_API_KEY")
	if apiKey == "" {
		logrus.Println("DIFY_API_KEY is not set")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server configuration error"})
		return
	}

	// 创建向 Dify API 发送的 HTTP 请求
	difyReq, err := http.NewRequest("POST", "https://api.dify.ai/v1/chat-messages", bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		logrus.Printf("Error creating request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
		return
	}

	// 设置请求头
	difyReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	difyReq.Header.Set("Content-Type", "application/json")

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(difyReq)
	if err != nil {
		logrus.Printf("Error sending request to Dify API: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to communicate with Dify API"})
		return
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		errorText := ""
		if resp.Body != nil {
			bodyBytes, _ := io.ReadAll(resp.Body)
			errorText = string(bodyBytes)
		}
		logrus.Printf("Dify API returned status: %s, message: %s", resp.Status, errorText)
		c.JSON(resp.StatusCode, gin.H{"error": "Dify API error"})
		return
	}

	// 设置响应头以支持流式传输
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	// 逐行读取响应体并解析有效负载
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 5 && line[:5] == "data:" {
			line = line[5:]
			var difyResponse DifyResponse
			err = json.Unmarshal([]byte(line), &difyResponse)
			if err != nil {
				logrus.Printf("Error decoding Dify response line: %v", err)
				logrus.Printf("Dify response line: %s", line)
				continue
			}

			// 构建 StepFun 风格的返回给前端的报文
			response := struct {
				ID      string `json:"id"`
				Created int64  `json:"created"`
				Model   string `json:"model"`
				Choices []struct {
					Index        int    `json:"index"`
					FinishReason string `json:"finish_reason"`
					Delta        struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
				Usage struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
			}{
				ID:      difyResponse.TaskID,
				Created: difyResponse.CreatedAt,
				Model:   "dify",
				Choices: []struct {
					Index        int    `json:"index"`
					FinishReason string `json:"finish_reason"`
					Delta        struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"delta"`
				}{
					{
						Index:        0,
						FinishReason: "stop",
						Delta: struct {
							Role    string `json:"role"`
							Content string `json:"content"`
						}{
							Role:    "assistant",
							Content: difyResponse.Answer,
						},
					},
				},
				Usage: struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				}{
					PromptTokens:     difyResponse.Usage.PromptTokens,
					CompletionTokens: difyResponse.Usage.CompletionTokens,
					TotalTokens:      difyResponse.Usage.TotalTokens,
				},
			}

			// 打印返回给前端的报文
			logrus.Printf("Dify 返回前端的 StepFun 风格报文: %v", response)

			// 转发每一行到前端
			fmt.Fprintf(c.Writer, "data: %s\n\n", toJSONString(response))
			// 刷新缓冲区
			c.Writer.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		logrus.Printf("Error reading Dify response body: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read Dify response"})
		return
	}

	c.Status(http.StatusOK)
}
