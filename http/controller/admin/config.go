package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
)

type Config struct {
}

type ldapConfigForm struct {
	Revision      *uint64               `json:"revision"`
	Enable        *bool                 `json:"enable"`
	Url           string                `json:"url"`
	TlsCaFile     string                `json:"tls_ca_file"`
	TlsVerify     *bool                 `json:"tls_verify"`
	BaseDn        string                `json:"base_dn"`
	BindDn        string                `json:"bind_dn"`
	BindPassword  *string               `json:"bind_password"`
	AdminPassword string                `json:"admin_password"`
	User          model.LdapUserSetting `json:"user"`
}

// ServerConfig 卡皮远控服务配置
// @Tags ADMIN
// @Summary 卡皮远控服务配置
// @Description 返回原始 id_server、浏览器信令入口 ws_host 及公钥和中继配置；ws_host 来自运行时配置，为空时浏览器沿用 id_server。
// @Accept  json
// @Produce  json
// @Success 200 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /admin/config/server [get]
// @Security token
func (co *Config) ServerConfig(c *gin.Context) {
	mustLogin := true
	cf := &response.ServerConfigResponse{
		IdServer:    global.Config.Capylink.IdServer,
		WsHost:      global.Config.Capylink.WsHost,
		Key:         global.Config.Capylink.Key,
		RelayServer: global.Config.Capylink.RelayServer,
		ApiServer:   global.Config.Capylink.ApiServer,
		MustLogin:   &mustLogin,
	}
	response.Success(c, cf)
}

// AppConfig APP服务配置
// @Tags ADMIN
// @Summary APP服务配置
// @Description APP服务配置
// @Accept  json
// @Produce  json
// @Success 200 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /admin/config/app [get]
// @Security token
func (co *Config) AppConfig(c *gin.Context) {
	registerEnabled, err := service.RegistrationState()
	if err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "OperationFailed"))
		return
	}
	response.Success(c, &gin.H{
		"web_client": global.Config.App.WebClient,
		"register":   registerEnabled,
	})
}

// UpdateAppConfig updates administrator-controlled platform settings.
func (co *Config) UpdateAppConfig(c *gin.Context) {
	var form struct {
		Register *bool `json:"register"`
	}
	if err := c.ShouldBindJSON(&form); err != nil || form.Register == nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	if err := service.SetRegistrationEnabled(*form.Register); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "OperationFailed"))
		return
	}
	response.Success(c, gin.H{"register": *form.Register})
}

// LdapConfig returns the persisted LDAP directory configuration.
func (co *Config) LdapConfig(c *gin.Context) {
	setting, err := service.GetLdapSetting()
	if err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, err.Error()))
		return
	}
	response.Success(c, gin.H{
		"revision":          setting.Revision,
		"enable":            setting.Enable,
		"url":               setting.Url,
		"tls_ca_file":       setting.TlsCaFile,
		"tls_verify":        setting.TlsVerify,
		"base_dn":           setting.BaseDn,
		"bind_dn":           setting.BindDn,
		"bind_password_set": setting.BindPassword != "",
		"user":              setting.User,
	})
}

// UpdateLdapConfig persists and activates the LDAP directory configuration.
func (co *Config) UpdateLdapConfig(c *gin.Context) {
	var form ldapConfigForm
	if err := c.ShouldBindJSON(&form); err != nil || form.Enable == nil || form.Revision == nil || form.TlsVerify == nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	setting := model.LdapSetting{
		Revision:  *form.Revision,
		Enable:    *form.Enable,
		Url:       form.Url,
		TlsCaFile: form.TlsCaFile,
		TlsVerify: *form.TlsVerify,
		BaseDn:    form.BaseDn,
		BindDn:    form.BindDn,
		User:      form.User,
	}
	admin := service.AllService.UserService.CurUser(c)
	if err := service.SaveLdapSetting(setting, form.BindPassword, admin, form.AdminPassword); err != nil {
		message := "OperationFailed"
		code := 101
		switch err {
		case service.ErrLdapSettingsChanged:
			code = http.StatusConflict
			message = err.Error()
		case service.ErrLdapSettingInvalid, service.ErrLdapBindingPasswordRequired, service.ErrLdapSettingsTableMissing, service.ErrLdapEnableAttrPairRequired,
			service.ErrLdapLocalAdminRequired, service.ErrLdapDirectoryUnavailable:
			message = err.Error()
		}
		response.Fail(c, code, response.TranslateMsg(c, message))
		return
	}
	response.Success(c, gin.H{"enable": setting.Enable})
}

// AdminConfig ADMIN服务配置
// @Tags ADMIN
// @Summary ADMIN服务配置
// @Description ADMIN服务配置
// @Accept  json
// @Produce  json
// @Success 200 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /admin/config/admin [get]
// @Security token
func (co *Config) AdminConfig(c *gin.Context) {
	response.Success(c, &gin.H{"title": global.Config.Admin.Title})
}
