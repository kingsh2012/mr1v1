package handlers

import (
	"github.com/gin-gonic/gin"
	"mr1v1-server/internal/resp"
	"mr1v1-server/internal/wxserver/room"
	"mr1v1-server/internal/wxserver/store"
)

// MatchEnded 接收 manager-backend/consumer 在比赛进入终态（terminated/timeout/
// error/finished）时的同步通知。只有finished(正常打完)才标记completed留在公开
// 列表里给大家看最终比分，其余异常终态直接软删除(不再出现在公开列表，但本人
// 历史记录还看得到)，并提醒仍在房间页的玩家。
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

		// 只有"finished"(正常打完)才保留在公开房间列表里给大家看最终比分；
		// timeout/terminated/error这几种异常终态，房间从来没有真正打完，不该
		// 跟正常结束的比赛混在一起展示——软删除掉，但"我的比赛记录"不过滤
		// deleted_at，本人历史记录里还是能看到准确的终态。
		if req.State == "finished" {
			err = s.CompleteRoom(c.Request.Context(), roomID)
		} else {
			status := req.State
			if status == "" {
				status = "terminated"
			}
			err = s.DeleteRoom(c.Request.Context(), roomID, status)
		}
		if err != nil {
			resp.Fail(c, 500, "db error")
			return
		}
		if hub, ok := mgr.GetIfExists(roomID); ok {
			hub.NotifyMatchEnded("比赛已结束，服务器已销毁")
		}
		resp.OK(c, gin.H{"ok": "1"})
	}
}

// RoundUpdate 接收consumer在每个回合结束(round_end)时的同步通知，更新对应房间
// 的实时比分，让房间列表里matched状态的房间能展示当前比分。异步、不阻塞游戏流程，
// 房间不存在(比赛不是通过小程序房间发起的)时UPDATE影响0行，静默忽略即可。
func RoundUpdate(s *store.Store, mgr *room.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			MatchID      string `json:"match_id" binding:"required"`
			ScoreCreator int    `json:"score_creator"`
			ScoreJoiner  int    `json:"score_joiner"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			resp.Fail(c, 400, "match_id required")
			return
		}
		if err := s.UpdateRoomScoreByMatchID(c.Request.Context(), req.MatchID, req.ScoreCreator, req.ScoreJoiner); err != nil {
			resp.Fail(c, 500, "db error")
			return
		}
		if roomID, err := s.GetRoomIDByMatchID(c.Request.Context(), req.MatchID); err == nil && roomID != "" {
			if hub, ok := mgr.GetIfExists(roomID); ok {
				hub.NotifyScoreUpdate(req.ScoreCreator, req.ScoreJoiner)
			}
		}
		resp.OK(c, gin.H{"ok": "1"})
	}
}
