package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	wxconfig "mr1v1-server/internal/wxserver/config"
	"mr1v1-server/internal/wxserver/handlers"
	"mr1v1-server/internal/wxserver/legacy"
	"mr1v1-server/internal/wxserver/matchmaker"
	"mr1v1-server/internal/wxserver/room"
	"mr1v1-server/internal/wxserver/store"
)

func main() {
	cfg := wxconfig.LoadWx()
	ctx := context.Background()

	if cfg.DatabaseURL == "" {
		slog.Error("DATABASE_URL is required")
		return
	}
	s, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("store open failed", "err", err)
		return
	}
	if err := s.Migrate(ctx); err != nil {
		slog.Error("store migrate failed", "err", err)
		return
	}
	slog.Info("store connected and migrated")

	mm := matchmaker.New(cfg.BackendURL, cfg.InternalAPIKey)
	mgr := room.NewManager(cfg.BackendURL, cfg.InternalAPIKey, s)

	const (
		apiPrefix = "/api/wx"
		wsPrefix  = "/ws/wx"
	)

	mux := http.NewServeMux()
	mux.HandleFunc(apiPrefix+"/login", handlers.LoginHandler(cfg, s))
	mux.HandleFunc(apiPrefix+"/user", handlers.UserHandler(s))
	mux.HandleFunc(apiPrefix+"/rooms", handlers.RoomsHandler(s, mgr))
	mux.HandleFunc(apiPrefix+"/rooms/", handlers.RoomActionHandler(s, apiPrefix))
	mux.HandleFunc(wsPrefix+"/room/", handlers.RoomWSHandler(s, mgr, wsPrefix))
	mux.HandleFunc(wsPrefix+"/matchmaking", handlers.MatchmakingHandler(mm, s))

	if cfg.LegacyAPIURL != "" {
		syncer := legacy.NewSyncer(cfg.LegacyAPIURL, s)
		go syncer.Start(ctx, 10*time.Minute)
		slog.Info("legacy player sync enabled")
	} else {
		slog.Warn("LEGACY_API_URL not set, legacy player sync disabled")
	}
	mux.HandleFunc(apiPrefix+"/legacy-players/search", handlers.SearchLegacyPlayersHandler(s))
	mux.HandleFunc(apiPrefix+"/legacy-players/bind", handlers.BindLegacyPlayerHandler(s))

	slog.Info("mr1v1-wx listening", "addr", ":"+cfg.Port, "backend", cfg.BackendURL)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		slog.Error("server stopped", "error", err)
	}
}
