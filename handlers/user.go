// handlers/user.go
package handlers

import (
	"net/http"
	"openapi-cms/dbop"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// HandleValidateUser 处理验证用户并返回用户名的请求
func HandleValidateUser(db *dbop.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从上下文中获取用户名
		usernameInterface, exists := c.Get("userName")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token中未找到用户名"})
			return
		}

		username, ok := usernameInterface.(string)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token中的用户名格式不正确"})
			return
		}

		// 查询数据库中是否存在该用户
		user, err := dbop.GetUserByUsername(db, username)
		if err != nil {
			logrus.Errorf("数据库错误: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库错误"})
			return
		}

		if user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "用户不存在"})
			return
		}

		// 返回用户名
		c.JSON(http.StatusOK, gin.H{"username": user.Username})
	}
}
