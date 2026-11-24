package admin

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/lib/upload"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpdateProfile 修改当前账号昵称。
// @Tags 个人资料
// @Summary 修改当前账号昵称
// @Accept json
// @Produce json
// @Param body body object true "昵称表单，nickname 为 1 至 32 个字符"
// @Success 200 {object} response.Response{data=admin.LoginPayload}
// @Router /admin/user/profile [put]
// @Security token
func (ct *User) UpdateProfile(c *gin.Context) {
	var form struct {
		Nickname string `json:"nickname"`
	}
	if err := c.ShouldBindJSON(&form); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	form.Nickname = strings.TrimSpace(form.Nickname)
	if n := utf8.RuneCountInString(form.Nickname); n < 1 || n > 32 {
		response.Fail(c, 101, response.TranslateMsg(c, "InvalidNickname"))
		return
	}
	u := service.AllService.UserService.CurUser(c)
	u.Nickname = form.Nickname
	if err := service.AllService.UserService.Update(u, "nickname"); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "OperationFailed"))
		return
	}
	ct.Current(c)
}

// UploadAvatar 上传当前账号头像，替换上一次上传的本地图片。
// @Tags 个人资料
// @Summary 上传头像
// @Description 接受 2 MB 以内且尺寸不超过 4096 × 4096 的 PNG、JPEG、GIF，保存为本地 PNG。
// @Accept multipart/form-data
// @Produce json
// @Param avatar formData file true "头像图片"
// @Success 200 {object} response.Response{data=admin.LoginPayload}
// @Router /admin/user/avatar [post]
// @Security token
func (ct *User) UploadAvatar(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, upload.MaxAvatarBytes+(64<<10))
	if err := c.Request.ParseMultipartForm(upload.MaxAvatarBytes + (64 << 10)); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "AvatarUploadFailed"))
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	file, _, err := c.Request.FormFile("avatar")
	if err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "AvatarMissing"))
		return
	}
	defer file.Close()
	url, err := upload.SaveAvatar(file)
	if err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "AvatarUploadFailed"))
		return
	}
	u := service.AllService.UserService.CurUser(c)
	var current model.User
	var previousAvatar string
	err = global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "avatar").First(&current, u.Id).Error; err != nil {
			return err
		}
		previousAvatar = current.Avatar
		return tx.Model(&current).Update("avatar", url).Error
	})
	if err != nil {
		if cleanupErr := upload.RemoveAvatar(url); cleanupErr != nil {
			global.Logger.WithError(cleanupErr).Warn("Failed to remove unsaved avatar")
		}
		response.Fail(c, 101, response.TranslateMsg(c, "AvatarUploadFailed"))
		return
	}
	if err := upload.RemoveAvatar(previousAvatar); err != nil {
		global.Logger.WithError(err).Warn("Failed to remove previous avatar")
	}
	u.Avatar = url
	ct.Current(c)
}

// Avatar 获取已上传的头像。
// @Tags 个人资料
// @Summary 获取头像图片
// @Produce image/png
// @Param name path string true "头像文件名"
// @Success 200 {file} file "PNG 头像"
// @Failure 404 {string} string "头像不存在"
// @Router /avatar/{name} [get]
func (ct *User) Avatar(c *gin.Context) {
	path := upload.AvatarPath(upload.AvatarURLPrefix + c.Param("name"))
	if path == "" {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.File(path)
}
