package main

import (
	"log/slog"
	"net/http"
	"os"

	"mr1v1-collector/internal/agent"
	"mr1v1-collector/internal/config"
)

func main() {
	cfg := config.LoadAgentFromEnv()

	a, err := agent.New(cfg)
	if err != nil {
		slog.Error("init agent failed", "error", err)
		os.Exit(1)
	}
	defer a.Close()

	slog.Info("agent listening", "addr", cfg.HTTP.Listen)
	if err := http.ListenAndServe(cfg.HTTP.Listen, a.Handler()); err != nil {
		slog.Error("http server stopped", "error", err)
		os.Exit(1)
	}
}
