package api

import (
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/request/api"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	apiResp "github.com/lejianwen/rustdesk-api/v2/http/response/api"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
	"github.com/lejianwen/rustdesk-api/v2/utils"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"gorm.io/gorm"
)

type Oauth struct {
}

// OidcAuth
// @Tags Oauth
// @Summary OidcAuth
// @Description OidcAuth
// @Accept  json
// @Produce  json
// @Success 200 {object} apiResp.LoginRes
// @Failure 500 {object} response.ErrorResponse
// @Router /oidc/auth [post]
func (o *Oauth) OidcAuth(c *gin.Context) {
	f := &api.OidcAuthRequest{}
	err := c.ShouldBindJSON(f)
	if err != nil {
		response.Error(c, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	if f.DeviceInfo.Type == model.LoginLogClientWebAdmin {
		response.Error(c, response.TranslateMsg(c, "ParamsError"))
		return
	}

	oauthService := service.AllService.OauthService

	err, state, verifier, nonce, url := oauthService.BeginAuth(f.Op)
	if err != nil {
		response.Error(c, response.TranslateMsg(c, err.Error()))
		return
	}

	service.AllService.OauthService.SetOauthCache(state, &service.OauthCacheItem{
		Action:     service.OauthActionTypeLogin,
		Id:         string(f.Id),
		Op:         f.Op,
		Uuid:       string(f.Uuid),
		DeviceName: f.DeviceInfo.Name,
		DeviceOs:   f.DeviceInfo.Os,
		DeviceType: f.DeviceInfo.Type,
		Verifier:   verifier,
		Nonce:      nonce,
	}, 5*60)
	//fmt.Println("code url", code, url)
	c.JSON(http.StatusOK, gin.H{
		"code": state,
		"url":  url,
	})
}

func (o *Oauth) OidcAuthQueryPre(c *gin.Context, webAdmin bool) (*model.User, *model.UserToken) {
	q := &api.OidcAuthQuery{}

	if err := c.ShouldBind(q); err != nil {
		response.Error(c, response.TranslateMsg(c, "ParamsError")+": "+err.Error())
		return nil, nil
	}

	// 获取 OAuth 缓存
	oauthService := service.AllService.OauthService
	v := oauthService.GetOauthCache(q.Code)
	if v == nil {
		response.Error(c, response.TranslateMsg(c, "OauthExpired"))
		return nil, nil
	}
	if v.Action != service.OauthActionTypeLogin || webAdmin != (v.DeviceType == model.LoginLogClientWebAdmin) || (webAdmin && (v.Ticket == "" || subtle.ConstantTimeCompare([]byte(q.Ticket), []byte(v.Ticket)) != 1)) {
		response.Error(c, response.TranslateMsg(c, "ParamsError"))
		return nil, nil
	}

	// 如果 UserId 为 0，说明还在授权中
	if v.UserId == 0 {
		if v.DeviceType == model.LoginLogClientWebAdmin {
			response.Success(c, gin.H{"pending": true})
			return nil, nil
		}
		//fix: 1.4.2 webclient oidc
		c.JSON(http.StatusOK, gin.H{"message": "Authorization in progress, please login and bind", "error": "No authed oidc is found"})
		return nil, nil
	}

	claimed := *v
	claimed.Redeeming = true
	if v.Redeeming || !oauthService.ReplaceOauthCache(q.Code, v, &claimed) {
		if oauthService.GetOauthCache(q.Code) == nil {
			response.Error(c, response.TranslateMsg(c, "OauthExpired"))
		} else {
			c.JSON(http.StatusServiceUnavailable, response.ErrorResponse{Error: response.TranslateMsg(c, "SystemError")})
		}
		return nil, nil
	}
	defer oauthService.ConsumeOauthCache(q.Code, &claimed)

	u := &model.User{}
	err := service.DB.First(u, claimed.UserId).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, response.TranslateMsg(c, "UserNotFound"))
		return nil, nil
	}
	if err != nil {
		oauthService.ReplaceOauthCache(q.Code, &claimed, v)
		c.JSON(http.StatusInternalServerError, response.ErrorResponse{Error: response.TranslateMsg(c, "SystemError")})
		return nil, nil
	}
	if !service.AllService.UserService.CheckUserEnable(u) {
		response.Error(c, response.TranslateMsg(c, "UserDisabled"))
		return nil, nil
	}

	// 创建登录日志并生成用户令牌
	ut, err := service.AllService.UserService.Login(u, &model.LoginLog{
		UserId:   u.Id,
		Client:   claimed.DeviceType,
		DeviceId: model.DeviceIdentity(claimed.Id),
		Uuid:     model.DeviceIdentity(claimed.Uuid),
		Ip:       c.ClientIP(),
		Type:     model.LoginLogTypeOauth,
		Platform: claimed.DeviceOs,
	}, !webAdmin)

	if err != nil {
		// 恢复原状态，保留原来的过期时间。
		oauthService.ReplaceOauthCache(q.Code, &claimed, v)
		c.JSON(http.StatusInternalServerError, response.ErrorResponse{Error: response.TranslateMsg(c, "SystemError")})
		return nil, nil
	}

	// 返回用户令牌
	return u, ut
}

