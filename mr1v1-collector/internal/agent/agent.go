// Package agent merges the telemetry gateway with the per-host control
// plane: heartbeat reporting and create-command handling. See
// AGENT_ARCHITECTURE_DESIGN.md for the overall design.
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"mr1v1-collector/internal/agentproto"
	"mr1v1-collector/internal/config"
	"mr1v1-collector/internal/dockerctl"
	"mr1v1-collector/internal/envelope"
	"mr1v1-collector/internal/gateway"
	"mr1v1-collector/internal/rcon"
)

const (
	defaultHeartbeatInterval         = 10 * time.Second
	defaultStopTimeout               = 15 * time.Second
	defaultDestroyCommand            = "mr1v1_match_destroy"
	defaultDestroyCountdownAfterRCON = 5 * time.Second
)

// matchState tracks the per-match state an agent needs after creating a
// container, so it can be torn down again on mr1v1_match_end.
type matchState struct {
	port         int
	rconPassword string
}

// Agent runs the telemetry gateway (HTTP /record -> mr1v1/{match_id}) and
// the control-plane connection (heartbeat + create-command subscription) on
// a single MQTT broker connection per role.
type Agent struct {
	cfg    *config.AgentConfig
	gw     *gateway.Server
	client mqtt.Client
	docker *dockerctl.Client
	hostID string

	mu      sync.Mutex
	matches map[string]matchState // match_id -> {port, rcon_password}
}

// New creates an Agent: connects the telemetry gateway, opens a separate
// control-plane MQTT connection, subscribes to this host's create-command
// topic, and starts the heartbeat loop.
func New(cfg *config.AgentConfig) (*Agent, error) {
	hostID, err := loadOrCreateHostID(cfg.HostID.File)
	if err != nil {
		return nil, err
	}

	docker, err := dockerctl.New()
	if err != nil {
		return nil, err
	}

	// a is captured by the gateway's onEnvelope hook below. It is only
	// invoked from HTTP request handlers, which cannot fire until New
	// returns and a has been assigned, so the nil window is safe.
	var a *Agent
	gwMQTT := cfg.MQTT
	gwMQTT.ClientID = "mr1v1-agent-" + hostID
	gw, err := gateway.NewWithHook(&config.GatewayConfig{
		HTTP:  cfg.HTTP,
		MQTT:  gwMQTT,
		Queue: struct{ Capacity int }{Capacity: cfg.Queue.Capacity},
	}, func(env envelope.Envelope) {
		a.onEnvelope(env)
	})
	if err != nil {
		docker.Close()
		return nil, fmt.Errorf("init telemetry gateway: %w", err)
	}

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.MQTT.Broker).
		SetClientID("mr1v1-agent-" + hostID + "-ctl").
		SetAutoReconnect(true).
		SetConnectTimeout(10 * time.Second).
		SetKeepAlive(30 * time.Second)
	if cfg.MQTT.User != "" {
		opts.SetUsername(cfg.MQTT.User)
		opts.SetPassword(cfg.MQTT.Pass)
	}

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		gw.Close()
		docker.Close()
		return nil, fmt.Errorf("connect control-plane mqtt broker %s: %w", cfg.MQTT.Broker, token.Error())
	}

	a = &Agent{
		cfg:     cfg,
		gw:      gw,
		client:  client,
		docker:  docker,
		hostID:  hostID,
		matches: make(map[string]matchState),
	}

	createTopic := agentproto.CreateTopic(hostID)
	if token := client.Subscribe(createTopic, 1, a.onCreate); token.Wait() && token.Error() != nil {
		a.Close()
		return nil, fmt.Errorf("subscribe %s: %w", createTopic, token.Error())
	}

	go a.heartbeatLoop()

	slog.Info("agent started", "host_id", hostID, "create_topic", createTopic)
	return a, nil
}

// Handler returns the telemetry gateway's HTTP handler (POST /record, GET
// /healthz).
func (a *Agent) Handler() http.Handler {
	return a.gw.Handler()
}

// Close disconnects both MQTT connections and the Docker client.
func (a *Agent) Close() {
	a.client.Disconnect(250)
	a.gw.Close()
	a.docker.Close()
}

