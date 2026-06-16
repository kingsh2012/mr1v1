// Package backend implements the platform side of the control plane: it
// maintains an in-memory registry of agents (via their heartbeats), and
// exposes an HTTP API to create matches by picking an idle agent/port and
// dispatching a create command over MQTT. See AGENT_ARCHITECTURE_DESIGN.md
// sections 4 and 8.
package backend

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"mr1v1-collector/internal/agentproto"
	"mr1v1-collector/internal/config"
)

const defaultAgentStale = 30 * time.Second

// agentRecord is the registry entry for one agent.
type agentRecord struct {
	heartbeat agentproto.Heartbeat
	lastSeen  time.Time
	// allocated tracks ports this backend has assigned to matches since the
	// agent's last heartbeat, so the same port isn't handed out twice while
	// waiting for the agent's busy_ports list to catch up.
	allocated map[int]string // port -> match_id
}

// Backend holds the agent registry and MQTT connection.
type Backend struct {
	cfg    *config.BackendConfig
	client mqtt.Client

	mu     sync.Mutex
	agents map[string]*agentRecord // host_id -> record
}

// New connects to the MQTT broker, subscribes to agent heartbeats/status
// reports, and returns a ready-to-use Backend.
func New(cfg *config.BackendConfig) (*Backend, error) {
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

	b := &Backend{
		cfg:    cfg,
		client: client,
		agents: make(map[string]*agentRecord),
	}

	if token := client.Subscribe(agentproto.HeartbeatSubscribeFilter, 0, b.onHeartbeat); token.Wait() && token.Error() != nil {
		client.Disconnect(250)
		return nil, fmt.Errorf("subscribe %s: %w", agentproto.HeartbeatSubscribeFilter, token.Error())
	}
	if token := client.Subscribe(agentproto.StatusSubscribeFilter, 1, b.onStatus); token.Wait() && token.Error() != nil {
		client.Disconnect(250)
		return nil, fmt.Errorf("subscribe %s: %w", agentproto.StatusSubscribeFilter, token.Error())
	}

	return b, nil
}

// Close disconnects the MQTT connection.
func (b *Backend) Close() {
	b.client.Disconnect(250)
}

// Handler returns the HTTP API: POST /matches, GET /agents, GET /healthz.
func (b *Backend) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /matches", b.handleCreateMatch)
	mux.HandleFunc("GET /agents", b.handleListAgents)
	mux.HandleFunc("GET /healthz", b.handleHealthz)
	return mux
}

