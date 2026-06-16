package config

import "os"

type WxConfig struct {
	WxAppID        string
	WxAppSecret    string
	GameServerAddr string
	Port           string
}

func LoadWx() *WxConfig {
	return &WxConfig{
		WxAppID:        getEnv("WX_APP_ID", ""),
		WxAppSecret:    getEnv("WX_APP_SECRET", ""),
		GameServerAddr: getEnv("GAME_SERVER_ADDR", "ws://localhost:9000"),
		Port:           getEnv("WX_PORT", "8082"),
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
