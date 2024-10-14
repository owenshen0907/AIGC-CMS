package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// 其他结构体定义保持不变，集中在 models.go 中

// handleChatMessagesStepFun 处理 StepFun 聊天消息的请求
func handleChatMessagesStepFun(c *gin.Context) {
	var payload RequestPayload
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
	stepFunRequest := StepFunRequestPayload{
		Model:  "step-2-16k-nightly", // 根据需要选择模型
		Stream: true,
	}

	// 系统消息
	systemMessage := StepFunMessage{
		Role:    "system",
		Content: "你是由阶跃星辰为微信对话框提供的AI图像分析师，善于图片分析，可以分析图片中的文字，地址，建筑，人物，动物，食物，植物等结构清晰的物品。在输出结果的时候请将内容排版的美观，使其在微信中显示时易于阅读。请使用适当的换行和空行，不要包含 `\\n` 等符号。示例格式：",
	}

	// 用户消息
	userMessage := StepFunMessage{
		Role:    "user",
		Content: payload.Query,
	}

	// 将消息添加到请求中
	stepFunRequest.Messages = []StepFunMessage{systemMessage, userMessage}

	// 将请求体编码为 JSON
	stepFunReqBodyBytes, err := json.Marshal(stepFunRequest)
	if err != nil {
		logrus.Printf("Error marshaling StepFun request payload: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server error"})
		return
	}

	// 打印发送给 StepFun API 的请求报文
	logrus.Printf("StepFun API 请求报文: %s", string(stepFunReqBodyBytes))

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
