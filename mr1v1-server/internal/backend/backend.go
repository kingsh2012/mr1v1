// Package backend 实现平台控制面HTTP API：建房、查询agent列表。
// agent状态持久化在PostgreSQL，backend通过MQTT向指定agent下发建房指令。
package backend

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	mathrand "math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5/pgxpool"

	"mr1v1-server/internal/agentproto"
	"mr1v1-server/internal/config"
	"mr1v1-server/pkg/a2s"
)

//go:embed static
var staticFiles embed.FS

// Backend 持有MQTT连接和PG连接池，提供HTTP API。
type Backend struct {
	cfg        *config.BackendConfig
	client     mqtt.Client
	pool       *pgxpool.Pool
	cancelLoop context.CancelFunc
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
	ctx, cancel := context.WithCancel(context.Background())
	b := &Backend{cfg: cfg, client: client, pool: pool, cancelLoop: cancel}
	if token := client.Subscribe(agentproto.StatusSubscribeFilter, 1, b.onStatus); token.Wait() && token.Error() != nil {
		cancel()
		client.Disconnect(250)
		pool.Close()
		return nil, fmt.Errorf("subscribe %s: %w", agentproto.StatusSubscribeFilter, token.Error())
	}

	go b.timeoutLoop(ctx)

	return b, nil
}

// Close 断开MQTT连接并关闭PG连接池。
func (b *Backend) Close() {
	b.cancelLoop()
	b.client.Disconnect(250)
	b.pool.Close()
}

// timeoutLoop 每分钟扫一次活跃比赛，超时的自动强制销毁并标记为 timeout 状态。
func (b *Backend) timeoutLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.sweepTimeoutMatches(ctx)
		}
	}
}

func (b *Backend) sweepTimeoutMatches(ctx context.Context) {
	waitSec := b.cfg.MatchWaitingTimeoutSeconds
	playSec := b.cfg.MatchPlayingTimeoutSeconds

	rows, err := b.pool.Query(ctx, `
		SELECT match_id, agent_uuid, state
		FROM mr1v1_match
		WHERE (state = 'waiting'  AND update_time < NOW() - $1 * INTERVAL '1 second')
		   OR (state = 'playing'  AND update_time < NOW() - $2 * INTERVAL '1 second')
		   OR (state = 'creating' AND update_time < NOW() - $1 * INTERVAL '1 second')
	`, waitSec, playSec)
	if err != nil {
		slog.Error("timeout sweep query failed", "error", err)
		return
	}
	defer rows.Close()

	type staleMatch struct {
		matchID   string
		agentUUID string
		state     string
	}
	var stale []staleMatch
	for rows.Next() {
		var m staleMatch
		if err := rows.Scan(&m.matchID, &m.agentUUID, &m.state); err == nil {
			stale = append(stale, m)
		}
	}

	for _, m := range stale {
		slog.Info("auto-timeout match", "match_id", m.matchID, "state", m.state)
		cmd := agentproto.DestroyCommand{MatchID: m.matchID, Force: true}
		payload, _ := json.Marshal(cmd)
		topic := agentproto.DestroyTopic(m.agentUUID)
		if token := b.client.Publish(topic, 1, false, payload); token.Wait() && token.Error() != nil {
			slog.Error("publish timeout destroy failed", "error", token.Error(), "match_id", m.matchID)
		}
		b.pool.Exec(ctx,
			`UPDATE mr1v1_match SET state='timeout', update_time=NOW() WHERE match_id=$1`, m.matchID)
		b.writeLog(m.matchID, "platform", "timeout_destroy", map[string]any{
				"prev_state":   m.state,
				"agent_uuid":   m.agentUUID,
				"waiting_sec":  waitSec,
				"playing_sec":  playSec,
			})
	}
}

