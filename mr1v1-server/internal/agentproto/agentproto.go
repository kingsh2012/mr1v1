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

// ContainerDetail mirrors dockerctl.ContainerDetail for the MQTT payload.
// Duplicated here to keep agentproto import-free of internal packages.
type ContainerDetail struct {
	ID      string            `json:"id"`
	Image   string            `json:"image"`
	Command string            `json:"command"`
	Created int64             `json:"created"`
	Status  string            `json:"status"`
	Names   []string          `json:"names"`
	Env     []string          `json:"env"`
	Labels  map[string]string `json:"labels"`
}

// Heartbeat 由 agent 定期上报，consumer 将其 upsert 到 mr1v1_agent 表。
// CPU 字段为 CPU 逻辑核心数（字符串形式，如 "4"），consumer 用它初始化 rehlds_run_max 默认值。
// RunningMatches 为当前正在运行的 rehlds 容器的 match_id 列表。
// Containers 为主机上全量容器的详细信息（含 ENV），consumer 存为 containers_json。
type Heartbeat struct {
	UUID            string            `json:"uuid"`
	Hostname        string            `json:"hostname"`
	PublicIP        string            `json:"public_ip"`
	LocalIP         string            `json:"local_ip"`
	CPU             string            `json:"cpu"`
	MemMB           int64             `json:"mem_mb"`
	DiskGB          int64             `json:"disk_gb"`
	RunningMatches  []string          `json:"running_matches"`
	Containers      []ContainerDetail `json:"containers"`
	Timestamp       int64             `json:"ts"`
}

// CreateCommand 由 backend 下发给指定 agent，指示其拉起一个 rehlds 容器。
type CreateCommand struct {
	MatchID    string `json:"match_id"`
	ServerName string `json:"server_name"`
	Port       int    `json:"port"`
	P0SteamID  string `json:"p0_steamid"`
	P1SteamID  string `json:"p1_steamid"`
	Image      string `json:"image"`
	// Map 由backend按比赛类型(手枪/步枪/狙击)从地图池随机选定，留空则容器内start.sh
	// 用默认地图兜底
	Map string `json:"map,omitempty"`
	// BotTestMode 仅供端到端测试使用：为true时双方slot由2个Bot顶替，
	// 无需真实玩家连入即可走完一整局比赛。正式排位赛不设置此字段。
	BotTestMode bool `json:"bot_test_mode,omitempty"`
}

// DestroyCommand 由 backend 下发给指定 agent，指示其销毁一个 rehlds 容器。
// Force=false：先发 RCON 倒计时再 docker stop（优雅结束）。
// Force=true：直接 docker stop，不走 RCON。
type DestroyCommand struct {
	MatchID string `json:"match_id"`
	Force   bool   `json:"force"`
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

func DestroyTopic(uuid string) string {
	return fmt.Sprintf("%s/%s/destroy", TopicPrefix, uuid)
}

func StatusTopic(uuid string) string {
	return fmt.Sprintf("%s/%s/status", TopicPrefix, uuid)
}

const HeartbeatSubscribeFilter = TopicPrefix + "/+/heartbeat"
const StatusSubscribeFilter = TopicPrefix + "/+/status"
const DestroySubscribeFilter = TopicPrefix + "/+/destroy"
