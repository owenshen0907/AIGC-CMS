// main.go
package main

import (
	"log"
	"openapi-cms/dbop"
	"openapi-cms/handlers"
	"openapi-cms/tool"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

func main() {
	// 加载环境变量
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file")
	}

	// 初始化日志
	configureLogger()

	// 在非生产环境下打印 API 密钥（发布前请确保 ENV 设置为 "production"）
	if os.Getenv("ENV") != "production" {
		logrus.Infof("DIFY_API_KEY: %s", os.Getenv("DIFY_API_KEY"))
		logrus.Infof("STEPFUN_API_KEY: %s", os.Getenv("STEPFUN_API_KEY"))
	}

	// 初始化数据库
	db, err := dbop.NewDatabase()
	if err != nil {
		logrus.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// 初始化 Gin 路由器
	router := gin.Default()

	// 配置 CORS 中间件
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"}, // 根据需要修改
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// 定义路由并注入依赖
	api := router.Group("/api")
	{
		// 创建向量存储，使用闭包传递 dbop
		api.POST("/create-vector-store", func(c *gin.Context) {
			handlers.HandleCreateVectorStore(c, db)
		})
		// 更新向量存储，使用闭包传递 dbop
		api.PUT("/update-vector-store/:name", func(c *gin.Context) {
			handlers.HandleUpdateKnowledgeBase(c, db)
		})

		// 聊天消息处理器
		api.POST("/chat-messages/dify", handlers.HandleChatMessagesDify)
		api.POST("/chat-messages/stepfun", handlers.HandleChatMessagesStepFun)

		// 获取数据，使用闭包传递 dbop
		api.GET("/get-data", dbop.HandleGetData(db))
		// 新增路由，获取某个知识库下的文件信息
		api.GET("/knowledge-bases/:id/files", dbop.GetFilesByKnowledgeBaseID(db))
		// 新增上传文件路由，使用闭包传递 dbop
		api.POST("/knowledge-uploads-file", func(c *gin.Context) {
			tool.HandleUploadFile(c, db)
		})
	}

	// 监听端口
	port := os.Getenv("PORT")
	if port == "" {
		port = "4000"
	}
	if err := router.Run(":" + port); err != nil {
		logrus.Fatalf("Failed to run server: %v", err)
	}
}

// configureLogger 设置日志配置
func configureLogger() {
	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.InfoLevel)
}
