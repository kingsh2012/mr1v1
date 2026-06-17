package main

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	wxconfig "mr1v1-server/internal/wxserver/config"
	"mr1v1-server/internal/wxserver/handlers"
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

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(handlers.CORS())

	wx := r.Group("/api/wx")
	wx.POST("/login", handlers.Login(cfg, s))
	wx.GET("/legacy-players/search", handlers.SearchLegacyPlayers(s))

	auth := wx.Group("", handlers.Auth(s))
	auth.GET("/user", handlers.GetUser(s))
	auth.POST("/user", handlers.UpdateSteamID(s))
	auth.PATCH("/user", handlers.UpdateProfile(s))
	auth.GET("/rooms", handlers.ListRooms(s))
	auth.POST("/rooms", handlers.CreateRoom(s))
	auth.POST("/rooms/:id/join", handlers.JoinRoom(s))
	auth.DELETE("/rooms/:id", handlers.LeaveRoom(s))
	auth.POST("/legacy-players/bind", handlers.BindLegacyPlayer(s))

	ws := r.Group("/ws/wx")
	ws.GET("/room/:id", handlers.RoomWS(s, mgr))
	ws.GET("/matchmaking", handlers.Matchmaking(mm, s))

	slog.Info("mr1v1-wx listening", "addr", ":"+cfg.Port, "backend", cfg.BackendURL)
	if err := r.Run(":" + cfg.Port); err != nil {
		slog.Error("server stopped", "error", err)
	}
}
