package handlers

import (
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
		resp.OK(c, rooms)
	}
}

func CreateRoom(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Title    string `json:"title"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Title) == "" {
			resp.Fail(c, 400, "title required")
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
		if err := s.CreateRoom(c.Request.Context(), id, req.Title, oid, req.Password); err != nil {
			resp.Fail(c, 500, "db error")
			return
		}

		rm, _ := s.GetRoom(c.Request.Context(), id)
		resp.OK(c, rm)
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
