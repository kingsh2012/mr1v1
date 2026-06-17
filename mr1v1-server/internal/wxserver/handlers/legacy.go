package handlers

import (
	"github.com/gin-gonic/gin"
	"mr1v1-server/internal/resp"
	"mr1v1-server/internal/wxserver/store"
)

func SearchLegacyPlayers(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		keyword := c.Query("keyword")
		if keyword == "" {
			resp.Fail(c, 400, "keyword required")
			return
		}
		players, err := s.SearchLegacyPlayers(c.Request.Context(), keyword)
		if err != nil {
			resp.Fail(c, 500, "search failed")
			return
		}
		if players == nil {
			players = []store.LegacyPlayer{}
		}
		resp.OK(c, players)
	}
}

func BindLegacyPlayer(s *store.Store) gin.HandlerFunc {
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
