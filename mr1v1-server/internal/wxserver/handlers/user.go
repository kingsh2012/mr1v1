package handlers

import (
	"encoding/json"
	"net/http"

	"mr1v1-server/internal/wxserver/store"
)

func UserHandler(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		token := r.Header.Get("Authorization")
		openid, ok := s.GetOpenIDByToken(r.Context(), token)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		switch r.Method {
		case http.MethodGet:
			u, err := s.GetUser(r.Context(), openid)
			if err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}
			var steamID, avatarURL, nickname, createdAt string
			if u != nil {
				steamID = u.SteamID
				avatarURL = u.AvatarURL
				nickname = u.Nickname
				createdAt = u.CreatedAt.Format("2006-01-02T15:04:05Z")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"openid":     openid,
				"steam_id":   steamID,
				"avatar_url": avatarURL,
				"nickname":   nickname,
				"created_at": createdAt,
			})

		case http.MethodPost:
			var req struct {
				SteamID string `json:"steam_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SteamID == "" {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if err := s.UpdateSteamID(r.Context(), openid, req.SteamID); err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"steam_id": req.SteamID})

		case http.MethodPatch:
			var req struct {
				AvatarURL string `json:"avatar_url"`
				Nickname  string `json:"nickname"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if err := s.UpdateProfile(r.Context(), openid, req.AvatarURL, req.Nickname); err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"ok": "1"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
