package config

import "strings"

type Gin struct {
	ApiAddr       string `mapstructure:"api-addr"`
	AdminAddr     string `mapstructure:"admin-addr"`
	Mode          string
	ResourcesPath string `mapstructure:"resources-path"`
	TrustProxy    string `mapstructure:"trust-proxy"`
}

func (g Gin) TrustedProxies() []string {
	var proxies []string
	for _, entry := range strings.Split(g.TrustProxy, ",") {
		if proxy := strings.TrimSpace(entry); proxy != "" {
			proxies = append(proxies, proxy)
		}
	}
	return proxies
}
