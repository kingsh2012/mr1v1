// Package backend 实现平台控制面HTTP API：建房、查询agent列表。
// agent状态持久化在PostgreSQL，backend通过MQTT向指定agent下发建房指令。
package backend

import (
	"bytes"
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
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mr1v1-server/internal/agentproto"
	"mr1v1-server/internal/config"
	"mr1v1-server/internal/legacy"
	"mr1v1-server/internal/model"
	"mr1v1-server/internal/resp"
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
	sessions   map[string]string // token → username
	sessionsMu sync.RWMutex
}

// New 连接MQTT broker和PostgreSQL，返回可用的Backend。
func New(cfg *config.BackendConfig) (*Backend, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		cfg.DB.Host, cfg.DB.Port, cfg.DB.User, cfg.DB.Pass, cfg.DB.DBName, cfg.DB.SSLMode, cfg.DB.Timezone)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	for _, stmt := range model.BackendStatements {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			pool.Close()
			return nil, fmt.Errorf("migrate backend tables: %w\nSQL: %s", err, stmt[:min(len(stmt), 80)])
		}
	}
	if err := seedDefaultMaps(context.Background(), pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("seed default maps: %w", err)
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
	b := &Backend{cfg: cfg, client: client, pool: pool, cancelLoop: cancel, sessions: make(map[string]string)}
	if token := client.Subscribe(agentproto.StatusSubscribeFilter, 1, b.onStatus); token.Wait() && token.Error() != nil {
		cancel()
		client.Disconnect(250)
		pool.Close()
		return nil, fmt.Errorf("subscribe %s: %w", agentproto.StatusSubscribeFilter, token.Error())
	}

	go b.timeoutLoop(ctx)

	if cfg.LegacyAPIURL != "" {
		syncer := legacy.NewSyncer(cfg.LegacyAPIURL, pool)
		go syncer.Start(ctx, time.Duration(cfg.LegacySyncIntervalMinutes)*time.Minute)
		slog.Info("legacy player sync enabled", "interval_min", cfg.LegacySyncIntervalMinutes)
	} else {
		slog.Warn("LEGACY_API_URL not set, legacy player sync disabled")
	}

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
		FROM manager_matches
		WHERE (state = 'waiting'  AND updated_at < NOW() - $1 * INTERVAL '1 second')
		   OR (state = 'playing'  AND updated_at < NOW() - $2 * INTERVAL '1 second')
		   OR (state = 'creating' AND updated_at < NOW() - $1 * INTERVAL '1 second')
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
			`UPDATE manager_matches SET state='timeout', updated_at=NOW() WHERE match_id=$1`, m.matchID)
		b.notifyWxMatchEnded(m.matchID, "timeout")
		b.writeLog(m.matchID, "platform", "timeout_destroy", map[string]any{
			"prev_state":  m.state,
			"agent_uuid":  m.agentUUID,
			"waiting_sec": waitSec,
			"playing_sec": playSec,
		})
	}
}

// Handler 返回HTTP API路由，prefix 为路由前缀（如 "/api/manager"）。
func (b *Backend) Handler(prefix string) http.Handler {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(b.authMiddleware(prefix))

	api := r.Group(prefix)
	// 比赛
	api.POST("/matches", b.handleCreateMatch)
	api.GET("/matches", b.handleListMatches)
	api.GET("/matches/:id/logs", b.handleMatchLogs)
	api.GET("/matches/:id/server", b.handleMatchServer)
	api.POST("/matches/:id/end", b.handleEndMatch)
	api.POST("/matches/:id/destroy", b.handleDestroyMatch)
	// Agent 管理
	api.GET("/agents", b.handleListAgents)
	api.PATCH("/agents/:uuid", b.handleUpdateAgent)
	api.GET("/agents/:uuid/containers", b.handleAgentContainers)
	// Rehlds 镜像配置
	api.GET("/rehlds-configs", b.handleListRehldsConfigs)
	api.POST("/rehlds-configs", b.handleCreateRehldsConfig)
	api.PATCH("/rehlds-configs/:id/activate", b.handleActivateRehldsConfig)
	// 地图池（捡枪式赛制按手枪/步枪/狙击分类，建服时按category随机选图）
	api.GET("/maps", b.handleListMaps)
	api.POST("/maps", b.handleCreateMap)
	api.PATCH("/maps/:id", b.handleUpdateMap)
	api.DELETE("/maps/:id", b.handleDeleteMap)
	// 认证（免鉴权）
	api.POST("/auth/login", b.handleLogin)
	api.POST("/auth/logout", b.handleLogout)
	// 健康检查
	api.GET("/healthz", b.handleHealthz)
	// 小程序侧数据只读查看（房间/微信用户/老玩家）
	api.GET("/wx-rooms", b.handleListWxRooms)
	api.GET("/wx-users", b.handleListWxUsers)
	api.PATCH("/wx-users/:openid", b.handleUpdateWxUser)
	api.DELETE("/wx-users/:openid", b.handleDeleteWxUser)
	api.GET("/legacy-players", b.handleListLegacyPlayers)

	// 静态文件 & SPA 路由兜底
	r.NoRoute(gin.WrapH(b.staticHandler()))

	return r
}

