// models.go
package main

import (
	"bytes"
	"fmt"
	"net/http"
)

// DatabaseInterface 定义数据库操作接口
type DatabaseInterface interface {
	InsertVectorStore(id, name, DisplayName, description, tags string) error
	// 添加其他需要的方法
}

// KnowledgeBase 定义知识库结构体

// RequestPayload 定义接收自前端的请求结构
type RequestPayload struct {
	Inputs         interface{} `json:"inputs,omitempty"`
	Query          string      `json:"query,omitempty"`
	ResponseMode   string      `json:"response_mode,omitempty"`
	ConversationID string      `json:"conversation_id,omitempty"`
	User           string      `json:"user,omitempty"`
	Files          []File      `json:"files,omitempty"`
	Name           string      `json:"name"`
	Description    string      `json:"description"`
	Tags           string      `json:"tags"` // 标签以逗号分隔的字符串
}

// File 定义文件结构
type File struct {
	Type           string `json:"type"`
	TransferMethod string `json:"transfer_method"`
	URL            string `json:"url"`
}

// StepFunMessageContent 定义消息内容结构
type StepFunMessageContent struct {
	Type     string                  `json:"type"` // "text" 或 "image_url"
	Text     string                  `json:"text,omitempty"`
	ImageURL *StepFunMessageImageURL `json:"image_url,omitempty"`
}

// StepFunMessageImageURL 定义图片 URL 结构
type StepFunMessageImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail"`
}

// StepFunMessage 定义消息结构
type StepFunMessage struct {
	Role    string      `json:"role"`    // "system" 或 "user"
	Content interface{} `json:"content"` // 对于 "system" 是字符串，对于 "user" 是 []StepFunMessageContent
}

// StepFunRequestPayload 定义发送到 StepFun API 的请求结构
type StepFunRequestPayload struct {
	Model    string           `json:"model"`  // "step-1v-8k"
	Stream   bool             `json:"stream"` // true
	Messages []StepFunMessage `json:"messages"`
}

// StepFunResponse 定义 StepFun API 的响应结构
type StepFunResponse struct {
	ID            string `json:"id"`
	UsageBytes    int    `json:"usage_bytes,omitempty"`
	VectorStoreID string `json:"vector_store_id,omitempty"`
}

// DifyResponse 定义 Dify API 的响应结构
type DifyResponse struct {
	TaskID         string `json:"task_id"`
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	Answer         string `json:"answer"`
	CreatedAt      int64  `json:"created_at"`
	Usage          struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// sendStepFunRequest 通用的 HTTP 请求发送函数
func sendStepFunRequest(method, url string, headers map[string]string, body *bytes.Buffer) (*http.Response, error) {
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
