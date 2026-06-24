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
		// 老玩家身份本来就是按SteamID找回来的，连带把legacy_players里的name/weixin_photo
		// 同步成微信这边的昵称/头像——weixin_photo是老5v5平台同步过来的微信头像地址，
		// 个别老记录可能没采集到，没有的话只同步昵称，前端按新昵称兜底生成identicon
		nickname, avatarURL := "", ""
		if lp, err := s.GetLegacyPlayerByID(c.Request.Context(), req.SteamID); err == nil && lp != nil {
			nickname = lp.Name
			avatarURL = lp.WeixinPhoto
			if avatarURL != "" {
				_ = s.UpdateProfile(c.Request.Context(), openid(c), avatarURL, nickname)
			} else {
				_ = s.UpdateNickname(c.Request.Context(), openid(c), nickname)
			}
		}
		resp.OK(c, gin.H{"steam_id": req.SteamID, "nickname": nickname, "avatar_url": avatarURL})
	}
}
