package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const (
	DebugMode     = "debug"
	ReleaseMode   = "release"
	DefaultConfig = "conf/config.yaml"
)

type App struct {
	WebClient        int           `mapstructure:"web-client"`
	ShowSwagger      int           `mapstructure:"show-swagger"`
	TokenExpire      time.Duration `mapstructure:"token-expire"`
	WebSso           bool          `mapstructure:"web-sso"`
	AdminOrigins     string        `mapstructure:"admin-origins"`
	CaptchaThreshold int           `mapstructure:"captcha-threshold"`
	BanThreshold     int           `mapstructure:"ban-threshold"`
}
type Admin struct {
	Title           string `mapstructure:"title"`
	Hello           string `mapstructure:"hello"`
	HelloFile       string `mapstructure:"hello-file"`
	IdServerPort    int    `mapstructure:"id-server-port"`
	RelayServerPort int    `mapstructure:"relay-server-port"`
}
type SMTP struct {
	Host     string `mapstructure:"host"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Port     int    `mapstructure:"port"`
	Crypto   string `mapstructure:"crypto"`
}
type Config struct {
	Lang       string `mapstructure:"lang"`
	App        App
	Admin      Admin
	SMTP       SMTP `mapstructure:"smtp"`
	Gorm       Gorm
	Mysql      Mysql
	Postgresql Postgresql
	Gin        Gin
	Logger     Logger
	Redis      Redis
	Cache      Cache
	Oss        Oss
	Jwt        Jwt
	Capylink   Capylink
	Proxy      Proxy
	Ldap       Ldap
}

func (a *Admin) Init() {
	if a.IdServerPort == 0 {
		a.IdServerPort = DefaultIdServerPort
	}
	if a.RelayServerPort == 0 {
		a.RelayServerPort = DefaultRelayServerPort
	}
}

// Init 初始化配置
func Init(rowVal *Config, path string) *viper.Viper {
	if path == "" {
		path = DefaultConfig
	}
	v := viper.GetViper()
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.SetEnvPrefix("CAPYLINK_API")
	v.SetDefault("app.admin-origins", "")
	v.SetDefault("app.captcha-threshold", -1)
	v.SetDefault("app.ban-threshold", 10)
	v.SetDefault("gin.trust-proxy", "")
	v.SetDefault("jwt.key", "")
	v.SetDefault("jwt.expire-duration", 168*time.Hour)
	v.SetDefault("capylink.webclient-share-api-server", "")
	v.SetDefault("capylink.device-credential-key", "")
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	err := v.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}
	/*
		v.WatchConfig()


			//监听配置修改没什么必要
			v.OnConfigChange(func(e fsnotify.Event) {
				//配置文件修改监听
				fmt.Println("config file changed:", e.Name)
				if err2 := v.Unmarshal(rowVal); err2 != nil {
					fmt.Println(err2)
				}
				rowVal.Capylink.LoadKeyFile()
				rowVal.Capylink.ParsePort()
			})
	*/
	if err := v.Unmarshal(rowVal); err != nil {
		panic(fmt.Errorf("Fatal error config: %s \n", err))
	}
	if rowVal.Jwt.Key != "" && rowVal.Jwt.ExpireDuration <= 0 {
		panic("jwt.expire-duration must be positive when JWT is enabled")
	}
	if _, err := rowVal.Capylink.DeviceCredentialKeyBytes(); err != nil {
		panic(err)
	}
	if address := strings.TrimSpace(rowVal.Capylink.WebclientShareApiServer); address != "" {
		base, err := url.Parse(address)
		if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" || base.User != nil || strings.ContainsAny(address, "?#") {
			panic("capylink.webclient-share-api-server must be an absolute HTTP(S) URL without user information, query, or fragment")
		}
		if port := base.Port(); port != "" {
			if _, err := strconv.ParseUint(port, 10, 16); err != nil {
				panic("capylink.webclient-share-api-server has an invalid port")
			}
		}
		rowVal.Capylink.WebclientShareApiServer = strings.TrimRight(base.String(), "/")
	} else {
		rowVal.Capylink.WebclientShareApiServer = ""
	}
	if rowVal.App.BanThreshold > 0 && rowVal.App.CaptchaThreshold >= rowVal.App.BanThreshold {
		panic(fmt.Errorf("app.captcha-threshold (%d) must be lower than app.ban-threshold (%d) when both are enabled",
			rowVal.App.CaptchaThreshold, rowVal.App.BanThreshold))
	}
	rowVal.Capylink.LoadKeyFile()
	rowVal.Admin.Init()
	return v
}

// ReadEnv 读取环境变量
func ReadEnv(rowVal interface{}) *viper.Viper {
	v := viper.New()
	v.AutomaticEnv()
	if err := v.Unmarshal(rowVal); err != nil {
		fmt.Println(err)
	}
	return v
}