func (b *Backend) handleHealthz(c *gin.Context) {
	resp.OK(c, "ok")
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
		tag, _ := b.pool.Exec(context.Background(),
			`UPDATE manager_matches SET state='error', updated_at=NOW()
			 WHERE match_id=$1 AND state NOT IN ('finished','terminated','timeout','error')`,
			status.MatchID)
		if tag.RowsAffected() > 0 {
			b.notifyWxMatchEnded(status.MatchID, "error")
		}
		b.writeLog(status.MatchID, "agent", "container_stopped", status)
		return
	default:
		return
	}
	if _, err := b.pool.Exec(context.Background(),
		`UPDATE manager_matches SET state=$1, updated_at=NOW() WHERE match_id=$2`,
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
	// Category 手枪/步枪/狙击，决定从哪个地图池随机选图；为空则不指定地图(走容器默认兜底)
	Category string `json:"category,omitempty"`
	// MapName 玩家创建房间时直接指定的地图，优先级高于Category——给了就直接用，
	// 不再随机选；为空才走Category随机选图的逻辑
	MapName string `json:"map_name,omitempty"`
	// BotTestMode 仅供端到端测试使用，见 agentproto.CreateCommand.BotTestMode。
	BotTestMode bool `json:"bot_test_mode,omitempty"`
}

type createMatchResponse struct {
	MatchID  string `json:"match_id"`
	UUID     string `json:"uuid"`
	PublicIP string `json:"public_ip"`
	Port     int    `json:"port"`
}

