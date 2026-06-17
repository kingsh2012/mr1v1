package handlers

import (
	"encoding/json"
	"net/http"

	"mr1v1-server/internal/wxserver/store"
)

func SearchLegacyPlayersHandler(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		keyword := r.URL.Query().Get("keyword")
		if keyword == "" {
			http.Error(w, "keyword required", http.StatusBadRequest)
			return
		}

		players, err := s.SearchLegacyPlayers(r.Context(), keyword)
		if err != nil {
			http.Error(w, "search failed", http.StatusInternalServerError)
			return
		}
		if players == nil {
			players = []store.LegacyPlayer{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(players)
	}
}

func BindLegacyPlayerHandler(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		token := r.Header.Get("Authorization")
		openid, ok := s.GetOpenIDByToken(r.Context(), token)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

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
	}
}
