package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"mr1v1-server/internal/wxserver/matchmaker"
	"mr1v1-server/internal/wxserver/models"
	"mr1v1-server/internal/wxserver/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// unmarshalJSON is a thin wrapper used by auth.go too.
func unmarshalJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

type joinMsg struct {
	Type    string `json:"type"`
	Token   string `json:"token"`
	SteamID string `json:"steamid"`
}

func Matchmaking(mm *matchmaker.Matchmaker, s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var join joinMsg
		if err := json.Unmarshal(raw, &join); err != nil || join.Type != "join_queue" {
			conn.WriteJSON(models.MatchMessage{Type: "error", Message: "invalid message"})
			return
		}
		if join.SteamID == "" {
			conn.WriteJSON(models.MatchMessage{Type: "error", Message: "steamid required"})
			return
		}

		openid, ok := s.GetOpenIDByToken(c.Request.Context(), join.Token)
		if !ok {
			conn.WriteJSON(models.MatchMessage{Type: "error", Message: "invalid token"})
			return
		}

		player := &models.Player{OpenID: openid, SteamID: join.SteamID, Conn: conn}
		conn.WriteJSON(models.MatchMessage{Type: "waiting", Message: "等待匹配中..."})
		mm.Join(player)

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				mm.Leave(player)
				return
			}
		}
	}
}