func (b *Backend) handleCreateMatch(c *gin.Context) {
	var req createMatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, 400, "invalid json: "+err.Error())
		return
	}
	if req.P0SteamID == "" || req.P1SteamID == "" {
		resp.Fail(c, 400, "p0_steamid and p1_steamid are required")
		return
	}

	matchID, err := generateMatchID()
	if err != nil {
		resp.Fail(c, 500, "internal error")
		return
	}

	ctx := c.Request.Context()
	uuid, publicIP, port, err := b.pickAgentPort(ctx)
	if err != nil {
		slog.Error("pick agent failed", "error", err)
		resp.Fail(c, 503, "no idle agent available")
		return
	}

	image, err := b.activeRehldsImage(ctx)
	if err != nil {
		slog.Error("get rehlds image failed", "error", err)
		resp.Fail(c, 503, "rehlds config not found")
		return
	}

	serverName := req.ServerName
	if serverName == "" {
		serverName = fmt.Sprintf("mr1v1 1v1 #%s", matchID[:8])
	}

	mapName := req.MapName
	if mapName == "" {
		mapName = b.pickRandomMap(ctx, req.Category)
	}

	cmd := agentproto.CreateCommand{
		MatchID:     matchID,
		ServerName:  serverName,
		Port:        port,
		P0SteamID:   req.P0SteamID,
		P1SteamID:   req.P1SteamID,
		Image:       image,
		Map:         mapName,
		BotTestMode: req.BotTestMode,
	}
	payload, _ := json.Marshal(cmd)

	topic := agentproto.CreateTopic(uuid)
	if token := b.client.Publish(topic, 1, false, payload); token.Wait() && token.Error() != nil {
		slog.Error("publish create command failed", "error", token.Error())
		resp.Fail(c, 500, "internal error")
		return
	}

	// 写入比赛记录
	if _, err := b.pool.Exec(ctx, `
		INSERT INTO manager_matches
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
	resp.OK(c, createMatchResponse{MatchID: matchID, UUID: uuid, PublicIP: publicIP, Port: port})
}

// pickAgentPort 从PG查询一个可用agent并分配端口。
// agent可用条件：status='enabled'，心跳在stale阈值内，rehlds_run_max>0，
// rehlds_port_range格式"start-end"（为空表示agent尚未初始化完成，不参与调度）。
// 多个agent都有空闲容量时，按"当前在跑比赛数 / rehlds_run_max"负载占比从低到高选，
// 而不是固定优先用心跳最新的那个——避免出现一台agent被打满、其他agent完全空闲的不均衡情况。
func (b *Backend) pickAgentPort(ctx context.Context) (uuid, publicIP string, port int, err error) {
	stale := b.cfg.AgentStaleSeconds
	if stale <= 0 {
		stale = 30
	}

	rows, err := b.pool.Query(ctx, `
		SELECT uuid, public_ip, rehlds_port_range, rehlds_run_max
		FROM manager_agents
		WHERE status = 'enabled'
		  AND heartbeat_at > NOW() - $1 * INTERVAL '1 second'
		  AND rehlds_run_max > 0
		  AND rehlds_port_range != ''
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

	type option struct {
		uuid      string
		publicIP  string
		available []int
		busyCount int
		runMax    int
	}
	var best *option
	var bestLoad float64

	for _, c := range candidates {
		var start, end int
		if _, err := fmt.Sscanf(c.portRange, "%d-%d", &start, &end); err != nil {
			continue
		}

		// 查该agent当前活跃比赛占用的端口
		busyRows, err := b.pool.Query(ctx, `
			SELECT port FROM manager_matches
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

		load := float64(len(busy)) / float64(c.runMax)
		if best == nil || load < bestLoad {
			best = &option{uuid: c.uuid, publicIP: c.publicIP, available: available, busyCount: len(busy), runMax: c.runMax}
			bestLoad = load
		}
	}

	if best == nil {
		return "", "", 0, fmt.Errorf("all agents are at capacity")
	}
	mathrand.Shuffle(len(best.available), func(i, j int) { best.available[i], best.available[j] = best.available[j], best.available[i] })
	return best.uuid, best.publicIP, best.available[0], nil
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
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (b *Backend) handleListMatches(c *gin.Context) {
	rows, err := b.pool.Query(c.Request.Context(), `
		SELECT match_id, p0_steamid, p1_steamid, server_name,
		       agent_uuid, port, image, state, created_at, updated_at
		FROM manager_matches
		ORDER BY created_at DESC
		LIMIT 100
	`)
	if err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	defer rows.Close()

	var result []matchRow
	for rows.Next() {
		var m matchRow
		if err := rows.Scan(&m.MatchID, &m.P0SteamID, &m.P1SteamID, &m.ServerName,
			&m.AgentUUID, &m.Port, &m.Image, &m.State, &m.CreatedAt, &m.UpdatedAt); err != nil {
			continue
		}
		result = append(result, m)
	}
	if result == nil {
		result = []matchRow{}
	}
	resp.OK(c, result)
}

// handleEndMatch 优雅结束比赛：agent 先发 RCON 倒计时再 docker stop。
func (b *Backend) handleEndMatch(c *gin.Context) {
	b.dispatchDestroy(c, false)
}

// handleDestroyMatch 强制销毁：agent 直接 docker stop，不走 RCON。
func (b *Backend) handleDestroyMatch(c *gin.Context) {
	b.dispatchDestroy(c, true)
}

func (b *Backend) dispatchDestroy(c *gin.Context, force bool) {
	matchID := c.Param("id")
	if matchID == "" {
		resp.Fail(c, 400, "match_id required")
		return
	}

	var agentUUID string
	err := b.pool.QueryRow(c.Request.Context(),
		`SELECT agent_uuid FROM manager_matches WHERE match_id=$1`, matchID,
	).Scan(&agentUUID)
	if err != nil {
		resp.Fail(c, 404, "match not found")
		return
	}

	cmd := agentproto.DestroyCommand{MatchID: matchID, Force: force}
	payload, _ := json.Marshal(cmd)
	topic := agentproto.DestroyTopic(agentUUID)
	if token := b.client.Publish(topic, 1, false, payload); token.Wait() && token.Error() != nil {
		slog.Error("publish destroy command failed", "error", token.Error(), "match_id", matchID)
		resp.Fail(c, 500, "internal error")
		return
	}

	// 平台主动终止 → terminated（不参与结算），区别于正常 finished 和意外 error
	b.pool.Exec(c.Request.Context(),
		`UPDATE manager_matches SET state='terminated', updated_at=NOW() WHERE match_id=$1`, matchID)
	b.notifyWxMatchEnded(matchID, "terminated")

	action := "end_dispatched"
	if force {
		action = "destroy_dispatched"
	}
	b.writeLog(matchID, "platform", action, map[string]any{
		"agent_uuid": agentUUID,
		"command":    cmd,
	})

	slog.Info("dispatched destroy command", "match_id", matchID, "force", force, "agent", agentUUID)
	resp.OK(c, gin.H{"status": "ok"})
}

type opLogRow struct {
	ID        int64     `json:"id"`
	MatchID   string    `json:"match_id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

func (b *Backend) handleMatchLogs(c *gin.Context) {
	matchID := c.Param("id")
	rows, err := b.pool.Query(c.Request.Context(), `
		SELECT id, match_id, actor, action, detail, created_at
		FROM manager_operation_logs
		WHERE match_id = $1
		ORDER BY created_at ASC
	`, matchID)
	if err != nil {
		resp.Fail(c, 500, "db error")
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
	resp.OK(c, result)
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
		`INSERT INTO manager_operation_logs (match_id, actor, action, detail)
		 VALUES ($1, $2, $3, $4)`,
		matchID, actor, action, detailStr,
	); err != nil {
		slog.Error("write operation log failed", "error", err, "action", action)
	}
}

// notifyWxMatchEnded 比赛进入终态时同步通知 wxserver 关闭对应房间（如果是
// 通过小程序房间发起的比赛）。异步、失败只记日志，不影响主流程。
func (b *Backend) notifyWxMatchEnded(matchID, state string) {
	if b.cfg.WxBackendURL == "" {
		return
	}
	go func() {
		body, _ := json.Marshal(map[string]string{"match_id": matchID, "state": state})
		req, err := http.NewRequest(http.MethodPost, b.cfg.WxBackendURL+"/api/wx/internal/match-ended", bytes.NewReader(body))
		if err != nil {
			slog.Warn("build notify wx match-ended request failed", "match_id", matchID, "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if b.cfg.InternalAPIKey != "" {
			req.Header.Set("X-API-Key", b.cfg.InternalAPIKey)
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("notify wx match-ended failed", "match_id", matchID, "error", err)
			return
		}
		defer resp.Body.Close()
	}()
}

// activeRehldsImage 从PG查询当前生效的rehlds镜像。
func (b *Backend) activeRehldsImage(ctx context.Context) (string, error) {
	var image string
	err := b.pool.QueryRow(ctx, `
		SELECT image FROM manager_rehlds_configs
		WHERE is_active = TRUE
		ORDER BY id DESC LIMIT 1
	`).Scan(&image)
	if err != nil {
		return "", fmt.Errorf("query rehlds config: %w", err)
	}
	return image, nil
}

type agentRow struct {
	UUID         string    `json:"uuid"`
	Hostname     string    `json:"hostname"`
	PublicIP     string    `json:"public_ip"`
	LocalIP      string    `json:"local_ip"`
	CPU          string    `json:"cpu"`
	MemMB        int64     `json:"mem_mb"`
	DiskGB       int64     `json:"disk_gb"`
	Status       string    `json:"status"`
	RehldsRunMax int       `json:"rehlds_run_max"`
	PortRange    string    `json:"rehlds_port_range"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	HeartbeatAt  time.Time `json:"heartbeat_at"`
}

func (b *Backend) handleListAgents(c *gin.Context) {
	rows, err := b.pool.Query(c.Request.Context(), `
		SELECT uuid, hostname, public_ip, local_ip, cpu, mem_mb, disk_gb,
		       status, rehlds_run_max, rehlds_port_range,
		       created_at, updated_at, heartbeat_at
		FROM manager_agents
		ORDER BY heartbeat_at DESC
	`)
	if err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	defer rows.Close()

	var result []agentRow
	for rows.Next() {
		var a agentRow
		if err := rows.Scan(&a.UUID, &a.Hostname, &a.PublicIP, &a.LocalIP,
			&a.CPU, &a.MemMB, &a.DiskGB, &a.Status,
			&a.RehldsRunMax, &a.PortRange,
			&a.CreatedAt, &a.UpdatedAt, &a.HeartbeatAt); err != nil {
			continue
		}
		result = append(result, a)
	}
	if result == nil {
		result = []agentRow{}
	}
	resp.OK(c, result)
}

// handleAgentContainers 返回指定 agent 最近一次心跳上报的全量容器列表（含 ENV）。
func (b *Backend) handleAgentContainers(c *gin.Context) {
	uuid := c.Param("uuid")
	var raw []byte
	err := b.pool.QueryRow(c.Request.Context(),
		`SELECT containers_json FROM manager_agents WHERE uuid=$1`, uuid,
	).Scan(&raw)
	if err != nil {
		resp.Fail(c, 404, "agent not found")
		return
	}
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("[]")
	}
	var containers any
	if err := json.Unmarshal(raw, &containers); err != nil {
		resp.Fail(c, 500, "decode error")
		return
	}
	resp.OK(c, containers)
}

// handleUpdateAgent 更新 agent 的可配置字段（status/rehlds_run_max/rehlds_port_range）。
func (b *Backend) handleUpdateAgent(c *gin.Context) {
	uuid := c.Param("uuid")
	var req struct {
		Status       *string `json:"status"`
		RehldsRunMax *int    `json:"rehlds_run_max"`
		PortRange    *string `json:"rehlds_port_range"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, 400, "invalid json")
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
		resp.Fail(c, 400, "nothing to update")
		return
	}
	sets = append(sets, "updated_at=NOW()")
	args = append(args, uuid)

	q := fmt.Sprintf("UPDATE manager_agents SET %s WHERE uuid=$%d", strings.Join(sets, ","), n)
	if _, err := b.pool.Exec(c.Request.Context(), q, args...); err != nil {
		slog.Error("update agent failed", "error", err, "uuid", uuid)
		resp.Fail(c, 500, "db error")
		return
	}
	resp.OK(c, gin.H{"status": "ok"})
}

type rehldsConfigRow struct {
	ID        int64     `json:"id"`
	Image     string    `json:"image"`
	Version   string    `json:"version"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

func (b *Backend) handleListRehldsConfigs(c *gin.Context) {
	rows, err := b.pool.Query(c.Request.Context(),
		`SELECT id, image, version, is_active, created_at FROM manager_rehlds_configs ORDER BY id DESC`)
	if err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	defer rows.Close()

	var result []rehldsConfigRow
	for rows.Next() {
		var cfgRow rehldsConfigRow
		if err := rows.Scan(&cfgRow.ID, &cfgRow.Image, &cfgRow.Version, &cfgRow.IsActive, &cfgRow.CreatedAt); err != nil {
			continue
		}
		result = append(result, cfgRow)
	}
	if result == nil {
		result = []rehldsConfigRow{}
	}
	resp.OK(c, result)
}

func (b *Backend) handleCreateRehldsConfig(c *gin.Context) {
	var req struct {
		Image    string `json:"image"`
		Version  string `json:"version"`
		IsActive bool   `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Image == "" {
		resp.Fail(c, 400, "image is required")
		return
	}

	ctx := c.Request.Context()
	if req.IsActive {
		b.pool.Exec(ctx, `UPDATE manager_rehlds_configs SET is_active=FALSE`)
	}
	var id int64
	err := b.pool.QueryRow(ctx,
		`INSERT INTO manager_rehlds_configs (image, version, is_active) VALUES ($1,$2,$3) RETURNING id`,
		req.Image, req.Version, req.IsActive,
	).Scan(&id)
	if err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	resp.OK(c, gin.H{"id": id})
}

func (b *Backend) handleActivateRehldsConfig(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, 400, "invalid id")
		return
	}
	ctx := c.Request.Context()
	b.pool.Exec(ctx, `UPDATE manager_rehlds_configs SET is_active=FALSE`)
	if _, err := b.pool.Exec(ctx, `UPDATE manager_rehlds_configs SET is_active=TRUE WHERE id=$1`, id); err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	resp.OK(c, gin.H{"status": "ok"})
}

