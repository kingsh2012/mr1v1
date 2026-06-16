// Package gateway 实现HTTP /record接收 -> 内存队列 -> MQTT发布。
package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"mr1v1-collector/internal/config"
	"mr1v1-collector/internal/envelope"
)

// Server 持有HTTP handler和MQTT发布所需的状态。
type Server struct {
	cfg         *config.GatewayConfig
	client      mqtt.Client
	ownsClient  bool // true时Close()负责断开连接
	queue       chan envelope.Envelope
	// onEnvelope, if set, is called synchronously for every accepted
	// envelope before it is queued for MQTT publish. Used by the agent to
	// react to events such as mr1v1_match_end (container teardown).
	onEnvelope func(envelope.Envelope)
}

// New 根据配置创建Server并连接MQTT broker。
func New(cfg *config.GatewayConfig) (*Server, error) {
	return NewWithHook(cfg, nil)
}

// NewWithHook 与New相同，但额外注册一个onEnvelope回调。
func NewWithHook(cfg *config.GatewayConfig, onEnvelope func(envelope.Envelope)) (*Server, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.MQTT.Broker).
		SetClientID(cfg.MQTT.ClientID).
		SetAutoReconnect(true).
		SetConnectTimeout(10 * time.Second).
		SetKeepAlive(30 * time.Second)
	if cfg.MQTT.User != "" {
		opts.SetUsername(cfg.MQTT.User)
		opts.SetPassword(cfg.MQTT.Pass)
	}

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, fmt.Errorf("connect mqtt broker %s: %w", cfg.MQTT.Broker, token.Error())
	}

	capacity := cfg.Queue.Capacity
	if capacity <= 0 {
		capacity = 10000
	}

	s := &Server{
		cfg:        cfg,
		client:     client,
		ownsClient: true,
		queue:      make(chan envelope.Envelope, capacity),
		onEnvelope: onEnvelope,
	}
	go s.publishLoop()
	return s, nil
}

// NewWithClient 使用调用方已建立的MQTT连接创建Server。
// 连接的生命周期由调用方管理，Close()不会断开连接。
func NewWithClient(client mqtt.Client, topicPrefix string, capacity int, onEnvelope func(envelope.Envelope)) *Server {
	if capacity <= 0 {
		capacity = 10000
	}
	s := &Server{
		cfg: &config.GatewayConfig{},
		client:     client,
		ownsClient: false,
		queue:      make(chan envelope.Envelope, capacity),
		onEnvelope: onEnvelope,
	}
	s.cfg.MQTT.TopicPrefix = topicPrefix
	go s.publishLoop()
	return s
}

// Handler 返回注册了 /record 和 /healthz 的 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /record", s.handleRecord)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (s *Server) handleRecord(w http.ResponseWriter, r *http.Request) {
	var env envelope.Envelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	if env.MatchID == "" || env.Type == "" {
		http.Error(w, "match_id and type are required", http.StatusBadRequest)
		return
	}

	if !envelope.KnownTypes[env.Type] {
		slog.Warn("received unknown event type, forwarding anyway", "type", env.Type, "match_id", env.MatchID)
	}

	if s.onEnvelope != nil {
		s.onEnvelope(env)
	}

	select {
	case s.queue <- env:
		w.WriteHeader(http.StatusOK)
	default:
		slog.Error("queue full, dropping event", "type", env.Type, "match_id", env.MatchID)
		http.Error(w, "queue full", http.StatusServiceUnavailable)
	}
}

func (s *Server) publishLoop() {
	for env := range s.queue { //nolint:revive
		payload, err := json.Marshal(env)
		if err != nil {
			slog.Error("marshal envelope failed", "error", err, "type", env.Type, "match_id", env.MatchID)
			continue
		}

		topic := fmt.Sprintf("%s/%s", s.cfg.MQTT.TopicPrefix, env.MatchID)
		token := s.client.Publish(topic, 1, false, payload)
		if token.Wait() && token.Error() != nil {
			slog.Error("publish mqtt failed", "error", token.Error(), "topic", topic, "type", env.Type)
		}
	}
}

// Close 关闭队列并（若连接由本Server创建）断开MQTT连接。
func (s *Server) Close() {
	close(s.queue)
	if s.ownsClient {
		s.client.Disconnect(250)
	}
}
