// Package config 提供各服务的配置结构体及从环境变量加载的函数。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
)

// GatewayConfig 对应 gateway 服务（仅本地开发用，生产由 agent 内嵌）。
type GatewayConfig struct {
	HTTP struct {
		Listen string
	}
	MQTT struct {
		Broker      string
		TopicPrefix string
		ClientID    string
		User        string
		Pass        string
	}
	Queue struct {
		Capacity int
	}
}

// ConsumerConfig 对应 cmd/consumer。
type ConsumerConfig struct {
	MQTT struct {
		Broker   string
		Topic    string
		ClientID string
		User     string
		Pass     string
	}
	DB struct {
		Host     string
		Port     int
		User     string
		Pass     string
		DBName   string
		SSLMode  string
		Timezone string
	}
}

// AgentConfig 对应 cmd/agent。
type AgentConfig struct {
	HTTP struct {
		Listen string
	}
	MQTT struct {
		Broker      string
		TopicPrefix string
		ClientID    string
		User        string
		Pass        string
	}
	Queue struct {
		Capacity int
	}
	Heartbeat struct {
		IntervalSeconds int
		PublicIP        string
	}
	Docker struct {
		DefaultImage            string
		StopTimeoutSeconds      int
		DestroyCommand          string
		DestroyCountdownSeconds int
	}
}

// BackendConfig 对应 cmd/backend。
type BackendConfig struct {
	HTTP struct {
		Listen string
	}
	MQTT struct {
		Broker   string
		ClientID string
		User     string
		Pass     string
	}
	DB struct {
		Host     string
		Port     int
		User     string
		Pass     string
		DBName   string
		SSLMode  string
		Timezone string
	}
	AgentStaleSeconds          int
	MatchWaitingTimeoutSeconds int
	MatchPlayingTimeoutSeconds int
	AdminUser                  string
	AdminPass                  string
}

