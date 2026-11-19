package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/service"
)

func requireAdminLogin(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, response.Response{
		Code:    http.StatusUnauthorized,
		Message: response.TranslateMsg(c, "NeedLogin"),
	})
}

// BackendUserAuth 后台权限验证中间件
func BackendUserAuth() gin.HandlerFunc {
	return func(c *gin.Context) {

		//测试先关闭
		token := c.GetHeader("api-token")
		if token == "" {
			requireAdminLogin(c)
			return
		}
		user, ut := service.AllService.UserService.InfoByAccessToken(token)
		if user.Id == 0 {
			requireAdminLogin(c)
			return
		}

		if !service.AllService.UserService.CheckUserEnable(user) {
			c.JSON(401, gin.H{
				"error": "Unauthorized",
			})
			c.Abort()
			return
		}

		c.Set("curUser", user)
		c.Set("token", token)
		//如果时间小于1天,token自动续期
		service.AllService.UserService.AutoRefreshAccessToken(ut)

		c.Next()
	}
}
