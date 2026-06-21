package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"mr1v1-server/internal/resp"
	"mr1v1-server/internal/wxserver/config"
	"mr1v1-server/internal/wxserver/store"
)

func Login(cfg *config.WxConfig, s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Code string `json:"code" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			resp.Fail(c, 400, "code required")
			return
		}

		openid, err := fetchOpenID(cfg, req.Code)
		if err != nil {
			resp.Fail(c, 500, "failed to get openid: "+err.Error())
			return
		}

		if err := s.UpsertUser(c.Request.Context(), openid); err != nil {
			slog.Warn("upsert wx_user failed", "openid", openid, "err", err)
			resp.Fail(c, 500, "db error")
			return
		}

		// 被禁用的账号不让登录成功（UpsertUser的ON CONFLICT分支不会动status，
		// 被封的账号每次登录都会卡在这里），给个能让前端区分出来的明确提示
		u, err := s.GetUser(c.Request.Context(), openid)
		if err == nil && u != nil && u.Status != "enabled" {
			resp.Fail(c, 403, "account_disabled")
			return
		}

		token := uuid.New().String()
		if err := s.CreateSession(c.Request.Context(), token, openid); err != nil {
			slog.Warn("create session failed", "err", err)
			resp.Fail(c, 500, "db error")
			return
		}

		resp.OK(c, gin.H{"token": token, "openid": openid})
	}
}

func fetchOpenID(cfg *config.WxConfig, code string) (string, error) {
	url := fmt.Sprintf(
		"https://api.weixin.qq.com/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		cfg.WxAppID, cfg.WxAppSecret, code,
	)
	httpResp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return "", err
	}
	defer httpResp.Body.Close()

	body, _ := io.ReadAll(httpResp.Body)
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
