// models.go
package models

import "database/sql"

// DatabaseInterface 定义数据库操作接口
type DatabaseInterface interface {
	InsertVectorStore(id, name, DisplayName, description, tags, ModelOwner, creator_id string) error
	GetKnowledgeBaseByID(id string) (*KnowledgeBase, error)
	GetKnowledgeBaseByName(name string) (*KnowledgeBase, error)
	UpdateKnowledgeBaseByName(name, displayName, description, tags, modelOwner string) error
	UpdateKnowledgeBaseIDByName(name string, id string) error
	UpdateKnowledgeBase(id, displayName, description, tags string) error
	//InsertUploadedFile(fileID, fileName, filePath, fileType, fileDescription string) error
	GetUploadedFileByID(fileID string) (*UploadedFile, error)
	UpdateUploadedFileStatus(fileID, status string) error
	InsertFile(id, vectorStoreID string, usageBytes int, fileID, status, purpose string) error
	Query(query string, args ...interface{}) (*sql.Rows, error)
	Close() error
	// 事务管理方法
	BeginTransaction() (*sql.Tx, error)
	CommitTransaction(tx *sql.Tx) error
	RollbackTransaction(tx *sql.Tx) error
	// 新增事务内插入方法
	InsertUploadedFileTx(tx *sql.Tx, fileID, fileName, filePath, fileType, fileDescription string) error
	InsertFileKnowledgeRelationTx(tx *sql.Tx, fileID, knowledgeBaseID string) error
}

// KnowledgeBase 定义知识库结构体

// RequestPayload 定义了，选择stepfun时，接收自前端的请求结构
type RequestPayload struct {
	Inputs         interface{} `json:"inputs,omitempty"`
	Query          string      `json:"query,omitempty"`
	ResponseMode   string      `json:"response_mode,omitempty"`
	ConversationID string      `json:"conversation_id,omitempty"`
	User           string      `json:"user,omitempty"`
	//Files          []File      `json:"files,omitempty"`
	FileIDs       []string `json:"file_ids,omitempty"`
	FileType      string   `json:"file_type"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tags          string   `json:"tags"`            // 标签以逗号分隔的字符串
	VectorStoreID string   `json:"vector_store_id"` // 新增字段，用于传递 vector_store_id
	ModelOwner    string   `json:"model_owner"`     // 新增字段
	WebSearch     bool     `json:"web_search"`
	VectorFileIds []string `json:"vector_file_ids,omitempty"`
}

// File 定义文件结构
//type File struct {
//	Type           string `json:"type"`
//	TransferMethod string `json:"transfer_method"`
//	URL            string `json:"url"`
//}

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

// StepFunToolFunction 定义工具的功能描述及可选参数
type StepFunToolFunction struct {
	Description    string            `json:"description"`
	Options        map[string]string `json:"options,omitempty"` // 可选项是map类型，存储工具的配置参数
	PromptTemplate string            `json:"prompt_template,omitempty"`
}

// StepFunTool 定义工具结构
type StepFunTool struct {
	Type     string              `json:"type"`     // 工具类型
	Function StepFunToolFunction `json:"function"` // 工具的功能
}

// StepFunRequestPayload 定义发送到 StepFun API 的请求结构
type StepFunRequestPayload struct {
	Model    string           `json:"model"`  // "step-1v-8k"
	Stream   bool             `json:"stream"` // true
	Messages []StepFunMessage `json:"messages"`
	Tools    []StepFunTool    `json:"tools,omitempty"` // 新增字段，用于存储工具
}

// StepFunResponse 定义 StepFun API 的响应结构-创建知识库
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

// TokenCountRequest represents the request payload for token counting
type TokenCountRequest struct {
	Model    string           `json:"model"`
	Messages []StepFunMessage `json:"messages"`
}

// TokenCountResponseData represents the 'data' field in the token count response
type TokenCountResponseData struct {
	TotalTokens int `json:"total_tokens"`
}

// TokenCountResponse represents the response from the token counting API
type TokenCountResponse struct {
	Data TokenCountResponseData `json:"data"`
}

// KnowledgeBase 定义知识库结构体
type KnowledgeBase struct {
	ID          string `json:"id"`
	Name        string `json:"name"`         // 知识库标识
	DisplayName string `json:"display_name"` // 知识库名称
	Description string `json:"description"`
	Tags        string `json:"tags"`
	CreatedAt   string `json:"created_at"`
	ModelOwner  string `json:"model_owner"` // 归属模型：stepfun，zhipu, moonshot, baichuan
	CreatorID   string `json:"creator_id"`
}

// UploadedFile 接收：前端请求，上传文件到后台
type UploadedFile struct {
	FileID      string `json:"file_id"`
	Filename    string `json:"file_name"`
	FilePath    string `json:"file_path"`
	FileType    string `json:"file_type"`
	Description string `json:"file_description"`
	Status      string `json:"status"`
	UploadTime  string `json:"upload_time"`
}

// FileStatusResponse 请求： StepFun API ,获取：doc parser上传文件的响应，和获取文件状态响应
type FileStatusResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Bytes     int    `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
	Status    string `json:"status"`
}

// TriggerUploadRequest 接收：前端请求，上传文件到stepfun.
// 【知识库页面上传文件】
type TriggerUploadRequest struct {
	ModelOwner    string `json:"model_owner" binding:"required"`
	FileID        string `json:"file_id" binding:"required"`
	Purpose       string `json:"purpose" binding:"required"`
	VectorStoreID string `json:"vectorStoreID" binding:"required"`
}

// UploadResponse 用于stepfun API 上传文件到知识库。返回的文件id,知识库使用体积，知识库ID
type UploadResponse struct {
	ID            string `json:"id"`
	UsageBytes    int    `json:"usage_bytes"`
	VectorStoreID string `json:"vector_store_id"`
}
