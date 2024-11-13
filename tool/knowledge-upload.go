// tool/knowledge-uploads.go
package tool

import (
	"fmt"
	"github.com/joho/godotenv"
	"io"
	"mime/multipart"
	"net/http"
	"openapi-cms/dbop"
	"openapi-cms/middleware"
	"openapi-cms/models"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// 初始化加载环境变量（如果尚未在应用程序其他部分加载）
func init() {
	err := godotenv.Load()
	if err != nil {
		logrus.Warn("未能加载 .env 文件，使用系统环境变量")
	}
}

// HandleUploadFile 处理上传文件的请求
func HandleUploadFile(c *gin.Context, db *dbop.Database) {
	// 获取必要的表单参数
	vectorStoreID, fileDescription, modelOwner, err := getFormParams(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 获取上传目录
	uploadDir := getUploadDir()

	// 从表单中获取文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		logrus.Errorf("Error retrieving the file: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "File is required"})
		return
	}
	defer file.Close()

	// 获取文件大小
	fileSize := header.Size

	// 从上下文中获取用户名，如果未找到则返回错误
	userName, exists := middleware.GetUserName(c)
	if !exists || userName == "" {
		logrus.Warn("未能获取用户名")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: username not found"})
		return
	}
	// 检查 model_owner 的值
	if err := validateModelOwner(modelOwner, c); err != nil {
		return
	}
	// 判断文件是否已存在
	uploadedFile, err := db.GetUploadedFileByFileNameSizeUsername(header.Filename, userName, fileSize)
	if err != nil {
		logrus.Errorf("判断用户名下是否已经上传过该文件报错: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "判断用户名下是否已经上传过该文件报错"})
		return
	}
	fmt.Print(uploadedFile)
	// 处理已存在的文件
	if uploadedFile != nil {
		file_web_host := os.Getenv("FILE_WEB_HOST")
		//拼接文件路径
		file_web_path := fmt.Sprintf("%s%s", file_web_host, uploadedFile[0].FilePath)
		//判断是否为文件，如果是再进行下一步，否则直接，跳过。（图片视频等无需解析或retrieval）
		if isTextFile(header.Filename) {
			stepFileStatus := ""
			fileStepFileID := ""
			// 检查每个文件的 purpose 是否为 "retrieval"
			//var retrievals []RetrievalFileInfo
			for _, file := range uploadedFile {
				if file.StepFilePurpose == "retrieval" && file.StepVectorID == vectorStoreID {
					stepFileStatus = file.StepFileStatus
					fileStepFileID = file.StepFileID
					fmt.Println("在知识库下匹配到了同意图的文件")
				}
			}
			//此文件已经在知识库下，并解析完成，跳过上传
			if stepFileStatus == "completed" {
				c.JSON(http.StatusOK, gin.H{
					"status": "此文件已经在知识库下，解析完成，跳过上传",
				})
				return
			}
			//此文件已经在知识库下，在解析中，我将查询最新的解析状态
			if stepFileStatus == "processing" {
				c.JSON(http.StatusOK, gin.H{
					"status": "此文件已经在知识库下，在解析中，请在页面点击【更新状态】查询最新的解析状态",
				})
				return
			}
			//此文件已经在知识库下，但为跟知识库进行绑定，执行向量化动作
			if stepFileStatus == "uploaded" {
				_, err = BindFilesToVectorStore(vectorStoreID, fileStepFileID)
				if err != nil {
					logrus.Errorf("绑定文件到向量库报错: %v", err)
					c.JSON(http.StatusBadRequest, gin.H{"error": "绑定文件到向量库报错"})
					return
				}
				c.JSON(http.StatusOK, gin.H{
					"status": "此文件此前已经在知识库下，但未绑定知识库，现已进行绑定，请点击【更新状态】查询最新的解析状态",
				})
				return
			}
			//此文件已上传，但未在此知识库下，将进行上传，解析，更新状态
			handleExistingFile(uploadedFile[0], vectorStoreID, uploadDir, file_web_path, c, db)
			return
		} else {
			c.JSON(http.StatusOK, gin.H{
				"file_id":       uploadedFile[0].FileID,
				"status":        "此文件该用户已经上传，直接使用历史文件。待发送打消息窗口后再获取文件内容",
				"file_web_path": file_web_path,
			})
			return
		}
	}
	// 处理新文件上传（文件未上传过）
	if err := processNewFileUpload(c, db, file, header, uploadDir, userName, vectorStoreID, fileDescription, fileSize); err != nil {
		// 错误已在函数内部处理
		return
	}
}

// 获取表单参数
func getFormParams(c *gin.Context) (vectorStoreID, fileDescription, modelOwner string, err error) {
	vectorStoreID = c.PostForm("vector_store_id")
	if vectorStoreID == "" {
		return "", "", "", fmt.Errorf("向量知识库ID为必填")
	}

	fileDescription = c.PostForm("file_description")

	modelOwner = c.PostForm("model_owner")
	if modelOwner == "" {
		return "", "", "", fmt.Errorf("model_owner is required")
	}

	return vectorStoreID, fileDescription, modelOwner, nil
}

// 获取上传目录
func getUploadDir() string {
	uploadDir := os.Getenv("FILE_PATH")
	if uploadDir == "" {
		uploadDir = "./uploads" // 默认值
		logrus.Warn("环境变量 'FILE_PATH' 未设置，使用默认路径 './uploads'")
	}
	return uploadDir
}

// 验证 model_owner
func validateModelOwner(modelOwner string, c *gin.Context) error {
	switch modelOwner {
	case "local", "stepfun":
		// 允许的 model_owner，继续处理
		return nil
	default:
		// 不支持的 model_owner，返回错误
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("暂不支持 model_owner 为 '%s' 的知识库文件上传，待接入后再实现", modelOwner),
		})
		return fmt.Errorf("unsupported model_owner")
	}
}

