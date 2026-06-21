package handlers

import (
	"github.com/gin-gonic/gin"
	"mr1v1-server/internal/resp"
	"mr1v1-server/internal/wxserver/store"
)

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func Auth(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		openid, disabled, ok := s.ResolveSession(c.Request.Context(), token)
		if !ok {
			resp.Fail(c, 401, "unauthorized")
			return
		}
		// 账号被manager后台禁用：跟"没登录"区分开来返回，前端要明确提示联系管理员，
		// 而不是当成普通401静默弹回登录页
		if disabled {
			resp.Fail(c, 403, "account_disabled")
			return
		}
		c.Set("openid", openid)
		c.Next()
	}
}

// OptionalAuth 尝试用token解析openid，没带token或token失效也放行（不返回401），
// 用于游客可访问、但登录用户能额外识别身份的接口（比如房间列表要给登录用户标out哪间是自己的房）。
func OptionalAuth(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != "" {
			if openid, ok := s.GetOpenIDByToken(c.Request.Context(), token); ok {
				c.Set("openid", openid)
			}
		}
		c.Next()
	}
}

// InternalAuth 校验服务间调用的 X-API-Key（manager-backend/consumer 同步
// 比赛结束状态时使用），未配置 key 时直接拒绝，避免裸奔。
func InternalAuth(internalAPIKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if internalAPIKey == "" || c.GetHeader("X-API-Key") != internalAPIKey {
			resp.Fail(c, 401, "unauthorized")
			return
		}
		c.Next()
	}
}

func openid(c *gin.Context) string {
	v, _ := c.Get("openid")
	s, _ := v.(string)
	return s
}
