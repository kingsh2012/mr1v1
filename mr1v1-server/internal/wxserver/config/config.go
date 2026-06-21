package config

import (
	"os"
	"strconv"
)

type WxConfig struct {
	WxAppID          string
	WxAppSecret      string
	BackendURL       string
	Port             string
	DatabaseURL      string // PostgreSQL DSN
	InternalAPIKey   string // 与 backend 共享的内部调用 key
	RoomStaleMinutes int    // 房间无状态变化且无人在线超过此时长，自动软删除
	AvatarsDir       string // 上传头像落盘目录
	PublicURL        string // 外部可访问的本服务域名，用于拼出头像的永久URL
}

func LoadWx() *WxConfig {
	return &WxConfig{
		WxAppID:          getEnv("WX_APP_ID", ""),
		WxAppSecret:      getEnv("WX_APP_SECRET", ""),
		BackendURL:       getEnv("BACKEND_URL", "http://127.0.0.1:8181"),
		Port:             getEnv("WX_PORT", "8082"),
		DatabaseURL:      getEnv("DATABASE_URL", ""),
		InternalAPIKey:   getEnv("INTERNAL_API_KEY", ""),
		RoomStaleMinutes: getEnvInt("ROOM_STALE_MINUTES", 30),
		AvatarsDir:       getEnv("AVATARS_DIR", "./data/avatars"),
		PublicURL:        getEnv("WX_PUBLIC_URL", "https://mr1v1.smarteamlab.com"),
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}
