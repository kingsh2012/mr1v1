package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"mr1v1-server/internal/wxserver/room"
	"mr1v1-server/internal/wxserver/store"
)

var roomUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func RoomsHandler(s *store.Store, mgr *room.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		switch r.Method {
		case http.MethodGet:
			listRooms(w, r, s)
		case http.MethodPost:
			createRoom(w, r, s)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func listRooms(w http.ResponseWriter, r *http.Request, s *store.Store) {
	rooms, err := s.ListRooms(r.Context())
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if rooms == nil {
		rooms = []store.Room{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rooms)
}

func createRoom(w http.ResponseWriter, r *http.Request, s *store.Store) {
	token := r.Header.Get("Authorization")
	openid, ok := s.GetOpenIDByToken(r.Context(), token)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Title    string `json:"title"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		http.Error(w, "title required", http.StatusBadRequest)
		return
	}

	hasRoom, err := s.HasActiveRoom(r.Context(), openid)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if hasRoom {
		http.Error(w, "already have an active room", http.StatusConflict)
		return
	}

	id := uuid.New().String()
	if err := s.CreateRoom(r.Context(), id, req.Title, openid, req.Password); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	rm, _ := s.GetRoom(r.Context(), id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rm)
}

// RoomActionHandler handles POST {apiPrefix}/rooms/{id}/join and DELETE {apiPrefix}/rooms/{id}
func RoomActionHandler(s *store.Store, apiPrefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		token := r.Header.Get("Authorization")
		openid, ok := s.GetOpenIDByToken(r.Context(), token)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// extract room id and action from path: {apiPrefix}/rooms/{id}/join or {apiPrefix}/rooms/{id}
		path := strings.TrimPrefix(r.URL.Path, apiPrefix+"/rooms/")
		parts := strings.SplitN(path, "/", 2)
		roomID := parts[0]
		action := ""
		if len(parts) == 2 {
			action = parts[1]
		}

		switch {
		case r.Method == http.MethodPost && action == "join":
			joinRoom(w, r, s, roomID, openid)
		case r.Method == http.MethodDelete && action == "":
			leaveRoom(w, r, s, roomID, openid)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}
}

func joinRoom(w http.ResponseWriter, r *http.Request, s *store.Store, roomID, openid string) {
	rm, err := s.GetRoom(r.Context(), roomID)
	if err != nil || rm == nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	if rm.CreatorOpenID == openid {
		http.Error(w, "cannot join your own room", http.StatusForbidden)
		return
	}
	pw, err2 := s.GetRoomPassword(r.Context(), roomID)
	if err2 != nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	if pw != "" {
		var req struct {
			Password string `json:"password"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Password != pw {
			http.Error(w, "wrong password", http.StatusForbidden)
			return
		}
	}

	if err := s.JoinRoom(r.Context(), roomID, openid); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	rm, _ = s.GetRoom(r.Context(), roomID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rm)
}

func leaveRoom(w http.ResponseWriter, r *http.Request, s *store.Store, roomID, openid string) {
	if err := s.LeaveRoom(r.Context(), roomID, openid); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RoomWSHandler handles WS {wsPrefix}/room/{id}?token=xxx
func RoomWSHandler(s *store.Store, mgr *room.Manager, wsPrefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		openid, ok := s.GetOpenIDByToken(r.Context(), token)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		roomID := strings.TrimPrefix(r.URL.Path, wsPrefix+"/room/")

		rm, err := s.GetRoom(r.Context(), roomID)
		if err != nil || rm == nil {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}

		role := ""
		switch openid {
		case rm.CreatorOpenID:
			role = "creator"
		case rm.JoinerOpenID:
			role = "joiner"
		default:
			http.Error(w, "not a member of this room", http.StatusForbidden)
			return
		}

		u, _ := s.GetUser(r.Context(), openid)
		name, avatar, steamID := "", "", ""
		if u != nil {
			name = u.Nickname
			avatar = u.AvatarURL
			steamID = u.SteamID
		}

		conn, err := roomUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		hub := mgr.GetOrCreate(roomID)
		hub.Connect(conn, openid, name, avatar, steamID, role)
	}
}