// ── 地图池（捡枪式赛制按手枪/步枪/狙击分类，建服时按category随机选图） ──────────

// defaultMaps 是已验证的初始地图池（实测hlds_linux启动无wad/纹理缺失警告），
// 程序启动时自动写入，避免每次重新部署都要手动调API录入。
// aim_sk_glock/aim_sk_aug_sig/aim_sk_galil_famas本机找不到地图文件，暂未收录。
var defaultMaps = []struct{ category, mapName string }{
	{"pistol", "aim_sk_usp_deagle"},
	{"pistol", "aim_usp"},
	{"rifle", "aim_sk_ak_m4"},
	{"rifle", "ak47_m4a1_dust"},
	{"rifle", "aim_map"},
	{"rifle", "aim_qpad_2007"},
	{"sniper", "aim_sk_awp"},
	{"sniper", "awp_map_32"},
}

func seedDefaultMaps(ctx context.Context, pool *pgxpool.Pool) error {
	for _, m := range defaultMaps {
		if _, err := pool.Exec(ctx, `
			INSERT INTO manager_maps (category, map_name) VALUES ($1, $2)
			ON CONFLICT (category, map_name) DO NOTHING
		`, m.category, m.mapName); err != nil {
			return err
		}
	}
	return nil
}

type mapRow struct {
	ID        int64     `json:"id"`
	Category  string    `json:"category"`
	MapName   string    `json:"map_name"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

func (b *Backend) handleListMaps(c *gin.Context) {
	query := `SELECT id, category, map_name, enabled, created_at FROM manager_maps`
	var rows pgx.Rows
	var err error
	if category := c.Query("category"); category != "" {
		rows, err = b.pool.Query(c.Request.Context(), query+` WHERE category=$1 ORDER BY category, map_name`, category)
	} else {
		rows, err = b.pool.Query(c.Request.Context(), query+` ORDER BY category, map_name`)
	}
	if err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	defer rows.Close()

	var result []mapRow
	for rows.Next() {
		var m mapRow
		if err := rows.Scan(&m.ID, &m.Category, &m.MapName, &m.Enabled, &m.CreatedAt); err != nil {
			continue
		}
		result = append(result, m)
	}
	if result == nil {
		result = []mapRow{}
	}
	resp.OK(c, result)
}

func (b *Backend) handleCreateMap(c *gin.Context) {
	var req struct {
		Category string `json:"category"`
		MapName  string `json:"map_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Category == "" || req.MapName == "" {
		resp.Fail(c, 400, "category and map_name are required")
		return
	}

	var id int64
	err := b.pool.QueryRow(c.Request.Context(),
		`INSERT INTO manager_maps (category, map_name) VALUES ($1,$2)
		 ON CONFLICT (category, map_name) DO UPDATE SET enabled=TRUE RETURNING id`,
		req.Category, req.MapName,
	).Scan(&id)
	if err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	resp.OK(c, gin.H{"id": id})
}