// 处理已存在的文件
func handleExistingFile(uploadedFile *models.UploadedFile, vectorStoreID, uploadDir, file_web_path string, c *gin.Context, db *dbop.Database) {
	//file_web_host := os.Getenv("FILE_WEB_HOST")
	//如果是聊天窗口上传的文件
	if vectorStoreID == "local" {
		logrus.Info("聊天窗口上传的文件，直接返回upload表的文件ID")
		//拼接文件路径
		//file_web_path := fmt.Sprintf("%s%s", file_web_host, uploadedFile.FilePath)
		c.JSON(http.StatusOK, gin.H{
			"file_id": uploadedFile.FileID,
			"status":  "此文件该用户已经上传，直接使用历史文件。待发送打消息窗口后再获取文件内容",
			//"file_path":         uploadedFile.FilePath,
			"file_web_path": file_web_path,
			//"step_vector_id":    uploadedFile.StepVectorID,
			//"step_file_id":      uploadedFile.StepFileID,
			//"step_file_purpose": uploadedFile.StepFilePurpose,
			//"step_file_status":  uploadedFile.StepFileStatus,
		})
		return
	} else {
		//处理知识库上传的文件
		//logrus.Info("从知识库上传的文件，需要调stepfun接口上传文件去向量化")
		//判断文件是否被同时用作解析和retrieval

		//判断文件已经上传给step且类型为retrieval
		if vectorStoreID == uploadedFile.StepVectorID && uploadedFile.StepFilePurpose == "retrieval" {
			logrus.Info("文件已向量化，无需任何处理")
			c.JSON(http.StatusOK, gin.H{
				"status": "文件已向量化，无需任何处理",
			})
			return
		} else {
			//如果知识库或者类型有一个对不上，就说明该文件虽然上传过，但不再同一个知识库，或者不是retrieval用途，则需要上传文件到stepfun
			// 获取上传目录
			//uploadDir := getUploadDir()
			FilePath := filepath.Join(uploadDir, uploadedFile.FilePath)
			//向调用doc parser上传文件
			uploadResp, err := UploadFileToStepFunWithExtract(FilePath, uploadedFile.Filename, "retrieval")
			if err != nil {
				logrus.Errorf("上传文件到stepfun报错 %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "上传文件到stepfun报错"})
				return
			}
			// 更新数据库中的 files 表，存储 API 返回的文件信息，增加 fileID
			err = db.InsertFile(uploadResp.ID, vectorStoreID, uploadResp.Bytes, uploadedFile.FileID, "processing", "retrieval")
			if err != nil {
				logrus.Errorf("插入files表，retrieval的文件数据 %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "插入files表，retrieval的文件数据报错"})
				return
			}
			//再绑定文件到知识库。确保文件进行向量化
			//fileIDs := []string{uploadedFile.StepFileID}
			_, err = BindFilesToVectorStore(vectorStoreID, uploadedFile.StepFileID)
			if err != nil {
				logrus.Errorf("绑定文件到知识库报错 %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "绑定文件到知识库报错"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"status": "文件绑定到知识库，正常向量化中，可以在知识库点击获取向量化状态，获取向量化进度",
			})
		}
	}
}

// 处理新文件上传
func processNewFileUpload(
	c *gin.Context,
	db *dbop.Database,
	file multipart.File,
	header *multipart.FileHeader,
	uploadDir, userName, vectorStoreID, fileDescription string,
	fileSize int64,
) (err error) {
	currentDate := time.Now().Format("2006-01-02")
	targetDir := filepath.Join(uploadDir, userName, currentDate)

	// 创建上传目录
	if err = os.MkdirAll(targetDir, os.ModePerm); err != nil {
		logrus.Errorf("创建上传目录失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法创建上传目录"})
		return
	}

	// 确保文件名安全
	fileName := filepath.Base(header.Filename)
	filePath := filepath.Join(targetDir, fileName)
	fmt.Printf("filePath:%v\n", filePath)

	// 计算相对文件路径
	relativeFilePath, err := filepath.Rel(uploadDir, filePath)
	fmt.Printf("relativeFilePath:%v\n", relativeFilePath)
	if err != nil {
		logrus.Errorf("计算相对文件路径时出错: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "路径处理错误"})
		return
	}
	file_web_host := os.Getenv("FILE_WEB_HOST")
	file_web_path := fmt.Sprintf("%s%s", file_web_host, relativeFilePath)

	// 创建文件
	out, err := os.Create(filePath)
	if err != nil {
		logrus.Errorf("创建文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建文件失败"})
		return
	}
	defer out.Close()

	// 写入文件内容
	if _, err = io.Copy(out, file); err != nil {
		logrus.Errorf("写入文件到磁盘失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入文件到磁盘失败"})
		return
	}

	// 生成文件ID和类型
	fileID := uuid.New().String()
	fileType := header.Header.Get("Content-Type")

	// 开始数据库事务，只处理必要的数据库操作
	tx, err := db.BeginTransaction()
	if err != nil {
		logrus.WithError(err).Error("启动事务时出错")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库错误"})
		return
	}

	// 确保事务在函数结束时正确处理
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			logrus.Errorf("捕获到 panic: %v", p)
			err = fmt.Errorf("panic: %v", p)
		} else if err != nil {
			tx.Rollback()
		} else {
			commitErr := db.CommitTransaction(tx)
			if commitErr != nil {
				logrus.WithError(commitErr).Error("提交事务时出错")
				err = commitErr
			}
		}
	}()

	// 插入上传文件信息
	if err = db.InsertUploadedFileTx(tx, fileID, fileName, relativeFilePath, fileType, fileDescription, userName, fileSize); err != nil {
		logrus.Errorf("将上传文件记录插入数据库时出错: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法存储文件信息"})
		return
	}

	// 提交事务
	err = db.CommitTransaction(tx)
	if err != nil {
		logrus.Errorf("提交事务时出错: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库提交错误"})
		return
	}

	// 根据 vectorStoreID 处理不同逻辑，外部 API 调用移出事务之外
	if vectorStoreID == "local" {
		c.JSON(http.StatusOK, gin.H{
			"file_id":       fileID,
			"status":        "文件已上传",
			"file_web_path": file_web_path,
		})
		return
	}

	// 调用外部 API 上传文件到 StepFun
	uploadResp, err := UploadFileToStepFunWithExtract(filePath, fileName, "retrieval")
	if err != nil {
		logrus.Errorf("上传文件到stepfun报错 %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "上传文件到stepfun报错"})
		return
	}

	// 插入 files 表的数据，这里可以使用新的事务
	tx, err = db.BeginTransaction()
	if err != nil {
		logrus.WithError(err).Error("启动事务时出错")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库错误"})
		return
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			logrus.Errorf("捕获到 panic: %v", p)
			err = fmt.Errorf("panic: %v", p)
		} else if err != nil {
			tx.Rollback()
		} else {
			commitErr := db.CommitTransaction(tx)
			if commitErr != nil {
				logrus.WithError(commitErr).Error("提交事务时出错")
				err = commitErr
			}
		}
	}()

	// 插入 files 表
	err = db.InsertFile(uploadResp.ID, vectorStoreID, uploadResp.Bytes, fileID, "uploaded", "retrieval")
	if err != nil {
		logrus.Errorf("插入files表，retrieval的文件数据 %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "插入files表，retrieval的文件数据报错"})
		return
	}

	// 绑定文件到知识库
	_, err = BindFilesToVectorStore(vectorStoreID, uploadResp.ID)
	if err != nil {
		logrus.Errorf("绑定文件到知识库报错 %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "绑定文件到知识库报错"})
		return
	}

	// 更新 files 状态为 processing
	err = db.UpdateFilesStatus(fileID, "processing")
	if err != nil {
		logrus.Errorf("更新files状态为processing %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新files状态为processing报错"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "文件绑定到知识库，正常向量化中，可以在知识库点击获取向量化状态，获取向量化进度",
	})
	return
}

// 判断是否为文本文件的函数
func isTextFile(fileName string) bool {
	fileExt := filepath.Ext(fileName)
	return fileExt == ".txt" || fileExt == ".md" || fileExt == ".pdf" ||
		fileExt == ".doc" || fileExt == ".docx" || fileExt == ".xls" ||
		fileExt == ".xlsx" || fileExt == ".ppt" || fileExt == ".pptx" ||
		fileExt == ".csv" || fileExt == ".html" || fileExt == ".htm" ||
		fileExt == ".xml"
}