// autoClientID 在 MQTT_CLIENT_ID 未设置时自动生成唯一 client_id。
// 格式：{prefix}-{hostname}-{4字节随机hex}，多实例/重启后不会冲突。
func autoClientID(prefix string) string {
	hostname, _ := os.Hostname()
	b := make([]byte, 4)
	rand.Read(b) //nolint:errcheck
	return fmt.Sprintf("%s-%s-%s", prefix, hostname, hex.EncodeToString(b))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// LoadConsumerFromEnv 从环境变量加载 consumer 配置。
//
// 环境变量：
//
//	MQTT_BROKER      (default: tcp://localhost:1883)
//	MQTT_TOPIC       (default: mr1v1/#)
//	MQTT_CLIENT_ID   (default: mr1v1-consumer)
//	MQTT_USER
//	MQTT_PASS
//	DB_HOST          (default: localhost)
//	DB_PORT          (default: 5432)
//	DB_USER          (default: mr1v1)
//	DB_PASS
//	DB_NAME          (default: mr1v1)
//	DB_SSL_MODE      (default: disable)
//	DB_TIMEZONE      (default: Asia/Shanghai)
func LoadConsumerFromEnv() *ConsumerConfig {
	cfg := &ConsumerConfig{}
	cfg.MQTT.Broker = envOr("MQTT_BROKER", "tcp://localhost:1883")
	cfg.MQTT.Topic = envOr("MQTT_TOPIC", "mr1v1/#")
	cfg.MQTT.ClientID = envOr("MQTT_CLIENT_ID", autoClientID("mr1v1-consumer"))
	cfg.MQTT.User = envOr("MQTT_USER", "")
	cfg.MQTT.Pass = envOr("MQTT_PASS", "")
	cfg.DB.Host = envOr("DB_HOST", "localhost")
	cfg.DB.Port = envIntOr("DB_PORT", 5432)
	cfg.DB.User = envOr("DB_USER", "mr1v1")
	cfg.DB.Pass = envOr("DB_PASS", "")
	cfg.DB.DBName = envOr("DB_NAME", "mr1v1")
	cfg.DB.SSLMode = envOr("DB_SSL_MODE", "disable")
	cfg.DB.Timezone = envOr("DB_TIMEZONE", "Asia/Shanghai")
	return cfg
}

// LoadBackendFromEnv 从环境变量加载 backend 配置。
//
// 环境变量：
//
//	HTTP_LISTEN          (default: 0.0.0.0:8181)
//	MQTT_BROKER          (default: tcp://localhost:1883)
//	MQTT_CLIENT_ID       (default: 自动生成)
//	MQTT_USER
//	MQTT_PASS
//	DB_HOST              (default: localhost)
//	DB_PORT              (default: 5432)
//	DB_USER              (default: mr1v1)
//	DB_PASS
//	DB_NAME              (default: mr1v1)
//	DB_SSL_MODE          (default: disable)
//	DB_TIMEZONE          (default: Asia/Shanghai)
//	AGENT_STALE_SECONDS  (default: 30)
func LoadBackendFromEnv() *BackendConfig {
	cfg := &BackendConfig{}
	cfg.HTTP.Listen = envOr("HTTP_LISTEN", "0.0.0.0:8181")
	cfg.MQTT.Broker = envOr("MQTT_BROKER", "tcp://localhost:1883")
	cfg.MQTT.ClientID = envOr("MQTT_CLIENT_ID", autoClientID("mr1v1-backend"))
	cfg.MQTT.User = envOr("MQTT_USER", "")
	cfg.MQTT.Pass = envOr("MQTT_PASS", "")
	cfg.DB.Host = envOr("DB_HOST", "localhost")
	cfg.DB.Port = envIntOr("DB_PORT", 5432)
	cfg.DB.User = envOr("DB_USER", "mr1v1")
	cfg.DB.Pass = envOr("DB_PASS", "")
	cfg.DB.DBName = envOr("DB_NAME", "mr1v1")
	cfg.DB.SSLMode = envOr("DB_SSL_MODE", "disable")
	cfg.DB.Timezone = envOr("DB_TIMEZONE", "Asia/Shanghai")
	cfg.AgentStaleSeconds = envIntOr("AGENT_STALE_SECONDS", 30)
	cfg.MatchWaitingTimeoutSeconds = envIntOr("MATCH_WAITING_TIMEOUT_SECONDS", 300)
	cfg.MatchPlayingTimeoutSeconds = envIntOr("MATCH_PLAYING_TIMEOUT_SECONDS", 900)
	cfg.AdminUser = envOr("ADMIN_USER", "admin")
	cfg.AdminPass = envOr("ADMIN_PASS", "")
	return cfg
}

// LoadAgentFromEnv 从环境变量加载 agent 配置。
//
// 环境变量：
//
//	HTTP_LISTEN          (default: 0.0.0.0:7778)
//	MQTT_BROKER          (default: tcp://localhost:1883)
//	MQTT_USER
//	MQTT_PASS
//	QUEUE_CAPACITY       (default: 10000)
//	HEARTBEAT_INTERVAL   (default: 10)
//	PUBLIC_IP            (可选，不填则使用本机检测到的内网IP)
//	DOCKER_IMAGE
//	DOCKER_STOP_TIMEOUT  (default: 15)
//	DESTROY_COMMAND      (default: mr1v1_match_destroy)
//	DESTROY_COUNTDOWN    (default: 5)
func LoadAgentFromEnv() *AgentConfig {
	cfg := &AgentConfig{}
	cfg.HTTP.Listen = envOr("HTTP_LISTEN", "0.0.0.0:7778")
	cfg.MQTT.Broker = envOr("MQTT_BROKER", "tcp://localhost:1883")
	cfg.MQTT.TopicPrefix = envOr("MQTT_TOPIC_PREFIX", "mr1v1")
	cfg.MQTT.User = envOr("MQTT_USER", "")
	cfg.MQTT.Pass = envOr("MQTT_PASS", "")
	cfg.Queue.Capacity = envIntOr("QUEUE_CAPACITY", 10000)
	cfg.Heartbeat.IntervalSeconds = envIntOr("HEARTBEAT_INTERVAL", 10)
	cfg.Heartbeat.PublicIP = envOr("PUBLIC_IP", "")
	cfg.Docker.DefaultImage = envOr("DOCKER_IMAGE", "")
	cfg.Docker.StopTimeoutSeconds = envIntOr("DOCKER_STOP_TIMEOUT", 15)
	cfg.Docker.DestroyCommand = envOr("DESTROY_COMMAND", "mr1v1_match_destroy")
	cfg.Docker.DestroyCountdownSeconds = envIntOr("DESTROY_COUNTDOWN", 5)
	return cfg
}