func (b *Backend) handleUpdateMap(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, 400, "invalid id")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		resp.Fail(c, 400, "enabled is required")
		return
	}
	if _, err := b.pool.Exec(c.Request.Context(),
		`UPDATE manager_maps SET enabled=$2 WHERE id=$1`, id, *req.Enabled); err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	resp.OK(c, gin.H{"status": "ok"})
}

func (b *Backend) handleDeleteMap(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(c, 400, "invalid id")
		return
	}
	if _, err := b.pool.Exec(c.Request.Context(), `DELETE FROM manager_maps WHERE id=$1`, id); err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	resp.OK(c, gin.H{"status": "ok"})
}

// pickRandomMap 从指定分类(手枪/步枪/狙击)的启用地图里随机选一张。
// category为空或该分类下没有启用的地图时返回空字符串，调用方按start.sh的默认兜底处理，
// 不当作错误中断建服流程。
func (b *Backend) pickRandomMap(ctx context.Context, category string) string {
	if category == "" {
		return ""
	}
	var mapName string
	err := b.pool.QueryRow(ctx,
		`SELECT map_name FROM manager_maps WHERE category=$1 AND enabled=TRUE ORDER BY random() LIMIT 1`,
		category,
	).Scan(&mapName)
	if err != nil {
		return ""
	}
	return mapName
}

