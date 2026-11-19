package router

import (
	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/controller/web"
	"net/http"
	"path"
)

func WebInit(g *gin.Engine) {
	i := &web.Index{}
	g.GET("/", i.Index)

	if global.Config.App.WebClient == 1 {
		g.GET("/webclient-config/index.js", i.ConfigJs)
	}

	if global.Config.App.WebClient == 1 {
		g.StaticFS("/webclient", http.Dir(global.Config.Gin.ResourcesPath+"/web"))
		g.StaticFS("/webclient2", http.Dir(global.Config.Gin.ResourcesPath+"/web2"))
	}
	admin := g.Group("/_admin", func(c *gin.Context) {
		switch path.Clean(c.Param("filepath")) {
		case "/", "/index.html":
			c.Header("Content-Security-Policy", "frame-ancestors 'none'")
		}
		c.Next()
	})
	admin.StaticFS("/", http.Dir(global.Config.Gin.ResourcesPath+"/admin"))
}
