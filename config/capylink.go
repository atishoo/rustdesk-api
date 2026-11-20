package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

const (
	DefaultIdServerPort    = 21116
	DefaultRelayServerPort = 21117
)

type Capylink struct {
	IdServer        string `mapstructure:"id-server"`
	MustLogin       *bool  `mapstructure:"must-login"`
	IdServerPort    int    `mapstructure:"-"`
	RelayServer     string `mapstructure:"relay-server"`
	RelayServerPort int    `mapstructure:"-"`
	ApiServer       string `mapstructure:"api-server"`
	Key             string `mapstructure:"key"`
	KeyFile         string `mapstructure:"key-file"`
	Personal        int    `mapstructure:"personal"`
	//webclient-magic-queryonline
	WebclientMagicQueryonline int    `mapstructure:"webclient-magic-queryonline"`
	WebclientShareApiServer   string `mapstructure:"webclient-share-api-server"`
	WsHost                    string `mapstructure:"ws-host"`
}

func (rd *Capylink) LoadKeyFile() {
	if rd.Key != "" || rd.KeyFile == "" {
		return
	}

	// The full-s6 services can start before the bundled server publishes its key.
	waitForBundledKey := os.Getenv("CAPYLINK_API_S6_LOCAL_CONTROL") == "1" && rd.KeyFile == "/data/id_ed25519.pub"
	if waitForBundledKey && os.Getenv("ENCRYPTED_ONLY") != "1" {
		// Without -k _, hbbs can take an inline key and never create a file.
		var key string
		// hbbs checks the exact spellings first, then sorts mixed-case aliases.
		for _, name := range []string{"KEY", "key", "KEy", "KeY", "Key", "kEY", "kEy", "keY"} {
			if value, ok := os.LookupEnv(name); ok {
				key = value
				break
			}
		}
		// hbbs applies only top-level .env settings after the process environment.
		if file, err := ini.LoadSources(ini.LoadOptions{
			InsensitiveKeys:        true,
			IgnoreInlineComment:    true,
			AllowNonUniqueSections: true,
		}, "/data/.env"); err == nil {
			if entry, err := file.Section("").GetKey("KEY"); err == nil {
				key = entry.Value()
			}
		}
		if key != "" && key != "-" && key != "_" {
			privateKey, err := base64.StdEncoding.DecodeString(key)
			if err != nil {
				privateKey, err = base64.RawStdEncoding.DecodeString(key)
			}
			if err == nil && len(privateKey) == ed25519.PrivateKeySize {
				rd.Key = base64.StdEncoding.EncodeToString(privateKey[ed25519.SeedSize:])
				return
			}
			waitForBundledKey = false
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		b, err := os.ReadFile(rd.KeyFile)
		if err == nil {
			rd.Key = strings.TrimRight(string(b), "\r\n")
		}
		if rd.Key != "" || !waitForBundledKey {
			return
		}
		if !time.Now().Before(deadline) {
			panic("bundled capylink public key /data/id_ed25519.pub is not ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
