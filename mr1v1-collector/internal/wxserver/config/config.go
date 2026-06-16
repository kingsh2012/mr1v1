package config

import "os"

type WxConfig struct {
	WxAppID        string
	WxAppSecret    string
	BackendURL     string // mr1v1-backend HTTP 地址，如 http://127.0.0.1:8181
	Port           string
}

func LoadWx() *WxConfig {
	return &WxConfig{
		WxAppID:     getEnv("WX_APP_ID", ""),
		WxAppSecret: getEnv("WX_APP_SECRET", ""),
		BackendURL:  getEnv("BACKEND_URL", "http://127.0.0.1:8181"),
		Port:        getEnv("WX_PORT", "8082"),
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
