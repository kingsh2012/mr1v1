package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"mr1v1-server/internal/wxserver/config"
	"mr1v1-server/internal/wxserver/store"
)

type loginRequest struct {
	Code string `json:"code"`
}

func LoginHandler(cfg *config.WxConfig, s *store.Store) http.HandlerFunc {
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

		if err := s.UpsertUser(r.Context(), openid); err != nil {
			slog.Warn("upsert wx_user failed", "openid", openid, "err", err)
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		token := uuid.New().String()
		if err := s.CreateSession(r.Context(), token, openid); err != nil {
			slog.Warn("create session failed", "err", err)
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token, "openid": openid})
	}
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