// onEnvelope is the gateway hook: it reacts to mr1v1_match_end telemetry by
// tearing down that match's container (docker stop + rm, port release).
func (a *Agent) onEnvelope(env envelope.Envelope) {
	if env.Type != envelope.TypeMatchEnd {
		return
	}
	go a.teardownMatch(env.MatchID)
}

func (a *Agent) onCreate(_ mqtt.Client, msg mqtt.Message) {
	var cmd agentproto.CreateCommand
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		slog.Error("decode create command failed", "error", err, "payload", string(msg.Payload()))
		return
	}

	slog.Info("received create command", "match_id", cmd.MatchID, "port", cmd.Port,
		"server_name", cmd.ServerName, "p0", cmd.P0SteamID, "p1", cmd.P1SteamID, "image", cmd.Image)

	go a.createMatch(cmd)
}

func (a *Agent) createMatch(cmd agentproto.CreateCommand) {
	image := cmd.Image
	if image == "" {
		image = a.cfg.Docker.DefaultImage
	}
	if image == "" {
		slog.Error("create command has no image and no default_image configured", "match_id", cmd.MatchID)
		a.publishStatus(agentproto.StatusReport{
			MatchID: cmd.MatchID, HostID: a.hostID, Port: cmd.Port,
			State: agentproto.StateError, Message: "no image specified and no default_image configured",
			Timestamp: time.Now().Unix(),
		})
		return
	}

	rconPassword, err := randomRCONPassword()
	if err != nil {
		slog.Error("generate rcon password failed", "error", err, "match_id", cmd.MatchID)
		a.publishStatus(agentproto.StatusReport{
			MatchID: cmd.MatchID, HostID: a.hostID, Port: cmd.Port,
			State: agentproto.StateError, Message: err.Error(), Timestamp: time.Now().Unix(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = a.docker.CreateAndStart(ctx, dockerctl.Spec{
		MatchID:      cmd.MatchID,
		ServerName:   cmd.ServerName,
		Port:         cmd.Port,
		P0SteamID:    cmd.P0SteamID,
		P1SteamID:    cmd.P1SteamID,
		Image:        image,
		GatewayHTTP:  a.gatewayHTTPAddr(),
		RCONPassword: rconPassword,
	})
	if err != nil {
		slog.Error("create match container failed", "error", err, "match_id", cmd.MatchID)
		a.publishStatus(agentproto.StatusReport{
			MatchID: cmd.MatchID, HostID: a.hostID, Port: cmd.Port,
			State: agentproto.StateError, Message: err.Error(), Timestamp: time.Now().Unix(),
		})
		return
	}

	a.trackMatch(cmd.MatchID, matchState{port: cmd.Port, rconPassword: rconPassword})
	slog.Info("match container running", "match_id", cmd.MatchID, "port", cmd.Port, "image", image)
	a.publishStatus(agentproto.StatusReport{
		MatchID: cmd.MatchID, HostID: a.hostID, Port: cmd.Port,
		State: agentproto.StateRunning, Timestamp: time.Now().Unix(),
	})
}

func (a *Agent) teardownMatch(matchID string) {
	timeout := time.Duration(a.cfg.Docker.StopTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultStopTimeout
	}

	state, ok := a.matchByID(matchID)
	if ok && state.rconPassword != "" {
		a.triggerDestroyCountdown(matchID, state)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout+10*time.Second)
	defer cancel()

	if err := a.docker.StopAndRemoveByMatchID(ctx, matchID, timeout); err != nil {
		slog.Error("teardown match container failed", "error", err, "match_id", matchID)
		a.publishStatus(agentproto.StatusReport{
			MatchID: matchID, HostID: a.hostID,
			State: agentproto.StateError, Message: err.Error(), Timestamp: time.Now().Unix(),
		})
		return
	}

	port := a.freePortForMatch(matchID)
	slog.Info("match container torn down", "match_id", matchID, "port", port)
	a.publishStatus(agentproto.StatusReport{
		MatchID: matchID, HostID: a.hostID, Port: port,
		State: agentproto.StateStopped, Timestamp: time.Now().Unix(),
	})
}

// gatewayHTTPAddr returns the agent's own /record endpoint as seen from a
// network_mode:host container (127.0.0.1:<agent listen port>).
func (a *Agent) gatewayHTTPAddr() string {
	_, port, err := net.SplitHostPort(a.cfg.HTTP.Listen)
	if err != nil || port == "" {
		port = "7778"
	}
	return fmt.Sprintf("http://127.0.0.1:%s/record", port)
}

// triggerDestroyCountdown RCONs into the match's container to run the
// configured destroy command (default mr1v1_match_destroy), which is
// expected to broadcast a countdown and kick the players (see
// AGENT_ARCHITECTURE_DESIGN.md section 6). It waits briefly afterwards so
// the broadcast/kick can complete before the container is stopped. Errors
// are logged but do not block teardown - the agent must free the port
// regardless.
func (a *Agent) triggerDestroyCountdown(matchID string, state matchState) {
	command := a.cfg.Docker.DestroyCommand
	if command == "" {
		command = defaultDestroyCommand
	}

	addr := fmt.Sprintf("127.0.0.1:%d", state.port)
	if _, err := rcon.New(addr, state.rconPassword).Execute(command); err != nil {
		slog.Error("rcon destroy command failed", "error", err, "match_id", matchID, "addr", addr)
		return
	}

	countdown := time.Duration(a.cfg.Docker.DestroyCountdownSeconds) * time.Second
	if countdown <= 0 {
		countdown = defaultDestroyCountdownAfterRCON
	}
	time.Sleep(countdown)
}

func (a *Agent) trackMatch(matchID string, state matchState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.matches[matchID] = state
}

func (a *Agent) matchByID(matchID string) (matchState, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.matches[matchID]
	return state, ok
}

// freePortForMatch removes the tracked state for matchID and returns its
// port, or 0 if none was tracked (e.g. agent restarted after the container
// was created).
func (a *Agent) freePortForMatch(matchID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.matches[matchID]
	if !ok {
		return 0
	}
	delete(a.matches, matchID)
	return state.port
}

func (a *Agent) busyPortsList() []int {
	a.mu.Lock()
	defer a.mu.Unlock()
	ports := make([]int, 0, len(a.matches))
	for _, state := range a.matches {
		ports = append(ports, state.port)
	}
	return ports
}

func randomRCONPassword() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate rcon password: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func (a *Agent) publishStatus(status agentproto.StatusReport) {
	payload, err := json.Marshal(status)
	if err != nil {
		slog.Error("marshal status report failed", "error", err, "match_id", status.MatchID)
		return
	}
	topic := agentproto.StatusTopic(a.hostID)
	if token := a.client.Publish(topic, 1, false, payload); token.Wait() && token.Error() != nil {
		slog.Error("publish status report failed", "error", token.Error(), "topic", topic)
	}
}

func (a *Agent) heartbeatLoop() {
	interval := time.Duration(a.cfg.Heartbeat.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}

	privateIP := a.cfg.Heartbeat.PrivateIP
	if privateIP == "" {
		privateIP = detectPrivateIP()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		hb := agentproto.Heartbeat{
			HostID:         a.hostID,
			PublicIP:       a.cfg.Heartbeat.PublicIP,
			PrivateIP:      privateIP,
			PortRangeStart: a.cfg.Heartbeat.PortRangeStart,
			PortRangeEnd:   a.cfg.Heartbeat.PortRangeEnd,
			BusyPorts:      a.busyPortsList(),
			Timestamp:      time.Now().Unix(),
		}
		payload, err := json.Marshal(hb)
		if err != nil {
			slog.Error("marshal heartbeat failed", "error", err)
		} else {
			topic := agentproto.HeartbeatTopic(a.hostID)
			if token := a.client.Publish(topic, 0, false, payload); token.Wait() && token.Error() != nil {
				slog.Error("publish heartbeat failed", "error", token.Error(), "topic", topic)
			}
		}
		<-ticker.C
	}
}
