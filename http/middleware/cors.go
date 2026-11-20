package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"golang.org/x/net/idna"
)

func AdminOriginAllowed(c *gin.Context, origin string) bool {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := c.GetHeader("X-Forwarded-Proto"); (forwarded == "http" || forwarded == "https") && isTrustedProxy(c.Request.RemoteAddr) {
		scheme = forwarded
	}
	if normalized := normalizeOrigin(origin); normalized != "" && normalized == normalizeOrigin(scheme+"://"+c.Request.Host) {
		return true
	}
	return AdminRedirectOriginAllowed(origin)
}

func isTrustedProxy(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	addr := net.ParseIP(host)
	for _, proxy := range global.Config.Gin.TrustedProxies() {
		if ip := net.ParseIP(proxy); ip != nil && ip.Equal(addr) {
			return true
		}
		if _, network, err := net.ParseCIDR(proxy); err == nil && network.Contains(addr) {
			return true
		}
	}
	return false
}

func urlOrigin(u *url.URL) string {
	hostname := u.Hostname()
	if u.User != nil || hostname == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.ToLower(u.Host)
	if literal, suffix, ok := strings.Cut(host, "]"); ok && strings.HasPrefix(literal, "[") {
		if addr, err := netip.ParseAddr(literal[1:]); err == nil && addr.Is6() && addr.Zone() == "" {
			host = "[" + addr.String() + "]" + suffix
		}
	} else if strings.IndexFunc(hostname, func(r rune) bool { return r > unicode.MaxASCII }) >= 0 {
		ascii, err := idna.Lookup.ToASCII(hostname)
		if err != nil || ascii == "" {
			return ""
		}
		host = ascii + strings.TrimPrefix(u.Host, hostname)
	}
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		host = strings.TrimSuffix(host, ":"+port)
	}
	return u.Scheme + "://" + host
}

func normalizeOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path != "" || strings.ContainsAny(raw, "?#") {
		return ""
	}
	return urlOrigin(u)
}

// AdminRedirectOriginAllowed 只允许配置中的 OAuth 控制台回跳来源。
func AdminRedirectOriginAllowed(origin string) bool {
	origin = normalizeOrigin(origin)
	if origin == "" {
		return false
	}
	apiURL, err := url.Parse(global.Config.Capylink.ApiServer)
	if err == nil && origin == urlOrigin(apiURL) {
		return true
	}
	for _, allowed := range strings.Split(global.Config.App.AdminOrigins, ",") {
		if origin == normalizeOrigin(strings.TrimSpace(allowed)) {
			return true
		}
	}
	return false
}

func AdminRedirectAllowed(redirect, origin string) bool {
	target, err := url.Parse(redirect)
	return err == nil && !strings.ContainsAny(redirect, "?#") && urlOrigin(target) == normalizeOrigin(origin) && AdminRedirectOriginAllowed(origin)
}

// AdminCors 在登录限制器之前处理管理端请求，包括浏览器预检。
func AdminCors() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path != "/api/admin" && !strings.HasPrefix(c.Request.URL.Path, "/api/admin/") {
			c.Next()
			return
		}
		origin := c.GetHeader("Origin")
		c.Header("Vary", "Origin")
		if origin != "" {
			if !AdminOriginAllowed(c, origin) {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Header("Access-Control-Allow-Headers", "api-token,content-type,authorization ")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
