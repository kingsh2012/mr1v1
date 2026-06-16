// Package config 提供各服务的配置结构体及从环境变量加载的函数。
package config

import (
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
	HostID struct {
		File string
	}
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
		PrivateIP       string
		PortRangeStart  int
		PortRangeEnd    int
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
	AgentStaleSeconds int
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
	cfg.MQTT.ClientID = envOr("MQTT_CLIENT_ID", "mr1v1-consumer")
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
//	HTTP_LISTEN          (default: 0.0.0.0:8080)
//	MQTT_BROKER          (default: tcp://localhost:1883)
//	MQTT_CLIENT_ID       (default: mr1v1-backend)
//	MQTT_USER
//	MQTT_PASS
//	AGENT_STALE_SECONDS  (default: 30)
func LoadBackendFromEnv() *BackendConfig {
	cfg := &BackendConfig{}
	cfg.HTTP.Listen = envOr("HTTP_LISTEN", "0.0.0.0:8080")
	cfg.MQTT.Broker = envOr("MQTT_BROKER", "tcp://localhost:1883")
	cfg.MQTT.ClientID = envOr("MQTT_CLIENT_ID", "mr1v1-backend")
	cfg.MQTT.User = envOr("MQTT_USER", "")
	cfg.MQTT.Pass = envOr("MQTT_PASS", "")
	cfg.AgentStaleSeconds = envIntOr("AGENT_STALE_SECONDS", 30)
	return cfg
}

// LoadAgentFromEnv 从环境变量加载 agent 配置。
//
// 环境变量：
//
//	HOST_ID_FILE             (default: ./data/host_id)
//	HTTP_LISTEN              (default: 0.0.0.0:7778)
//	MQTT_BROKER              (default: tcp://localhost:1883)
//	MQTT_CLIENT_ID           (default: mr1v1-agent)
//	MQTT_USER
//	MQTT_PASS
//	QUEUE_CAPACITY           (default: 10000)
//	HEARTBEAT_INTERVAL       (default: 10)
//	PUBLIC_IP
//	PRIVATE_IP
//	PORT_RANGE_START         (default: 27015)
//	PORT_RANGE_END           (default: 27025)
//	DOCKER_IMAGE
//	DOCKER_STOP_TIMEOUT      (default: 15)
//	DESTROY_COMMAND          (default: mr1v1_match_destroy)
//	DESTROY_COUNTDOWN        (default: 5)
func LoadAgentFromEnv() *AgentConfig {
	cfg := &AgentConfig{}
	cfg.HostID.File = envOr("HOST_ID_FILE", "./data/host_id")
	cfg.HTTP.Listen = envOr("HTTP_LISTEN", "0.0.0.0:7778")
	cfg.MQTT.Broker = envOr("MQTT_BROKER", "tcp://localhost:1883")
	cfg.MQTT.ClientID = envOr("MQTT_CLIENT_ID", "mr1v1-agent")
	cfg.MQTT.User = envOr("MQTT_USER", "")
	cfg.MQTT.Pass = envOr("MQTT_PASS", "")
	cfg.Queue.Capacity = envIntOr("QUEUE_CAPACITY", 10000)
	cfg.Heartbeat.IntervalSeconds = envIntOr("HEARTBEAT_INTERVAL", 10)
	cfg.Heartbeat.PublicIP = envOr("PUBLIC_IP", "")
	cfg.Heartbeat.PrivateIP = envOr("PRIVATE_IP", "")
	cfg.Heartbeat.PortRangeStart = envIntOr("PORT_RANGE_START", 27015)
	cfg.Heartbeat.PortRangeEnd = envIntOr("PORT_RANGE_END", 27025)
	cfg.Docker.DefaultImage = envOr("DOCKER_IMAGE", "")
	cfg.Docker.StopTimeoutSeconds = envIntOr("DOCKER_STOP_TIMEOUT", 15)
	cfg.Docker.DestroyCommand = envOr("DESTROY_COMMAND", "mr1v1_match_destroy")
	cfg.Docker.DestroyCountdownSeconds = envIntOr("DESTROY_COUNTDOWN", 5)
	return cfg
}
