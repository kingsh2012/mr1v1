package config

import "os"

type WxConfig struct {
	WxAppID        string
	WxAppSecret    string
	BackendURL     string
	Port           string
	DatabaseURL    string // PostgreSQL DSN
	InternalAPIKey string // 与 backend 共享的内部调用 key
}

func LoadWx() *WxConfig {
	return &WxConfig{
		WxAppID:        getEnv("WX_APP_ID", ""),
		WxAppSecret:    getEnv("WX_APP_SECRET", ""),
		BackendURL:     getEnv("BACKEND_URL", "http://127.0.0.1:8181"),
		Port:           getEnv("WX_PORT", "8082"),
		DatabaseURL:    getEnv("DATABASE_URL", ""),
		InternalAPIKey: getEnv("INTERNAL_API_KEY", ""),
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
