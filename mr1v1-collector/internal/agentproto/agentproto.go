// Package agentproto defines the control-plane MQTT message types shared
// between the per-host agent and the platform backend.
//
// Topic layout (see AGENT_ARCHITECTURE_DESIGN.md section 4):
//
//	mr1v1-agent/{host_id}/heartbeat  agent -> backend
//	mr1v1-agent/{host_id}/create     backend -> agent
//	mr1v1-agent/{host_id}/status     agent -> backend
package agentproto

import "fmt"

// TopicPrefix is the MQTT topic prefix for all agent control-plane messages.
// It is intentionally separate from the telemetry prefix (mr1v1/{match_id}).
const TopicPrefix = "mr1v1-agent"

// State values reported in StatusReport.State.
const (
	StateRunning = "running"
	StateStopped = "stopped"
	StateError   = "error"
)

// Heartbeat is published periodically by an agent to report its identity,
// network addresses, and rehlds port allocation.
type Heartbeat struct {
	HostID         string `json:"host_id"`
	PublicIP       string `json:"public_ip"`
	PrivateIP      string `json:"private_ip"`
	PortRangeStart int    `json:"port_range_start"`
	PortRangeEnd   int    `json:"port_range_end"`
	BusyPorts      []int  `json:"busy_ports"`
	Timestamp      int64  `json:"ts"`
}

// CreateCommand is published by the platform backend to ask an agent to
// stand up a new rehlds container for a match.
type CreateCommand struct {
	MatchID    string `json:"match_id"`
	ServerName string `json:"server_name"`
	Port       int    `json:"port"`
	P0SteamID  string `json:"p0_steamid"`
	P1SteamID  string `json:"p1_steamid"`
	Image      string `json:"image"`
}

// StatusReport is published by an agent in response to a CreateCommand, and
// again when the container is torn down at the end of a match.
type StatusReport struct {
	MatchID   string `json:"match_id"`
	HostID    string `json:"host_id"`
	Port      int    `json:"port"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"ts"`
}

// HeartbeatTopic returns the topic an agent publishes its heartbeat to.
func HeartbeatTopic(hostID string) string {
	return fmt.Sprintf("%s/%s/heartbeat", TopicPrefix, hostID)
}

// CreateTopic returns the topic the backend publishes create commands to for
// a given agent.
func CreateTopic(hostID string) string {
	return fmt.Sprintf("%s/%s/create", TopicPrefix, hostID)
}

// StatusTopic returns the topic an agent publishes status reports to.
func StatusTopic(hostID string) string {
	return fmt.Sprintf("%s/%s/status", TopicPrefix, hostID)
}

// HeartbeatSubscribeFilter is the wildcard topic the backend subscribes to
// in order to receive heartbeats from all agents.
const HeartbeatSubscribeFilter = TopicPrefix + "/+/heartbeat"

// StatusSubscribeFilter is the wildcard topic the backend subscribes to in
// order to receive status reports from all agents.
const StatusSubscribeFilter = TopicPrefix + "/+/status"
