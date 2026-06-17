package handlers

import (
	"github.com/gin-gonic/gin"
	"mr1v1-server/internal/resp"
	"mr1v1-server/internal/wxserver/room"
	"mr1v1-server/internal/wxserver/store"
)

// MatchEnded 接收 manager-backend/consumer 在比赛进入终态（terminated/timeout/
// error/finished）时的同步通知，关闭对应房间并提醒仍在房间页的玩家。
func MatchEnded(s *store.Store, mgr *room.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			MatchID string `json:"match_id" binding:"required"`
			State   string `json:"state"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			resp.Fail(c, 400, "match_id required")
			return
		}

		roomID, err := s.GetRoomIDByMatchID(c.Request.Context(), req.MatchID)
		if err != nil {
			resp.Fail(c, 500, "db error")
			return
		}
		if roomID == "" {
			resp.OK(c, gin.H{"ok": "1"})
			return
		}

		if err := s.DeleteRoom(c.Request.Context(), roomID); err != nil {
			resp.Fail(c, 500, "db error")
			return
		}
		if hub, ok := mgr.GetIfExists(roomID); ok {
			hub.NotifyMatchEnded("比赛已结束，服务器已销毁")
		}
		resp.OK(c, gin.H{"ok": "1"})
	}
}
