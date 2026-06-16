// Package agentproto defines the control-plane MQTT message types shared
// between the per-host agent and the platform backend.
//
// Topic layout:
//
//	mr1v1-agent/{uuid}/heartbeat  agent -> consumer (写DB)
//	mr1v1-agent/{uuid}/create     backend -> agent
//	mr1v1-agent/{uuid}/status     agent -> backend (比赛状态回报)
package agentproto

import "fmt"

const TopicPrefix = "mr1v1-agent"

const (
	StateRunning = "running"
	StateStopped = "stopped"
	StateError   = "error"
)

// Heartbeat 由 agent 定期上报，consumer 将其 upsert 到 mr1v1_agent 表。
type Heartbeat struct {
	UUID      string `json:"uuid"`
	Hostname  string `json:"hostname"`
	PublicIP  string `json:"public_ip"`
	LocalIP   string `json:"local_ip"`
	CPU       string `json:"cpu"`
	MemMB     int64  `json:"mem_mb"`
	DiskGB    int64  `json:"disk_gb"`
	Timestamp int64  `json:"ts"`
}

// CreateCommand 由 backend 下发给指定 agent，指示其拉起一个 rehlds 容器。
type CreateCommand struct {
	MatchID    string `json:"match_id"`
	ServerName string `json:"server_name"`
	Port       int    `json:"port"`
	P0SteamID  string `json:"p0_steamid"`
	P1SteamID  string `json:"p1_steamid"`
	Image      string `json:"image"`
}

// StatusReport 由 agent 在建房结果/容器销毁后上报。
type StatusReport struct {
	MatchID   string `json:"match_id"`
	UUID      string `json:"uuid"`
	Port      int    `json:"port"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"ts"`
}

func HeartbeatTopic(uuid string) string {
	return fmt.Sprintf("%s/%s/heartbeat", TopicPrefix, uuid)
}

func CreateTopic(uuid string) string {
	return fmt.Sprintf("%s/%s/create", TopicPrefix, uuid)
}

func StatusTopic(uuid string) string {
	return fmt.Sprintf("%s/%s/status", TopicPrefix, uuid)
}

const HeartbeatSubscribeFilter = TopicPrefix + "/+/heartbeat"
const StatusSubscribeFilter = TopicPrefix + "/+/status"