// ── 小程序侧数据只读查看（房间/微信用户/老玩家） ──────────────────────────────

type wxRoomRow struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	CreatorOpenID string     `json:"creator_openid"`
	CreatorName   string     `json:"creator_name"`
	JoinerOpenID  string     `json:"joiner_openid"`
	JoinerName    string     `json:"joiner_name"`
	Locked        bool       `json:"locked"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

func (b *Backend) handleListWxRooms(c *gin.Context) {
	rows, err := b.pool.Query(c.Request.Context(), `
		SELECT r.id, r.title, r.creator_openid, COALESCE(u.nickname,''),
		       COALESCE(r.joiner_openid,''), COALESCE(j.nickname,''),
		       r.password != '' AS locked, r.status, r.created_at, r.deleted_at
		FROM wx_rooms r
		JOIN wx_users u ON u.openid = r.creator_openid
		LEFT JOIN wx_users j ON j.openid = r.joiner_openid
		ORDER BY r.created_at DESC
		LIMIT 200
	`)
	if err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	defer rows.Close()

	var result []wxRoomRow
	for rows.Next() {
		var rm wxRoomRow
		if err := rows.Scan(&rm.ID, &rm.Title, &rm.CreatorOpenID, &rm.CreatorName,
			&rm.JoinerOpenID, &rm.JoinerName, &rm.Locked, &rm.Status,
			&rm.CreatedAt, &rm.DeletedAt); err != nil {
			continue
		}
		result = append(result, rm)
	}
	if result == nil {
		result = []wxRoomRow{}
	}
	resp.OK(c, result)
}

type wxUserRow struct {
	OpenID    string    `json:"openid"`
	SteamID   string    `json:"steam_id"`
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatar_url"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (b *Backend) handleListWxUsers(c *gin.Context) {
	rows, err := b.pool.Query(c.Request.Context(), `
		SELECT openid, steam_id, nickname, avatar_url, status, created_at, updated_at
		FROM wx_users
		ORDER BY created_at DESC
		LIMIT 200
	`)
	if err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	defer rows.Close()

	var result []wxUserRow
	for rows.Next() {
		var u wxUserRow
		if err := rows.Scan(&u.OpenID, &u.SteamID, &u.Nickname, &u.AvatarURL, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
			continue
		}
		result = append(result, u)
	}
	if result == nil {
		result = []wxUserRow{}
	}
	resp.OK(c, result)
}

// handleUpdateWxUser 编辑微信用户：昵称/头像/SteamID/启用状态，都是可选字段，
// 传了哪个就改哪个。status传"disabled"即视为拉黑——该用户已签发的session立刻失效
// (ResolveSession会查到disabled直接拒绝)，新登录也会在Login里被卡住。
func (b *Backend) handleUpdateWxUser(c *gin.Context) {
	openid := c.Param("openid")
	var req struct {
		Status    *string `json:"status"`
		SteamID   *string `json:"steam_id"`
		Nickname  *string `json:"nickname"`
		AvatarURL *string `json:"avatar_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, 400, "invalid json")
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
	if req.SteamID != nil {
		sets = append(sets, fmt.Sprintf("steam_id=$%d", n))
		args = append(args, *req.SteamID)
		n++
	}
	if req.Nickname != nil {
		sets = append(sets, fmt.Sprintf("nickname=$%d", n))
		args = append(args, *req.Nickname)
		n++
	}
	if req.AvatarURL != nil {
		sets = append(sets, fmt.Sprintf("avatar_url=$%d", n))
		args = append(args, *req.AvatarURL)
		n++
	}
	if len(sets) == 0 {
		resp.Fail(c, 400, "nothing to update")
		return
	}
	sets = append(sets, "updated_at=NOW()")
	args = append(args, openid)

	q := fmt.Sprintf("UPDATE wx_users SET %s WHERE openid=$%d", strings.Join(sets, ","), n)
	tag, err := b.pool.Exec(c.Request.Context(), q, args...)
	if err != nil {
		slog.Error("update wx_user failed", "error", err, "openid", openid)
		resp.Fail(c, 500, "db error")
		return
	}
	if tag.RowsAffected() == 0 {
		resp.Fail(c, 404, "user not found")
		return
	}
	resp.OK(c, gin.H{"status": "ok"})
}

