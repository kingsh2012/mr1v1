package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"
	"mr1v1-server/internal/wxserver/matchmaker"
	"mr1v1-server/internal/wxserver/models"
	"mr1v1-server/internal/wxserver/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type joinMsg struct {
	Type    string `json:"type"`
	Token   string `json:"token"`
	SteamID string `json:"steamid"`
}

func MatchmakingHandler(mm *matchmaker.Matchmaker, s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
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

		openid, ok := s.GetOpenIDByToken(r.Context(), join.Token)
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
