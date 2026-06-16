package main

import (
	"log/slog"
	"net/http"
	"os"

	"mr1v1-server/internal/config"
	"mr1v1-server/internal/gateway"
)

func main() {
	cfg := &config.GatewayConfig{}
	cfg.HTTP.Listen = getEnv("HTTP_LISTEN", "0.0.0.0:7778")
	cfg.MQTT.Broker = getEnv("MQTT_BROKER", "tcp://localhost:1883")
	cfg.MQTT.TopicPrefix = getEnv("MQTT_TOPIC_PREFIX", "mr1v1")
	cfg.MQTT.ClientID = getEnv("MQTT_CLIENT_ID", "mr1v1-gateway")
	cfg.MQTT.User = getEnv("MQTT_USER", "")
	cfg.MQTT.Pass = getEnv("MQTT_PASS", "")
	cfg.Queue.Capacity = 10000

	srv, err := gateway.New(cfg)
	if err != nil {
		slog.Error("init gateway failed", "error", err)
		os.Exit(1)
	}
	defer srv.Close()

	slog.Info("gateway listening", "addr", cfg.HTTP.Listen)
	if err := http.ListenAndServe(cfg.HTTP.Listen, srv.Handler()); err != nil {
		slog.Error("http server stopped", "error", err)
		os.Exit(1)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
