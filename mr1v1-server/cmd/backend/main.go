package main

import (
	"log/slog"
	"net/http"
	"os"

	"mr1v1-server/internal/backend"
	"mr1v1-server/internal/config"
)

func main() {
	cfg := config.LoadBackendFromEnv()

	b, err := backend.New(cfg)
	if err != nil {
		slog.Error("init backend failed", "error", err)
		os.Exit(1)
	}
	defer b.Close()

	slog.Info("backend listening", "addr", cfg.HTTP.Listen)
	if err := http.ListenAndServe(cfg.HTTP.Listen, b.Handler()); err != nil {
		slog.Error("http server stopped", "error", err)
		os.Exit(1)
	}
}
