package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
)

func Limiter() gin.HandlerFunc {
	return limitLogin("Banned", func(c *gin.Context, message string) {
		response.Fail(c, http.StatusLocked, message)
	})
}

func NativeLoginLimiter() gin.HandlerFunc {
	return limitLogin("NativeLoginTemporarilyLimited", response.Error)
}

func limitLogin(message string, reject func(*gin.Context, string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		banned, _ := global.LoginLimiter.CheckSecurityStatus(c.ClientIP())
		if banned {
			reject(c, response.TranslateMsg(c, message))
			c.Abort()
			return
		}
		c.Next()
	}
}
