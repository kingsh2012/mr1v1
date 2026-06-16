// Package envelope 定义 AMXX 插件上报的统一消息信封以及各事件类型的data结构，
// 字段与 /data/rehlds/MR1V1_EVENTS.md 一一对应。
package envelope

import "encoding/json"

// Envelope 是 POST /record 的请求体结构。Data 保持原始JSON，
// 由gateway整体转发到MQTT，具体字段解析在consumer侧按Type进行。
type Envelope struct {
	Timestamp int64           `json:"timestamp"`
	MatchID   string          `json:"match_id"`
	Type      string          `json:"type"`
	Version   string          `json:"version"`
	Data      json.RawMessage `json:"data"`
}

// 事件类型常量，对应 MR1V1_EVENTS.md 中的 type 字段。
const (
	TypeMatchStart    = "mr1v1_match_start"
	TypeRoundEnd      = "mr1v1_round_end"
	TypeMatchEnd      = "mr1v1_match_end"
	TypeCombatBatch   = "mr1v1_combat_batch"
	TypeShootBatch    = "mr1v1_shoot_batch"
	TypePositionBatch = "mr1v1_position_batch"
)

// KnownTypes 列出当前已知的事件类型，便于gateway做存在性校验（未知类型仅记录warning，仍转发）。
var KnownTypes = map[string]bool{
	TypeMatchStart:    true,
	TypeRoundEnd:      true,
	TypeMatchEnd:      true,
	TypeCombatBatch:   true,
	TypeShootBatch:    true,
	TypePositionBatch: true,
}

// MatchStart 对应 mr1v1_match_start 的 data 字段。
type MatchStart struct {
	MatchID  string `json:"match_id"`
	Map      string `json:"map"`
	P0Name   string `json:"p0.name"`
	P0AuthID string `json:"p0.authid"`
	P0UserID int    `json:"p0.userid"`
	P1Name   string `json:"p1.name"`
	P1AuthID string `json:"p1.authid"`
	P1UserID int    `json:"p1.userid"`
}

// RoundEnd 对应 mr1v1_round_end 的 data 字段。
type RoundEnd struct {
	MatchID    string `json:"match_id"`
	Round      int    `json:"round"`
	Phase      int    `json:"phase"`
	WinnerSlot int    `json:"winner_slot"`
	Wins0      int    `json:"wins0"`
	Wins1      int    `json:"wins1"`
	P0Damage   int    `json:"p0.damage"`
	P0Hits     int    `json:"p0.hits"`
	P1Damage   int    `json:"p1.damage"`
	P1Hits     int    `json:"p1.hits"`
}

// MatchEnd 对应 mr1v1_match_end 的 data 字段。
type MatchEnd struct {
	MatchID    string `json:"match_id"`
	EndReason  string `json:"end_reason"`
	WinnerSlot int    `json:"winner_slot"`
	Wins0      int    `json:"wins0"`
	Wins1      int    `json:"wins1"`
	P0Name     string `json:"p0.name"`
	P0AuthID   string `json:"p0.authid"`
	P1Name     string `json:"p1.name"`
	P1AuthID   string `json:"p1.authid"`
}

// CombatEvent 是 mr1v1_combat_batch 的 events[] 元素。
type CombatEvent struct {
	Ts           int64  `json:"ts"`
	AttackerSlot int    `json:"attacker_slot"`
	VictimSlot   int    `json:"victim_slot"`
	Weapon       string `json:"weapon"`
	Damage       int    `json:"damage"`
	Hitgroup     int    `json:"hitgroup"`
}

// CombatBatch 对应 mr1v1_combat_batch 的 data 字段。
type CombatBatch struct {
	MatchID string        `json:"match_id"`
	Events  []CombatEvent `json:"events"`
}

// ShootEvent 是 mr1v1_shoot_batch 的 events[] 元素。
type ShootEvent struct {
	Ts            int64  `json:"ts"`
	Slot          int    `json:"slot"`
	Weapon        string `json:"weapon"`
	AmmoRemaining int    `json:"ammo_remaining"`
}

// ShootBatch 对应 mr1v1_shoot_batch 的 data 字段。
type ShootBatch struct {
	MatchID string       `json:"match_id"`
	Events  []ShootEvent `json:"events"`
}

// PositionEvent 是 mr1v1_position_batch 的 events[] 元素。
type PositionEvent struct {
	Ts    int64   `json:"ts"`
	Slot  int     `json:"slot"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
	Yaw   float64 `json:"yaw"`
	Pitch float64 `json:"pitch"`
}

// PositionBatch 对应 mr1v1_position_batch 的 data 字段。
type PositionBatch struct {
	MatchID string          `json:"match_id"`
	Events  []PositionEvent `json:"events"`
}
