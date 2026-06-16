package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"mr1v1-server/internal/config"
	"mr1v1-server/internal/consumer"
)

func main() {
	cfg := config.LoadConsumerFromEnv()

	c, err := consumer.New(cfg)
	if err != nil {
		slog.Error("init consumer failed", "error", err)
		os.Exit(1)
	}
	defer c.Close()

	slog.Info("consumer started", "broker", cfg.MQTT.Broker, "topic", cfg.MQTT.Topic)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	slog.Info("consumer shutting down")
}