// handleDeleteWxUser 删除一个微信用户。wx_rooms.creator_openid是NOT NULL外键，
// 该用户创建过的房间必须先删掉（不能像joiner_openid那样置空）；
// wx_sessions有ON DELETE CASCADE，不用手动处理。
func (b *Backend) handleDeleteWxUser(c *gin.Context) {
	openid := c.Param("openid")
	ctx := c.Request.Context()

	tx, err := b.pool.Begin(ctx)
	if err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM wx_rooms WHERE creator_openid = $1`, openid); err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	if _, err := tx.Exec(ctx, `UPDATE wx_rooms SET joiner_openid = NULL WHERE joiner_openid = $1`, openid); err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	tag, err := tx.Exec(ctx, `DELETE FROM wx_users WHERE openid = $1`, openid)
	if err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	if tag.RowsAffected() == 0 {
		resp.Fail(c, 404, "user not found")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		resp.Fail(c, 500, "db error")
		return
	}

	resp.OK(c, gin.H{"ok": "1"})
}

type legacyPlayerRow struct {
	SteamID       string  `json:"steam_id"`
	Name          string  `json:"name"`
	Score         float64 `json:"score"`
	TotalMatch    int     `json:"total_match"`
	TotalMatchWin int     `json:"total_match_win"`
	KD            float64 `json:"kd"`
	WinRate       float64 `json:"win_rate"`
	Rating        float64 `json:"rating"`
}

func (b *Backend) handleListLegacyPlayers(c *gin.Context) {
	keyword := c.Query("keyword")
	var rows pgx.Rows
	var err error
	if keyword != "" {
		rows, err = b.pool.Query(c.Request.Context(), `
			SELECT steam_id, name, score, total_match, total_match_win, kd, win_rate, rating
			FROM legacy_players
			WHERE name ILIKE '%' || $1 || '%'
			ORDER BY score DESC
			LIMIT 200
		`, keyword)
	} else {
		rows, err = b.pool.Query(c.Request.Context(), `
			SELECT steam_id, name, score, total_match, total_match_win, kd, win_rate, rating
			FROM legacy_players
			ORDER BY score DESC
			LIMIT 200
		`)
	}
	if err != nil {
		resp.Fail(c, 500, "db error")
		return
	}
	defer rows.Close()

	var result []legacyPlayerRow
	for rows.Next() {
		var p legacyPlayerRow
		if err := rows.Scan(&p.SteamID, &p.Name, &p.Score, &p.TotalMatch, &p.TotalMatchWin, &p.KD, &p.WinRate, &p.Rating); err != nil {
			continue
		}
		result = append(result, p)
	}
	if result == nil {
		result = []legacyPlayerRow{}
	}
	resp.OK(c, result)
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
	Info    *a2s.ServerInfo `json:"info"`
	Players *a2s.PlayerInfo `json:"players"`
	Rules   *a2s.RulesInfo  `json:"rules"`
	InfoErr string          `json:"info_error,omitempty"`
	PlrErr  string          `json:"players_error,omitempty"`
	RuleErr string          `json:"rules_error,omitempty"`
}

func (b *Backend) handleMatchServer(c *gin.Context) {
	matchID := c.Param("id")
	var publicIP string
	var port int
	err := b.pool.QueryRow(c.Request.Context(), `
		SELECT a.public_ip, m.port
		FROM manager_matches m
		JOIN manager_agents a ON a.uuid = m.agent_uuid
		WHERE m.match_id = $1
	`, matchID).Scan(&publicIP, &port)
	if err != nil {
		resp.Fail(c, 404, "match not found")
		return
	}

	addr := fmt.Sprintf("%s:%d", publicIP, port)
	out := serverQueryResponse{}

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
		out.InfoErr = r.err.Error()
	} else {
		out.Info = r.val
	}
	if r := <-plrCh; r.err != nil {
		out.PlrErr = r.err.Error()
	} else {
		out.Players = r.val
	}
	if r := <-ruleCh; r.err != nil {
		out.RuleErr = r.err.Error()
	} else {
		out.Rules = r.val
	}

	resp.OK(c, out)
}

func generateMatchID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ── Auth ──────────────────────────────────────────────────────────────────────

const sessionCookie = "mr1v1_session"

// authMiddleware 验证 Cookie session 或 X-API-Key header；ADMIN_PASS 未配置时直接放行。
func (b *Backend) authMiddleware(apiPrefix string) gin.HandlerFunc {
	if b.cfg.AdminPass == "" {
		slog.Warn("ADMIN_PASS not set, manager UI auth disabled")
		return func(c *gin.Context) {}
	}
	exempt := map[string]bool{
		apiPrefix + "/auth/login":  true,
		apiPrefix + "/auth/logout": true,
		apiPrefix + "/healthz":     true,
	}
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if exempt[path] {
			return
		}
		// 内部服务调用：X-API-Key 校验
		if b.cfg.InternalAPIKey != "" && c.GetHeader("X-API-Key") == b.cfg.InternalAPIKey {
			return
		}
		// 浏览器会话：Cookie 校验
		if cookie, err := c.Cookie(sessionCookie); err == nil {
			b.sessionsMu.RLock()
			_, ok := b.sessions[cookie]
			b.sessionsMu.RUnlock()
			if ok {
				return
			}
		}
		if strings.HasPrefix(path, "/api/") {
			resp.Fail(c, 401, "unauthorized")
			return
		}
		// 静态资源直接放行（JS/CSS/图标等，浏览器加载登录页时需要）
		if strings.HasPrefix(path, "/assets/") || strings.ContainsRune(path[1:], '.') {
			return
		}
		// SPA 页面请求：返回 index.html，React 负责跳转 /login
		c.Request.URL.Path = "/"
	}
}

func (b *Backend) handleLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, 400, "bad request")
		return
	}
	if req.Username != b.cfg.AdminUser || req.Password != b.cfg.AdminPass {
		resp.Fail(c, 401, "invalid credentials")
		return
	}
	tokenBytes := make([]byte, 16)
	rand.Read(tokenBytes) //nolint:errcheck
	token := hex.EncodeToString(tokenBytes)

	b.sessionsMu.Lock()
	b.sessions[token] = req.Username
	b.sessionsMu.Unlock()

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookie, token, 86400*7, "/", "", false, true)
	resp.OK(c, gin.H{"status": "ok"})
}

func (b *Backend) handleLogout(c *gin.Context) {
	if cookie, err := c.Cookie(sessionCookie); err == nil {
		b.sessionsMu.Lock()
		delete(b.sessions, cookie)
		b.sessionsMu.Unlock()
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookie, "", -1, "/", "", false, true)
	resp.OK(c, gin.H{"status": "ok"})
}