// OidcAuthQuery
// @Tags Oauth
// @Summary OidcAuthQuery
// @Description OidcAuthQuery
// @Accept  json
// @Produce  json
// @Success 200 {object} apiResp.LoginRes
// @Failure 500 {object} response.ErrorResponse
// @Router /oidc/auth-query [get]
func (o *Oauth) OidcAuthQuery(c *gin.Context) {
	u, ut := o.OidcAuthQueryPre(c, false)
	if u == nil || ut == nil {
		return
	}
	c.JSON(http.StatusOK, apiResp.LoginRes{
		AccessToken: ut.Token,
		Type:        "access_token",
		User:        *(&apiResp.UserPayload{}).FromUser(u),
	})
}

// OauthCallback 回调
// @Tags Oauth
// @Summary OauthCallback
// @Description OauthCallback
// @Accept  json
// @Produce  json
// @Success 200 {object} apiResp.LoginRes
// @Failure 500 {object} response.ErrorResponse
// @Router /oidc/callback [get]
func (o *Oauth) OauthCallback(c *gin.Context) {
	state := c.Query("state")
	if state == "" {
		c.HTML(http.StatusOK, "oauth_fail.html", gin.H{
			"message":     "ParamIsEmpty",
			"sub_message": "state",
		})
		return
	}
	cacheKey := state
	oauthService := service.AllService.OauthService
	//从缓存中获取
	oauthCache := oauthService.GetOauthCache(cacheKey)
	if oauthCache == nil {
		c.HTML(http.StatusOK, "oauth_fail.html", gin.H{
			"message": "OauthExpired",
		})
		return
	}
	webAdmin := oauthCache.DeviceType == model.LoginLogClientWebAdmin
	action := oauthCache.Action
	if (webAdmin || action == service.OauthActionTypeBind) && oauthCache.RedirectURL == "" {
		c.HTML(http.StatusOK, "oauth_fail.html", gin.H{"message": "ParamsError"})
		return
	}
	nonce := oauthCache.Nonce
	op := oauthCache.Op
	verifier := oauthCache.Verifier
	var user *model.User
	// 获取用户信息
	code := c.Query("code")
	err, oauthUser, identity := oauthService.Callback(code, verifier, op, nonce)
	if err != nil {
		if action == service.OauthActionTypeBind {
			oauthBindResult(c, oauthCache.RedirectURL, "OauthFailed")
			return
		}
		c.HTML(http.StatusOK, "oauth_fail.html", gin.H{
			"message":     "OauthFailed",
			"sub_message": err.Error(),
		})
		return
	}
	userId := oauthCache.UserId
	openid := oauthUser.OpenId
	if action == service.OauthActionTypeBind {

		//fmt.Println("bind", ty, userData)
		// 检查此openid是否已经绑定过
		utr := oauthService.UserThirdInfo(op, openid)
		if utr.UserId > 0 {
			oauthBindResult(c, oauthCache.RedirectURL, "OauthHasBindOtherUser")
			return
		}
		//绑定
		err := oauthService.BindOauthUser(userId, oauthUser, op, identity)
		if err != nil {
			message := "BindFail"
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				message = "ItemNotFound"
			case errors.Is(err, service.ErrOauthIdentityChanged):
				message = "OauthExpired"
			}
			oauthBindResult(c, oauthCache.RedirectURL, message)
			return
		}
		oauthBindResult(c, oauthCache.RedirectURL, "BindSuccess")
		return

	} else if action == service.OauthActionTypeLogin {
		//登录
		if userId != 0 || oauthCache.OpenId != "" {
			c.HTML(http.StatusOK, "oauth_fail.html", gin.H{
				"message": "OauthHasBeenSuccess",
			})
			return
		}
		var autoRegister bool
		user, autoRegister, err = service.AllService.UserService.InfoByOauthId(op, openid, identity)
		if err != nil {
			c.HTML(http.StatusOK, "oauth_fail.html", gin.H{"message": "OauthExpired"})
			return
		}
		if user == nil {
			if !autoRegister {
				//c.String(http.StatusInternalServerError, "还未绑定用户，请先绑定")
				pending := *oauthCache
				pending.UpdateFromOauthUser(oauthUser)
				pending.ProviderIdentity = identity
				if !oauthService.ReplaceOauthCache(cacheKey, oauthCache, &pending) {
					c.HTML(http.StatusOK, "oauth_fail.html", gin.H{"message": "OauthExpired"})
					return
				}
				if webAdmin {
					c.Redirect(http.StatusFound, oauthCache.RedirectURL+"#/oauth/bind/"+cacheKey)
				} else {
					c.Redirect(http.StatusFound, "/_admin/#/oauth/bind/"+cacheKey)
				}
				return
			}

			//自动注册
			err, user = service.AllService.UserService.RegisterByOauth(oauthUser, op, identity)
			if err != nil {
				message := err.Error()
				if errors.Is(err, service.ErrOauthIdentityChanged) {
					message = "OauthExpired"
				}
				c.HTML(http.StatusOK, "oauth_fail.html", gin.H{
					"message": message,
				})
				return
			}
		}
		confirmed := *oauthCache
		if webAdmin {
			confirmed.Ticket = utils.RandomString(32)
			if confirmed.Ticket == "" {
				c.HTML(http.StatusOK, "oauth_fail.html", gin.H{"message": "OauthFailed"})
				return
			}
		}
		confirmed.UserId = user.Id
		if !oauthService.ReplaceOauthCache(cacheKey, oauthCache, &confirmed) {
			c.HTML(http.StatusOK, "oauth_fail.html", gin.H{"message": "OauthExpired"})
			return
		}
		if webAdmin {
			c.Redirect(http.StatusFound, confirmed.RedirectURL+"#/login?oidc_ticket="+confirmed.Ticket)
			return
		}
		c.HTML(http.StatusOK, "oauth_success.html", gin.H{
			"message": "OauthSuccess",
		})
		return
	} else {
		c.HTML(http.StatusOK, "oauth_fail.html", gin.H{
			"message": "ParamsError",
		})
		return
	}

}

func oauthBindResult(c *gin.Context, redirectURL, message string) {
	target := redirectURL + "#/my/oauth"
	if message != "BindSuccess" {
		target += "?binding_error=1"
	}
	c.Redirect(http.StatusFound, target)
}

type MessageParams struct {
	Lang  string `json:"lang" form:"lang"`
	Title string `json:"title" form:"title"`
	Msg   string `json:"msg" form:"msg"`
}

func (o *Oauth) Message(c *gin.Context) {
	mp := &MessageParams{}
	if err := c.ShouldBindQuery(mp); err != nil {
		return
	}
	localizer := global.Localizer(mp.Lang)
	res := ""
	if mp.Title != "" {
		title, err := localizer.LocalizeMessage(&i18n.Message{
			ID: mp.Title,
		})
		if err == nil {
			res = utils.StringConcat(";title='", title, "';")
		}

	}
	if mp.Msg != "" {
		msg, err := localizer.LocalizeMessage(&i18n.Message{
			ID: mp.Msg,
		})
		if err == nil {
			res = utils.StringConcat(res, "msg = '", msg, "';")
		}
	}

	//返回js内容
	c.Header("Content-Type", "application/javascript")
	c.String(http.StatusOK, res)
}
