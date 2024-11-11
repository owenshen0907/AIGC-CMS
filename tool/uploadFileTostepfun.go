package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"openapi-cms/models"
	"os"
	"time"
)

type UploadResponse = models.UploadResponse
type FileStatusResponse = models.FileStatusResponse

// BindFilesToVectorStore 绑定文件 ID 到知识库
func BindFilesToVectorStore(vectorStoreID, fileIDs string) (*UploadResponse, error) {
	//fmt.Sprintf("绑定文件 ID:\"%s\" 到知识库：\"%s\"", fileIDs, vectorStoreID)
	fmt.Println("开始绑定...知识库ID:" + vectorStoreID + "文件ID:" + fileIDs)
	// StepFun API URL
	url := fmt.Sprintf("https://api.stepfun.com/v1/vector_stores/%s/files", vectorStoreID)

	payload := &bytes.Buffer{}
	writer := multipart.NewWriter(payload)
	_ = writer.WriteField("file_ids", fileIDs)

	// 关闭写入器以设置结束边界
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", url, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// 设置必要的头部
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+os.Getenv("STEPFUN_API_KEY"))

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("received non-200 response: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	// 解析响应 JSON
	var uploadResp UploadResponse
	err = json.NewDecoder(resp.Body).Decode(&uploadResp)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response JSON: %w", err)
	}

	return &uploadResp, nil
}

// uploadFileToStepFun 调用外部 StepFun API 上传文件
func UploadFileToStepFunWithExtract(filePath, filename, purpose string) (*FileStatusResponse, error) {
	// StepFun API URL
	url := fmt.Sprintf("https://api.stepfun.com/v1/files")

	// 打开本地文件
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// 创建缓冲区和 multipart 写入器
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	//添加purpose为file-extract这个固定值
	err = writer.WriteField("purpose", purpose)
	if err != nil {
		return nil, fmt.Errorf("failed to add 'purpose' field: %w", err)
	}

	// 添加文件字段
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return nil, fmt.Errorf("failed to copy file data: %w", err)
	}

	// 关闭写入器以设置结束边界
	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", url, &requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// 设置必要的头部
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+os.Getenv("STEPFUN_API_KEY"))

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("received non-200 response: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	// 解析响应 JSON
	var FileStatusResponse models.FileStatusResponse
	err = json.NewDecoder(resp.Body).Decode(&FileStatusResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response JSON: %w", err)
	}

	return &FileStatusResponse, nil
}

// GetFileStatus 获取文件解析的状态
func getFileStatus(fileID string) (string, error) {
	// 构建 StepFun API 的 URL
	url := fmt.Sprintf("https://api.stepfun.com/v1/files/%s", fileID)

	// 创建 HTTP GET 请求
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	// 设置必要的头部
	apiKey := os.Getenv("STEPFUN_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("未设置 STEPFUN_API_KEY 环境变量")
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("执行 HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("接收到非200响应: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	// 解析响应 JSON
	var statusResp models.FileStatusResponse
	err = json.NewDecoder(resp.Body).Decode(&statusResp)
	if err != nil {
		return "", fmt.Errorf("解析响应 JSON 失败: %w", err)
	}

	// 返回状态
	return statusResp.Status, nil
}

// PollFileStatus 每隔一秒查询一次文件状态，直到状态为 "success" 或超过超时时间
func PollFileStatus(fileID string, timeout time.Duration) (string, error) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	timeoutChan := time.After(timeout)

	for {
		select {
		case <-ticker.C:
			status, err := getFileStatus(fileID)
			if err != nil {
				return "", fmt.Errorf("查询文件状态时出错: %w", err)
			}
			if status == "success" {
				return status, nil
			}
			// 您可以根据需要处理其他状态，如 "pending"、"failed" 等
			fmt.Printf("当前文件状态: %s\n", status)
		case <-timeoutChan:
			return "", fmt.Errorf("查询文件状态超时，未在 %v 内完成解析", timeout)
		}
	}
}
