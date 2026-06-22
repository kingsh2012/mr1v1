package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"mr1v1-server/internal/resp"
	"mr1v1-server/internal/wxserver/room"
	"mr1v1-server/internal/wxserver/store"
)

var roomUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func ListRooms(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		rooms, err := s.ListRooms(c.Request.Context())
		if err != nil {
			resp.Fail(c, 500, "db error")
			return
		}
		if rooms == nil {
			rooms = []store.Room{}
		}
		// 这个接口游客也能访问（不强制登录），openid不对外暴露；
		// 只有带了有效token的请求才会算出is_mine，告诉前端哪间是自己创建的房
		oid := openid(c)
		if oid != "" {
			for i := range rooms {
				rooms[i].IsMine = rooms[i].CreatorOpenID == oid
			}
		}
		resp.OK(c, rooms)
	}
}

var validRoomCategories = map[string]bool{"pistol": true, "rifle": true, "sniper": true}

func CreateRoom(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Title    string `json:"title"`
			Password string `json:"password"`
			Category string `json:"category"`
			MapName  string `json:"map_name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Title) == "" {
			resp.Fail(c, 400, "title required")
			return
		}
		if !validRoomCategories[req.Category] {
			resp.Fail(c, 400, "category must be one of pistol/rifle/sniper")
			return
		}

		oid := openid(c)
		hasRoom, err := s.HasActiveRoom(c.Request.Context(), oid)
		if err != nil {
			resp.Fail(c, 500, "db error")
			return
		}
		if hasRoom {
			resp.Fail(c, 409, "already have an active room")
			return
		}

		id := uuid.New().String()
		if err := s.CreateRoom(c.Request.Context(), id, req.Title, oid, req.Password, req.Category, strings.TrimSpace(req.MapName)); err != nil {
			resp.Fail(c, 500, "db error")
			return
		}

		rm, _ := s.GetRoom(c.Request.Context(), id)
		resp.OK(c, rm)
	}
}

// MyMatches 返回当前登录用户参与过的所有比赛记录（任何状态都展示，不止已完成的）。
func MyMatches(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		records, err := s.ListMyMatches(c.Request.Context(), openid(c))
		if err != nil {
			resp.Fail(c, 500, "db error")
			return
		}
		if records == nil {
			records = []store.MatchRecord{}
		}
		resp.OK(c, records)
	}
}

func JoinRoom(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		roomID := c.Param("id")
		oid := openid(c)

		rm, err := s.GetRoom(c.Request.Context(), roomID)
		if err != nil || rm == nil {
			resp.Fail(c, 404, "room not found")
			return
		}
		if rm.CreatorOpenID == oid {
			resp.Fail(c, 403, "cannot join your own room")
			return
		}

		pw, err := s.GetRoomPassword(c.Request.Context(), roomID)
		if err != nil {
			resp.Fail(c, 404, "room not found")
			return
		}
		if pw != "" {
			var req struct {
				Password string `json:"password"`
			}
			c.ShouldBindJSON(&req)
			if req.Password != pw {
				resp.Fail(c, 403, "wrong password")
				return
			}
		}

		if err := s.JoinRoom(c.Request.Context(), roomID, oid); err != nil {
			if errors.Is(err, store.ErrRoomNotJoinable) {
				// 并发竞态：在密码校验和这次UPDATE之间，房间已经被别人抢先加入/状态变了，
				// 不能当成功处理——之前这里没检查RowsAffected，会把"没抢到"误报成"加入成功"
				resp.Fail(c, 409, "room already taken")
				return
			}
			resp.Fail(c, 500, "db error")
			return
		}

		rm, _ = s.GetRoom(c.Request.Context(), roomID)
		resp.OK(c, rm)
	}
}

// LeaveRoom 处理 DELETE /rooms/:id：creator 调用 = 销毁房间（软删除+踢出对手），
// joiner 调用 = 仅退出该房间（房间回到 waiting，等待新对手）。
func LeaveRoom(s *store.Store, mgr *room.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		roomID := c.Param("id")
		oid := openid(c)

		rm, err := s.GetRoom(c.Request.Context(), roomID)
		if err != nil {
			resp.Fail(c, 500, "db error")
			return
		}
		if rm == nil {
			resp.OK(c, gin.H{"ok": "1"})
			return
		}

		if rm.CreatorOpenID == oid {
			if err := s.DeleteRoom(c.Request.Context(), roomID); err != nil {
				resp.Fail(c, 500, "db error")
				return
			}
			if hub, ok := mgr.GetIfExists(roomID); ok {
				hub.CloseByCreator()
			}
		} else {
			if err := s.LeaveRoom(c.Request.Context(), roomID, oid); err != nil {
				resp.Fail(c, 500, "db error")
				return
			}
		}
		resp.OK(c, gin.H{"ok": "1"})
	}
}

func RoomWS(s *store.Store, mgr *room.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Query("token")
		oid, ok := s.GetOpenIDByToken(c.Request.Context(), token)
		if !ok {
			resp.Fail(c, 401, "unauthorized")
			return
		}

		roomID := c.Param("id")
		rm, err := s.GetRoom(c.Request.Context(), roomID)
		if err != nil || rm == nil {
			resp.Fail(c, 404, "room not found")
			return
		}

		role := ""
		switch oid {
		case rm.CreatorOpenID:
			role = "creator"
		case rm.JoinerOpenID:
			role = "joiner"
		default:
			resp.Fail(c, 403, "not a member of this room")
			return
		}

		u, _ := s.GetUser(c.Request.Context(), oid)
		name, avatar, steamID := "", "", ""
		if u != nil {
			name = u.Nickname
			avatar = u.AvatarURL
			steamID = u.SteamID
		}

		conn, err := roomUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}

		hub := mgr.GetOrCreate(roomID)
		hub.Connect(conn, oid, name, avatar, steamID, role)
	}
}
