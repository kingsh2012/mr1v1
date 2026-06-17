package handlers

import (
	"github.com/gin-gonic/gin"
	"mr1v1-server/internal/resp"
	"mr1v1-server/internal/wxserver/store"
)

func GetUser(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		oid := openid(c)
		u, err := s.GetUser(c.Request.Context(), oid)
		if err != nil {
			resp.Fail(c, 500, "db error")
			return
		}
		var steamID, avatarURL, nickname, createdAt string
		if u != nil {
			steamID = u.SteamID
			avatarURL = u.AvatarURL
			nickname = u.Nickname
			createdAt = u.CreatedAt.Format("2006-01-02T15:04:05Z")
		}
		resp.OK(c, gin.H{
			"openid":     oid,
			"steam_id":   steamID,
			"avatar_url": avatarURL,
			"nickname":   nickname,
			"created_at": createdAt,
		})
	}
}

func UpdateSteamID(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			SteamID string `json:"steam_id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			resp.Fail(c, 400, "steam_id required")
			return
		}
		if err := s.UpdateSteamID(c.Request.Context(), openid(c), req.SteamID); err != nil {
			resp.Fail(c, 500, "db error")
			return
		}
		resp.OK(c, gin.H{"steam_id": req.SteamID})
	}
}

func UpdateProfile(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			AvatarURL string `json:"avatar_url"`
			Nickname  string `json:"nickname"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			resp.Fail(c, 400, "bad request")
			return
		}
		if err := s.UpdateProfile(c.Request.Context(), openid(c), req.AvatarURL, req.Nickname); err != nil {
			resp.Fail(c, 500, "db error")
			return
		}
		resp.OK(c, gin.H{"ok": "1"})
	}
}
