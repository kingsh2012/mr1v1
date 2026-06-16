package main

import (
	"log/slog"
	"net/http"

	wxconfig "mr1v1-collector/internal/wxserver/config"
	"mr1v1-collector/internal/wxserver/handlers"
	"mr1v1-collector/internal/wxserver/matchmaker"
)

func main() {
	cfg := wxconfig.LoadWx()

	mm := matchmaker.New(cfg.GameServerAddr)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", handlers.LoginHandler(cfg))
	mux.HandleFunc("/ws/matchmaking", handlers.MatchmakingHandler(mm))

	slog.Info("mr1v1-wx listening", "addr", ":"+cfg.Port, "game_server", cfg.GameServerAddr)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		slog.Error("server stopped", "error", err)
	}
}
