// Package backend 实现平台控制面HTTP API：建房、查询agent列表。
// agent状态持久化在PostgreSQL，backend通过MQTT向指定agent下发建房指令。
package backend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5/pgxpool"

	"mr1v1-collector/internal/agentproto"
	"mr1v1-collector/internal/config"
)

// Backend 持有MQTT连接和PG连接池，提供HTTP API。
type Backend struct {
	cfg    *config.BackendConfig
	client mqtt.Client
	pool   *pgxpool.Pool
}

// New 连接MQTT broker和PostgreSQL，返回可用的Backend。
func New(cfg *config.BackendConfig) (*Backend, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		cfg.DB.Host, cfg.DB.Port, cfg.DB.User, cfg.DB.Pass, cfg.DB.DBName, cfg.DB.SSLMode, cfg.DB.Timezone)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

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
		pool.Close()
		return nil, fmt.Errorf("connect mqtt broker %s: %w", cfg.MQTT.Broker, token.Error())
	}

	// 订阅status回报，仅用于日志
	b := &Backend{cfg: cfg, client: client, pool: pool}
	if token := client.Subscribe(agentproto.StatusSubscribeFilter, 1, b.onStatus); token.Wait() && token.Error() != nil {
		client.Disconnect(250)
		pool.Close()
		return nil, fmt.Errorf("subscribe %s: %w", agentproto.StatusSubscribeFilter, token.Error())
	}

	return b, nil
}

// Close 断开MQTT连接并关闭PG连接池。
func (b *Backend) Close() {
	b.client.Disconnect(250)
	b.pool.Close()
}

// Handler 返回HTTP API路由。
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

func (b *Backend) onStatus(_ mqtt.Client, msg mqtt.Message) {
	var status agentproto.StatusReport
	if err := json.Unmarshal(msg.Payload(), &status); err != nil {
		slog.Error("decode status report failed", "error", err)
		return
	}
	slog.Info("agent status", "uuid", status.UUID, "match_id", status.MatchID,
		"port", status.Port, "state", status.State, "message", status.Message)
}

type createMatchRequest struct {
	P0SteamID  string `json:"p0_steamid"`
	P1SteamID  string `json:"p1_steamid"`
	ServerName string `json:"server_name"`
}

type createMatchResponse struct {
	MatchID string `json:"match_id"`
	UUID    string `json:"uuid"`
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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	uuid, port, err := b.pickAgentPort(ctx)
	if err != nil {
		slog.Error("pick agent failed", "error", err)
		http.Error(w, "no idle agent available", http.StatusServiceUnavailable)
		return
	}

	image, err := b.activeRehldsImage(ctx)
	if err != nil {
		slog.Error("get rehlds image failed", "error", err)
		http.Error(w, "rehlds config not found", http.StatusServiceUnavailable)
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
		Image:      image,
	}
	payload, _ := json.Marshal(cmd)

	topic := agentproto.CreateTopic(uuid)
	if token := b.client.Publish(topic, 1, false, payload); token.Wait() && token.Error() != nil {
		slog.Error("publish create command failed", "error", token.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	slog.Info("dispatched create command", "match_id", matchID, "uuid", uuid, "port", port, "image", image)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(createMatchResponse{MatchID: matchID, UUID: uuid, Port: port})
}

// pickAgentPort 从PG查询一个可用agent并分配端口。
// agent可用条件：status='enabled'，心跳在stale阈值内，rehlds_run_max>0，
// rehlds_port_range格式"start-end"，且有空闲端口。
func (b *Backend) pickAgentPort(ctx context.Context) (uuid string, port int, err error) {
	stale := b.cfg.AgentStaleSeconds
	if stale <= 0 {
		stale = 30
	}

	rows, err := b.pool.Query(ctx, `
		SELECT uuid, rehlds_port_range, rehlds_run_max
		FROM mr1v1_agent
		WHERE status = 'enabled'
		  AND heartbeat_time > NOW() - ($1 || ' seconds')::INTERVAL
		  AND rehlds_run_max > 0
		  AND rehlds_port_range != ''
		ORDER BY heartbeat_time DESC
	`, stale)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()

	type candidate struct {
		uuid       string
		portRange  string
		runMax     int
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.uuid, &c.portRange, &c.runMax); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return "", 0, fmt.Errorf("no available agent")
	}

	// 查各候选agent正在运行的容器端口
	for _, c := range candidates {
		var start, end int
		if _, err := fmt.Sscanf(c.portRange, "%d-%d", &start, &end); err != nil {
			continue
		}

		// 查该agent当前running的比赛端口
		busyRows, err := b.pool.Query(ctx, `
			SELECT port FROM mr1v1_match_status
			WHERE agent_uuid = $1 AND state = 'running'
		`, c.uuid)
		if err != nil {
			// 表可能不存在，降级直接用第一个端口
			return c.uuid, start, nil
		}
		busy := map[int]bool{}
		for busyRows.Next() {
			var p int
			busyRows.Scan(&p)
			busy[p] = true
		}
		busyRows.Close()

		for p := start; p <= end && p < start+c.runMax; p++ {
			if !busy[p] {
				return c.uuid, p, nil
			}
		}
	}
	return "", 0, fmt.Errorf("all agents are at capacity")
}

// activeRehldsImage 从PG查询当前生效的rehlds镜像。
func (b *Backend) activeRehldsImage(ctx context.Context) (string, error) {
	var image string
	err := b.pool.QueryRow(ctx, `
		SELECT image FROM mr1v1_rehlds_config
		WHERE is_active = TRUE
		ORDER BY id DESC LIMIT 1
	`).Scan(&image)
	if err != nil {
		return "", fmt.Errorf("query rehlds config: %w", err)
	}
	return image, nil
}

type agentRow struct {
	UUID          string    `json:"uuid"`
	Hostname      string    `json:"hostname"`
	PublicIP      string    `json:"public_ip"`
	LocalIP       string    `json:"local_ip"`
	CPU           string    `json:"cpu"`
	MemMB         int64     `json:"mem_mb"`
	DiskGB        int64     `json:"disk_gb"`
	Status        string    `json:"status"`
	RehldsRunMax  int       `json:"rehlds_run_max"`
	PortRange     string    `json:"rehlds_port_range"`
	HeartbeatTime time.Time `json:"heartbeat_time"`
}

func (b *Backend) handleListAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := b.pool.Query(r.Context(), `
		SELECT uuid, hostname, public_ip, local_ip, cpu, mem_mb, disk_gb,
		       status, rehlds_run_max, rehlds_port_range, heartbeat_time
		FROM mr1v1_agent
		ORDER BY heartbeat_time DESC
	`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []agentRow
	for rows.Next() {
		var a agentRow
		if err := rows.Scan(&a.UUID, &a.Hostname, &a.PublicIP, &a.LocalIP,
			&a.CPU, &a.MemMB, &a.DiskGB, &a.Status,
			&a.RehldsRunMax, &a.PortRange, &a.HeartbeatTime); err != nil {
			continue
		}
		result = append(result, a)
	}
	if result == nil {
		result = []agentRow{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func generateMatchID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
