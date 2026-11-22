package admin

import (
	"strings"

	"github.com/lejianwen/rustdesk-api/v2/model"
)

type LoginPayload struct {
	Id         uint     `json:"id"`
	Username   string   `json:"username"`
	Email      string   `json:"email"`
	Avatar     string   `json:"avatar"`
	Token      string   `json:"token"`
	RouteNames []string `json:"route_names"`
	Nickname   string   `json:"nickname"`
}

func (lp *LoginPayload) FromUser(user *model.User) {
	lp.Id = user.Id
	lp.Username = user.Username
	lp.Email = maskEmail(user.Email)
	lp.Avatar = user.Avatar
	lp.Nickname = strings.TrimSpace(user.Nickname)
	if lp.Nickname == "" {
		lp.Nickname = nicknameFromEmail(user.Email)
	}
}

// maskEmail keeps the domain and the first and last local-part characters so
// the account remains recognizable without returning the full address.
func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return ""
	}
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return "***"
	}

	local := []rune(email[:at])
	if len(local) == 1 {
		return "*@" + email[at+1:]
	}
	if len(local) == 2 {
		return string(local[0]) + "*@" + email[at+1:]
	}
	return string(local[0]) + strings.Repeat("*", len(local)-2) + string(local[len(local)-1]) + "@" + email[at+1:]
}

func nicknameFromEmail(email string) string {
	email = strings.TrimSpace(email)
	if at := strings.IndexByte(email, '@'); at > 0 {
		return email[:at]
	}
	return ""
}

type UserOauthItem struct {
	Op     string `json:"op"`
	Status int    `json:"status"`
}
