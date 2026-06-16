package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"mr1v1-collector/internal/wxserver/config"
)

var (
	tokenStore = map[string]string{} // token -> openid
	storeMu    sync.RWMutex
)

type loginRequest struct {
	Code string `json:"code"`
}

func LoginHandler(cfg *config.WxConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		openid, err := fetchOpenID(cfg, req.Code)
		if err != nil {
			http.Error(w, "failed to get openid: "+err.Error(), http.StatusInternalServerError)
			return
		}

		token := uuid.New().String()
		storeMu.Lock()
		tokenStore[token] = openid
		storeMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token})
	}
}

func GetOpenIDByToken(token string) (string, bool) {
	storeMu.RLock()
	defer storeMu.RUnlock()
	openid, ok := tokenStore[token]
	return openid, ok
}

func fetchOpenID(cfg *config.WxConfig, code string) (string, error) {
	url := fmt.Sprintf(
		"https://api.weixin.qq.com/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		cfg.WxAppID, cfg.WxAppSecret, code,
	)
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		OpenID  string `json:"openid"`
		ErrMsg  string `json:"errmsg"`
		ErrCode int    `json:"errcode"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.OpenID == "" {
		return "", fmt.Errorf("wechat error %d: %s", result.ErrCode, result.ErrMsg)
	}
	return result.OpenID, nil
}
