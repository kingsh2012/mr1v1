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
		openid, ok := s.GetOpenIDByToken(c.Request.Context(), token)
		if !ok {
			resp.Fail(c, 401, "unauthorized")
			return
		}
		c.Set("openid", openid)
		c.Next()
	}
}

func openid(c *gin.Context) string {
	v, _ := c.Get("openid")
	s, _ := v.(string)
	return s
}