// Handler 返回HTTP API路由。
func (b *Backend) Handler() http.Handler {
	mux := http.NewServeMux()
	// 比赛
	mux.HandleFunc("POST /api/matches", b.handleCreateMatch)
	mux.HandleFunc("GET /api/matches", b.handleListMatches)
	mux.HandleFunc("GET /api/matches/{id}/logs", b.handleMatchLogs)
	mux.HandleFunc("GET /api/matches/{id}/server", b.handleMatchServer)
	mux.HandleFunc("POST /api/matches/{id}/end", b.handleEndMatch)
	mux.HandleFunc("POST /api/matches/{id}/destroy", b.handleDestroyMatch)
	// Agent 管理
	mux.HandleFunc("GET /api/agents", b.handleListAgents)
	mux.HandleFunc("PATCH /api/agents/{uuid}", b.handleUpdateAgent)
	mux.HandleFunc("GET /api/agents/{uuid}/containers", b.handleAgentContainers)
	// Rehlds 镜像配置
	mux.HandleFunc("GET /api/rehlds-configs", b.handleListRehldsConfigs)
	mux.HandleFunc("POST /api/rehlds-configs", b.handleCreateRehldsConfig)
	mux.HandleFunc("PATCH /api/rehlds-configs/{id}/activate", b.handleActivateRehldsConfig)
	// 健康检查 & 静态文件
	mux.HandleFunc("GET /api/healthz", b.handleHealthz)
	mux.Handle("/", b.staticHandler())
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

	// agent running → match waiting（容器就绪，等待玩家）
	// agent error   → match error
	// agent stopped → 若 match 还未 finished 则标记 error
	var newState, action string
	switch status.State {
	case agentproto.StateRunning:
		newState = "waiting"
		action = "container_started"
	case agentproto.StateError:
		newState = "error"
		action = "container_error"
	case agentproto.StateStopped:
		// 只有意外停止才标 error；finished/terminated/timeout/error 已是终态不覆盖
		b.pool.Exec(context.Background(),
			`UPDATE mr1v1_match SET state='error', update_time=NOW()
			 WHERE match_id=$1 AND state NOT IN ('finished','terminated','timeout','error')`,
			status.MatchID)
		b.writeLog(status.MatchID, "agent", "container_stopped", status)
		return
	default:
		return
	}
	if _, err := b.pool.Exec(context.Background(),
		`UPDATE mr1v1_match SET state=$1, update_time=NOW() WHERE match_id=$2`,
		newState, status.MatchID,
	); err != nil {
		slog.Error("update match state failed", "error", err, "match_id", status.MatchID)
	}
	b.writeLog(status.MatchID, "agent", action, status)
}

type createMatchRequest struct {
	P0SteamID  string `json:"p0_steamid"`
	P1SteamID  string `json:"p1_steamid"`
	ServerName string `json:"server_name"`
}

type createMatchResponse struct {
	MatchID   string `json:"match_id"`
	UUID      string `json:"uuid"`
	PublicIP  string `json:"public_ip"`
	Port      int    `json:"port"`
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
	uuid, publicIP, port, err := b.pickAgentPort(ctx)
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

	// 写入比赛记录
	if _, err := b.pool.Exec(ctx, `
		INSERT INTO mr1v1_match
			(match_id, p0_steamid, p1_steamid, server_name, agent_uuid, port, image, state)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'creating')
	`, matchID, req.P0SteamID, req.P1SteamID, serverName, uuid, port, image); err != nil {
		slog.Error("insert match failed", "error", err)
		// 非致命错误，不阻断响应
	}

	b.writeLog(matchID, "platform", "create_dispatched", map[string]any{
		"agent_uuid": uuid,
		"command":    cmd,
	})

	slog.Info("dispatched create command", "match_id", matchID, "uuid", uuid, "port", port, "image", image)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(createMatchResponse{MatchID: matchID, UUID: uuid, PublicIP: publicIP, Port: port})
}

