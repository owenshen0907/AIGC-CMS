// middleware/jwt.go
package middleware

import (
	"net/http"
	"os"
	// "strings"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5" // 导入 JWT 包
)

func JWTMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, err := c.Cookie("jwtToken")
		if err != nil {
			fmt.Println("未找到 jwtToken Cookie:", err)
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		fmt.Println("收到的 JWT 令牌:", tokenString)

		// 解析和验证 JWT 令牌
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			// 检查签名方法
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				fmt.Printf("意外的签名方法: %v\n", token.Header["alg"])
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(os.Getenv("JWT_SECRET")), nil
		})

		if err != nil {
			fmt.Println("JWT 令牌解析错误:", err)
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			fmt.Println("JWT 令牌声明:", claims)
			c.Set("userID", claims["userID"])
			c.Next()
		} else {
			fmt.Println("无效的 JWT 令牌声明")
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
	}
}