func (b *Backend) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (b *Backend) onHeartbeat(_ mqtt.Client, msg mqtt.Message) {
	var hb agentproto.Heartbeat
	if err := json.Unmarshal(msg.Payload(), &hb); err != nil {
		slog.Error("decode heartbeat failed", "error", err, "payload", string(msg.Payload()))
		return
	}
	if hb.HostID == "" {
		slog.Warn("heartbeat missing host_id", "payload", string(msg.Payload()))
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	rec, ok := b.agents[hb.HostID]
	if !ok {
		rec = &agentRecord{allocated: make(map[int]string)}
		b.agents[hb.HostID] = rec
	}
	rec.heartbeat = hb
	rec.lastSeen = time.Now()

	// Once the agent's own busy_ports reflects an allocation, drop our
	// local pending-allocation entry for it.
	busy := make(map[int]bool, len(hb.BusyPorts))
	for _, p := range hb.BusyPorts {
		busy[p] = true
	}
	for port := range rec.allocated {
		if busy[port] {
			delete(rec.allocated, port)
		}
	}
}

func (b *Backend) onStatus(_ mqtt.Client, msg mqtt.Message) {
	var status agentproto.StatusReport
	if err := json.Unmarshal(msg.Payload(), &status); err != nil {
		slog.Error("decode status report failed", "error", err, "payload", string(msg.Payload()))
		return
	}

	slog.Info("agent status report", "host_id", status.HostID, "match_id", status.MatchID,
		"port", status.Port, "state", status.State, "message", status.Message)

	if status.State != agentproto.StateStopped && status.State != agentproto.StateError {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if rec, ok := b.agents[status.HostID]; ok {
		delete(rec.allocated, status.Port)
	}
}

type createMatchRequest struct {
	P0SteamID  string `json:"p0_steamid"`
	P1SteamID  string `json:"p1_steamid"`
	ServerName string `json:"server_name"`
	Image      string `json:"image"`
}

type createMatchResponse struct {
	MatchID string `json:"match_id"`
	HostID  string `json:"host_id"`
	Port    int    `json:"port"`
}

func (b *Backend) handleCreateMatch(w http.ResponseWriter, r *http.Request) {
	var req createMatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.P0SteamID == "" || req.P1SteamID == "" {
		http.Error(w, "p0_steamid and p1_steamid are required", http.StatusBadRequest)
		return
	}

	matchID, err := generateMatchID()
	if err != nil {
		slog.Error("generate match id failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	hostID, port, ok := b.pickAgentPort()
	if !ok {
		http.Error(w, "no idle agent available", http.StatusServiceUnavailable)
		return
	}

	serverName := req.ServerName
	if serverName == "" {
		serverName = fmt.Sprintf("mr1v1 1v1 #%s", matchID[:8])
	}

	cmd := agentproto.CreateCommand{
		MatchID:    matchID,
		ServerName: serverName,
		Port:       port,
		P0SteamID:  req.P0SteamID,
		P1SteamID:  req.P1SteamID,
		Image:      req.Image,
	}
	payload, err := json.Marshal(cmd)
	if err != nil {
		slog.Error("marshal create command failed", "error", err, "match_id", matchID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	topic := agentproto.CreateTopic(hostID)
	if token := b.client.Publish(topic, 1, false, payload); token.Wait() && token.Error() != nil {
		slog.Error("publish create command failed", "error", token.Error(), "topic", topic)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	slog.Info("dispatched create command", "match_id", matchID, "host_id", hostID, "port", port)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(createMatchResponse{MatchID: matchID, HostID: hostID, Port: port})
}

// pickAgentPort selects the first non-stale agent with a free port in its
// configured range and reserves that port locally.
func (b *Backend) pickAgentPort() (hostID string, port int, ok bool) {
	stale := time.Duration(b.cfg.AgentStaleSeconds) * time.Second
	if stale <= 0 {
		stale = defaultAgentStale
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	for id, rec := range b.agents {
		if now.Sub(rec.lastSeen) > stale {
			continue
		}

		busy := make(map[int]bool, len(rec.heartbeat.BusyPorts)+len(rec.allocated))
		for _, p := range rec.heartbeat.BusyPorts {
			busy[p] = true
		}
		for p := range rec.allocated {
			busy[p] = true
		}

		for p := rec.heartbeat.PortRangeStart; p <= rec.heartbeat.PortRangeEnd; p++ {
			if !busy[p] {
				rec.allocated[p] = ""
				return id, p, true
			}
		}
	}
	return "", 0, false
}

type agentSummary struct {
	HostID    string `json:"host_id"`
	PublicIP  string `json:"public_ip"`
	PrivateIP string `json:"private_ip"`
	LastSeen  int64  `json:"last_seen"`
	BusyPorts []int  `json:"busy_ports"`
}

func (b *Backend) handleListAgents(w http.ResponseWriter, _ *http.Request) {
	b.mu.Lock()
	summaries := make([]agentSummary, 0, len(b.agents))
	for id, rec := range b.agents {
		summaries = append(summaries, agentSummary{
			HostID:    id,
			PublicIP:  rec.heartbeat.PublicIP,
			PrivateIP: rec.heartbeat.PrivateIP,
			LastSeen:  rec.lastSeen.Unix(),
			BusyPorts: rec.heartbeat.BusyPorts,
		})
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summaries)
}

func generateMatchID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