// pickAgentPort 从PG查询一个可用agent并分配端口。
// agent可用条件：status='enabled'，心跳在stale阈值内，rehlds_run_max>0，
// rehlds_port_range格式"start-end"，且有空闲端口。
func (b *Backend) pickAgentPort(ctx context.Context) (uuid, publicIP string, port int, err error) {
	stale := b.cfg.AgentStaleSeconds
	if stale <= 0 {
		stale = 30
	}

	rows, err := b.pool.Query(ctx, `
		SELECT uuid, public_ip, rehlds_port_range, rehlds_run_max
		FROM mr1v1_agent
		WHERE status = 'enabled'
		  AND heartbeat_time > NOW() - $1 * INTERVAL '1 second'
		  AND rehlds_run_max > 0
		  AND rehlds_port_range != ''
		ORDER BY heartbeat_time DESC
	`, stale)
	if err != nil {
		return "", "", 0, err
	}
	defer rows.Close()

	type candidate struct {
		uuid      string
		publicIP  string
		portRange string
		runMax    int
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.uuid, &c.publicIP, &c.portRange, &c.runMax); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return "", "", 0, fmt.Errorf("no available agent")
	}

	// 查各候选agent正在运行的容器端口
	for _, c := range candidates {
		var start, end int
		if _, err := fmt.Sscanf(c.portRange, "%d-%d", &start, &end); err != nil {
			continue
		}

		// 查该agent当前活跃比赛占用的端口
		busyRows, err := b.pool.Query(ctx, `
			SELECT port FROM mr1v1_match
			WHERE agent_uuid = $1 AND state IN ('creating','waiting','playing')
		`, c.uuid)
		if err != nil {
			// 表可能不存在，降级直接用第一个端口
			return c.uuid, c.publicIP, start, nil
		}
		busy := map[int]bool{}
		for busyRows.Next() {
			var p int
			busyRows.Scan(&p)
			busy[p] = true
		}
		busyRows.Close()

		var available []int
		for p := start; p <= end; p++ {
			if !busy[p] {
				available = append(available, p)
			}
		}
		if len(available) == 0 || len(busy) >= c.runMax {
			continue
		}
		mathrand.Shuffle(len(available), func(i, j int) { available[i], available[j] = available[j], available[i] })
		return c.uuid, c.publicIP, available[0], nil
	}
	return "", "", 0, fmt.Errorf("all agents are at capacity")
}

type matchRow struct {
	MatchID    string    `json:"match_id"`
	P0SteamID  string    `json:"p0_steamid"`
	P1SteamID  string    `json:"p1_steamid"`
	ServerName string    `json:"server_name"`
	AgentUUID  string    `json:"agent_uuid"`
	Port       int       `json:"port"`
	Image      string    `json:"image"`
	State      string    `json:"state"`
	CreateTime time.Time `json:"create_time"`
	UpdateTime time.Time `json:"update_time"`
}

