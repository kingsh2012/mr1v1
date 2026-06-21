package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	wxconfig "mr1v1-server/internal/wxserver/config"
	"mr1v1-server/internal/wxserver/handlers"
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

	mgr := room.NewManager(cfg.BackendURL, cfg.InternalAPIKey, s)

	go sweepStaleRooms(ctx, s, mgr, time.Duration(cfg.RoomStaleMinutes)*time.Minute)

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(handlers.CORS())
	// nginx只转发了/api/wx/和/ws/wx/这两个前缀到本服务，静态文件也挂在/api/wx/下面，
	// 不用额外改生产环境的反代配置
	r.Static("/api/wx/static/avatars", cfg.AvatarsDir)

	wx := r.Group("/api/wx")
	wx.POST("/login", handlers.Login(cfg, s))
	wx.GET("/legacy-players/search", handlers.SearchLegacyPlayers(s))
	// 房间列表不强制登录：游客也能看到有哪些房间可以玩，点进去加入/创建时才要求登录
	wx.GET("/rooms", handlers.OptionalAuth(s), handlers.ListRooms(s))
	// 随机昵称+头像预览("骰子"按钮用)和头像图片本身都不需要登录，纯生成/渲染，不读用户数据
	wx.GET("/random-profile", handlers.RandomProfile(cfg))
	wx.GET("/identicon/:seed", handlers.Identicon)

	auth := wx.Group("", handlers.Auth(s))
	auth.GET("/user", handlers.GetUser(s))
	auth.POST("/user", handlers.UpdateSteamID(s))
	auth.PATCH("/user", handlers.UpdateProfile(s))
	auth.POST("/avatar", handlers.UploadAvatar(cfg.AvatarsDir, cfg.PublicURL))
	auth.POST("/rooms", handlers.CreateRoom(s))
	auth.POST("/rooms/:id/join", handlers.JoinRoom(s))
	auth.DELETE("/rooms/:id", handlers.LeaveRoom(s, mgr))
	auth.POST("/legacy-players/bind", handlers.BindLegacyPlayer(s))

	ws := r.Group("/ws/wx")
	ws.GET("/room/:id", handlers.RoomWS(s, mgr))

	internal := r.Group("/api/wx/internal", handlers.InternalAuth(cfg.InternalAPIKey))
	internal.POST("/match-ended", handlers.MatchEnded(s, mgr))

	slog.Info("mr1v1-wx listening", "addr", ":"+cfg.Port, "backend", cfg.BackendURL)
	if err := r.Run(":" + cfg.Port); err != nil {
		slog.Error("server stopped", "error", err)
	}
}

// sweepStaleRooms 定时清理长时间无状态变化且当前无人在线的房间（孤儿房间、
// 双方早已断线但房间一直停留在 waiting/ready 的情况），软删除。
// 仍有人在线（room.Manager 里存在对应 Hub）的房间不会被清理，即使等待对手
// 的时间很长——只清理"确实没人了"的房间。
func sweepStaleRooms(ctx context.Context, s *store.Store, mgr *room.Manager, idleFor time.Duration) {
	if idleFor <= 0 {
		return
	}
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ids, err := s.StaleRoomIDs(ctx, idleFor)
			if err != nil {
				slog.Error("sweep stale rooms query failed", "error", err)
				continue
			}
			for _, id := range ids {
				if mgr.IsActive(id) {
					continue
				}
				if err := s.DeleteRoom(ctx, id); err != nil {
					slog.Error("sweep stale room failed", "room", id, "error", err)
					continue
				}
				slog.Info("auto-closed stale room", "room", id, "idle_for", idleFor)
			}
		}
	}
}