func (b *Backend) handleListMatches(w http.ResponseWriter, r *http.Request) {
	rows, err := b.pool.Query(r.Context(), `
		SELECT match_id, p0_steamid, p1_steamid, server_name,
		       agent_uuid, port, image, state, create_time, update_time
		FROM mr1v1_match
		ORDER BY create_time DESC
		LIMIT 100
	`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []matchRow
	for rows.Next() {
		var m matchRow
		if err := rows.Scan(&m.MatchID, &m.P0SteamID, &m.P1SteamID, &m.ServerName,
			&m.AgentUUID, &m.Port, &m.Image, &m.State, &m.CreateTime, &m.UpdateTime); err != nil {
			continue
		}
		result = append(result, m)
	}
	if result == nil {
		result = []matchRow{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleEndMatch 优雅结束比赛：agent 先发 RCON 倒计时再 docker stop。
func (b *Backend) handleEndMatch(w http.ResponseWriter, r *http.Request) {
	b.dispatchDestroy(w, r, false)
}

// handleDestroyMatch 强制销毁：agent 直接 docker stop，不走 RCON。
func (b *Backend) handleDestroyMatch(w http.ResponseWriter, r *http.Request) {
	b.dispatchDestroy(w, r, true)
}

func (b *Backend) dispatchDestroy(w http.ResponseWriter, r *http.Request, force bool) {
	matchID := r.PathValue("id")
	if matchID == "" {
		http.Error(w, "match_id required", http.StatusBadRequest)
		return
	}

	var agentUUID string
	err := b.pool.QueryRow(r.Context(),
		`SELECT agent_uuid FROM mr1v1_match WHERE match_id=$1`, matchID,
	).Scan(&agentUUID)
	if err != nil {
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}

	cmd := agentproto.DestroyCommand{MatchID: matchID, Force: force}
	payload, _ := json.Marshal(cmd)
	topic := agentproto.DestroyTopic(agentUUID)
	if token := b.client.Publish(topic, 1, false, payload); token.Wait() && token.Error() != nil {
		slog.Error("publish destroy command failed", "error", token.Error(), "match_id", matchID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 平台主动终止 → terminated（不参与结算），区别于正常 finished 和意外 error
	b.pool.Exec(r.Context(),
		`UPDATE mr1v1_match SET state='terminated', update_time=NOW() WHERE match_id=$1`, matchID)

	action := "end_dispatched"
	if force {
		action = "destroy_dispatched"
	}
	b.writeLog(matchID, "platform", action, map[string]any{
		"agent_uuid": agentUUID,
		"command":    cmd,
	})

	slog.Info("dispatched destroy command", "match_id", matchID, "force", force, "agent", agentUUID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type opLogRow struct {
	ID        int64     `json:"id"`
	MatchID   string    `json:"match_id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

func (b *Backend) handleMatchLogs(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("id")
	rows, err := b.pool.Query(r.Context(), `
		SELECT id, match_id, actor, action, detail, created_at
		FROM mr1v1_operation_log
		WHERE match_id = $1
		ORDER BY created_at ASC
	`, matchID)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var result []opLogRow
	for rows.Next() {
		var row opLogRow
		if err := rows.Scan(&row.ID, &row.MatchID, &row.Actor, &row.Action, &row.Detail, &row.CreatedAt); err != nil {
			continue
		}
		result = append(result, row)
	}
	if result == nil {
		result = []opLogRow{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// writeLog 写一条操作日志，detail 传任意可序列化结构体或 map，非阻塞。
func (b *Backend) writeLog(matchID, actor, action string, detail any) {
	var detailStr string
	if detail != nil {
		if s, ok := detail.(string); ok {
			detailStr = s
		} else if b, err := json.Marshal(detail); err == nil {
			detailStr = string(b)
		}
	}
	if _, err := b.pool.Exec(context.Background(),
		`INSERT INTO mr1v1_operation_log (match_id, actor, action, detail)
		 VALUES ($1, $2, $3, $4)`,
		matchID, actor, action, detailStr,
	); err != nil {
		slog.Error("write operation log failed", "error", err, "action", action)
	}
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
	UUID               string    `json:"uuid"`
	Hostname           string    `json:"hostname"`
	PublicIP           string    `json:"public_ip"`
	LocalIP            string    `json:"local_ip"`
	CPU                string    `json:"cpu"`
	MemMB              int64     `json:"mem_mb"`
	DiskGB             int64     `json:"disk_gb"`
	Status             string    `json:"status"`
	RehldsRunMax       int       `json:"rehlds_run_max"`
	PortRange          string    `json:"rehlds_port_range"`
	RunningContainers  string    `json:"running_containers"`
	CreateTime         time.Time `json:"create_time"`
	UpdateTime         time.Time `json:"update_time"`
	HeartbeatTime      time.Time `json:"heartbeat_time"`
}

func (b *Backend) handleListAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := b.pool.Query(r.Context(), `
		SELECT uuid, hostname, public_ip, local_ip, cpu, mem_mb, disk_gb,
		       status, rehlds_run_max, rehlds_port_range, running_containers,
		       create_time, update_time, heartbeat_time
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
			&a.RehldsRunMax, &a.PortRange, &a.RunningContainers,
			&a.CreateTime, &a.UpdateTime, &a.HeartbeatTime); err != nil {
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

// handleAgentContainers 返回指定 agent 最近一次心跳上报的全量容器列表（含 ENV）。
func (b *Backend) handleAgentContainers(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	var raw []byte
	err := b.pool.QueryRow(r.Context(),
		`SELECT containers_json FROM mr1v1_agent WHERE uuid=$1`, uuid,
	).Scan(&raw)
	if err != nil {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}
	if len(raw) == 0 {
		raw = []byte("[]")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}

// handleUpdateAgent 更新 agent 的可配置字段（status/rehlds_run_max/rehlds_port_range）。
func (b *Backend) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	var req struct {
		Status       *string `json:"status"`
		RehldsRunMax *int    `json:"rehlds_run_max"`
		PortRange    *string `json:"rehlds_port_range"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	sets := []string{}
	args := []any{}
	n := 1
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status=$%d", n))
		args = append(args, *req.Status)
		n++
	}
	if req.RehldsRunMax != nil {
		sets = append(sets, fmt.Sprintf("rehlds_run_max=$%d", n))
		args = append(args, *req.RehldsRunMax)
		n++
	}
	if req.PortRange != nil {
		sets = append(sets, fmt.Sprintf("rehlds_port_range=$%d", n))
		args = append(args, *req.PortRange)
		n++
	}
	if len(sets) == 0 {
		http.Error(w, "nothing to update", http.StatusBadRequest)
		return
	}
	sets = append(sets, "update_time=NOW()")
	args = append(args, uuid)

	q := fmt.Sprintf("UPDATE mr1v1_agent SET %s WHERE uuid=$%d", strings.Join(sets, ","), n)
	if _, err := b.pool.Exec(r.Context(), q, args...); err != nil {
		slog.Error("update agent failed", "error", err, "uuid", uuid)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type rehldsConfigRow struct {
	ID         int64     `json:"id"`
	Image      string    `json:"image"`
	Version    string    `json:"version"`
	IsActive   bool      `json:"is_active"`
	CreateTime time.Time `json:"create_time"`
}

func (b *Backend) handleListRehldsConfigs(w http.ResponseWriter, r *http.Request) {
	rows, err := b.pool.Query(r.Context(),
		`SELECT id, image, version, is_active, create_time FROM mr1v1_rehlds_config ORDER BY id DESC`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []rehldsConfigRow
	for rows.Next() {
		var c rehldsConfigRow
		if err := rows.Scan(&c.ID, &c.Image, &c.Version, &c.IsActive, &c.CreateTime); err != nil {
			continue
		}
		result = append(result, c)
	}
	if result == nil {
		result = []rehldsConfigRow{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (b *Backend) handleCreateRehldsConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Image    string `json:"image"`
		Version  string `json:"version"`
		IsActive bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Image == "" {
		http.Error(w, "image is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if req.IsActive {
		b.pool.Exec(ctx, `UPDATE mr1v1_rehlds_config SET is_active=FALSE`)
	}
	var id int64
	err := b.pool.QueryRow(ctx,
		`INSERT INTO mr1v1_rehlds_config (image, version, is_active) VALUES ($1,$2,$3) RETURNING id`,
		req.Image, req.Version, req.IsActive,
	).Scan(&id)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func (b *Backend) handleActivateRehldsConfig(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	b.pool.Exec(ctx, `UPDATE mr1v1_rehlds_config SET is_active=FALSE`)
	if _, err := b.pool.Exec(ctx, `UPDATE mr1v1_rehlds_config SET is_active=TRUE WHERE id=$1`, id); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// staticHandler 服务嵌入的前端静态文件，未匹配的路径返回 index.html（SPA路由）。
func (b *Backend) staticHandler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// static 目录不存在时返回占位响应
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "frontend not built", http.StatusNotFound)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 尝试直接服务文件
		f, err := sub.(fs.ReadDirFS).Open(strings.TrimPrefix(r.URL.Path, "/"))
		if err == nil {
			f.(interface{ Close() error }).Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// 文件不存在 → 返回 index.html（交给前端路由处理）
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

type serverQueryResponse struct {
	Info    *a2s.ServerInfo  `json:"info"`
	Players *a2s.PlayerInfo  `json:"players"`
	Rules   *a2s.RulesInfo   `json:"rules"`
	InfoErr string           `json:"info_error,omitempty"`
	PlrErr  string           `json:"players_error,omitempty"`
	RuleErr string           `json:"rules_error,omitempty"`
}

func (b *Backend) handleMatchServer(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("id")
	var publicIP string
	var port int
	err := b.pool.QueryRow(r.Context(), `
		SELECT a.public_ip, m.port
		FROM mr1v1_match m
		JOIN mr1v1_agent a ON a.uuid = m.agent_uuid
		WHERE m.match_id = $1
	`, matchID).Scan(&publicIP, &port)
	if err != nil {
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}

	addr := fmt.Sprintf("%s:%d", publicIP, port)
	resp := serverQueryResponse{}

	type result[T any] struct {
		val *T
		err error
	}
	infoCh := make(chan result[a2s.ServerInfo], 1)
	plrCh := make(chan result[a2s.PlayerInfo], 1)
	ruleCh := make(chan result[a2s.RulesInfo], 1)

	query := func(fn func(*a2s.Client) (any, error), ch any) {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("a2s panic: %v", r)
				switch c := ch.(type) {
				case chan result[a2s.ServerInfo]:
					c <- result[a2s.ServerInfo]{err: err}
				case chan result[a2s.PlayerInfo]:
					c <- result[a2s.PlayerInfo]{err: err}
				case chan result[a2s.RulesInfo]:
					c <- result[a2s.RulesInfo]{err: err}
				}
			}
		}()
		cli, err := a2s.NewClient(addr, 3*time.Second, true) // goldsrc=true for CS 1.6/ReHLDS
		if err != nil {
			switch c := ch.(type) {
			case chan result[a2s.ServerInfo]:
				c <- result[a2s.ServerInfo]{err: err}
			case chan result[a2s.PlayerInfo]:
				c <- result[a2s.PlayerInfo]{err: err}
			case chan result[a2s.RulesInfo]:
				c <- result[a2s.RulesInfo]{err: err}
			}
			return
		}
		defer cli.Close()
		v, e := fn(cli)
		switch c := ch.(type) {
		case chan result[a2s.ServerInfo]:
			if e != nil {
				c <- result[a2s.ServerInfo]{err: e}
			} else {
				c <- result[a2s.ServerInfo]{val: v.(*a2s.ServerInfo)}
			}
		case chan result[a2s.PlayerInfo]:
			if e != nil {
				c <- result[a2s.PlayerInfo]{err: e}
			} else {
				c <- result[a2s.PlayerInfo]{val: v.(*a2s.PlayerInfo)}
			}
		case chan result[a2s.RulesInfo]:
			if e != nil {
				c <- result[a2s.RulesInfo]{err: e}
			} else {
				c <- result[a2s.RulesInfo]{val: v.(*a2s.RulesInfo)}
			}
		}
	}

	go query(func(c *a2s.Client) (any, error) { return c.QueryInfo() }, infoCh)
	go query(func(c *a2s.Client) (any, error) { return c.QueryPlayers() }, plrCh)
	go query(func(c *a2s.Client) (any, error) { return c.QueryRules() }, ruleCh)

	if r := <-infoCh; r.err != nil {
		resp.InfoErr = r.err.Error()
	} else {
		resp.Info = r.val
	}
	if r := <-plrCh; r.err != nil {
		resp.PlrErr = r.err.Error()
	} else {
		resp.Players = r.val
	}
	if r := <-ruleCh; r.err != nil {
		resp.RuleErr = r.err.Error()
	} else {
		resp.Rules = r.val
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func generateMatchID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
