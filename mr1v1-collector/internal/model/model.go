// Package model 定义consumer写入PostgreSQL的6张表结构（GORM AutoMigrate）。
package model

import "time"

// AllTables 用于AutoMigrate，列出所有需要建表的model。
var AllTables = []any{
	&MatchStart{},
	&RoundEnd{},
	&MatchEnd{},
	&CombatEvent{},
	&ShootEvent{},
	&PositionEvent{},
}

// MatchStart 对应 mr1v1_match_start 事件，每场比赛一行。
type MatchStart struct {
	ID        uint   `gorm:"primaryKey"`
	MatchID   string `gorm:"column:match_id;uniqueIndex;size:64"`
	Map       string `gorm:"column:map;size:64"`
	P0Name    string `gorm:"column:p0_name;size:64"`
	P0AuthID  string `gorm:"column:p0_authid;size:64"`
	P0UserID  int    `gorm:"column:p0_userid"`
	P1Name    string `gorm:"column:p1_name;size:64"`
	P1AuthID  string `gorm:"column:p1_authid;size:64"`
	P1UserID  int    `gorm:"column:p1_userid"`
	Ts        int64  `gorm:"column:ts"`
	CreatedAt time.Time
}

func (MatchStart) TableName() string { return "mr1v1_match_start" }

// RoundEnd 对应 mr1v1_round_end 事件，每回合一行。
type RoundEnd struct {
	ID         uint   `gorm:"primaryKey"`
	MatchID    string `gorm:"column:match_id;index;size:64"`
	Round      int    `gorm:"column:round"`
	Phase      int    `gorm:"column:phase"`
	WinnerSlot int    `gorm:"column:winner_slot"`
	Wins0      int    `gorm:"column:wins0"`
	Wins1      int    `gorm:"column:wins1"`
	P0Damage   int    `gorm:"column:p0_damage"`
	P0Hits     int    `gorm:"column:p0_hits"`
	P1Damage   int    `gorm:"column:p1_damage"`
	P1Hits     int    `gorm:"column:p1_hits"`
	Ts         int64  `gorm:"column:ts"`
	CreatedAt  time.Time
}

func (RoundEnd) TableName() string { return "mr1v1_round_end" }

// MatchEnd 对应 mr1v1_match_end 事件，每场比赛一行。
type MatchEnd struct {
	ID         uint   `gorm:"primaryKey"`
	MatchID    string `gorm:"column:match_id;uniqueIndex;size:64"`
	EndReason  string `gorm:"column:end_reason;size:32"`
	WinnerSlot int    `gorm:"column:winner_slot"`
	Wins0      int    `gorm:"column:wins0"`
	Wins1      int    `gorm:"column:wins1"`
	P0Name     string `gorm:"column:p0_name;size:64"`
	P0AuthID   string `gorm:"column:p0_authid;size:64"`
	P1Name     string `gorm:"column:p1_name;size:64"`
	P1AuthID   string `gorm:"column:p1_authid;size:64"`
	Ts         int64  `gorm:"column:ts"`
	CreatedAt  time.Time
}

func (MatchEnd) TableName() string { return "mr1v1_match_end" }

// CombatEvent 对应 mr1v1_combat_batch 的 events[] 元素，每次命中一行。
type CombatEvent struct {
	ID           uint   `gorm:"primaryKey"`
	MatchID      string `gorm:"column:match_id;index;size:64"`
	Ts           int64  `gorm:"column:ts"`
	AttackerSlot int    `gorm:"column:attacker_slot"`
	VictimSlot   int    `gorm:"column:victim_slot"`
	Weapon       string `gorm:"column:weapon;size:32"`
	Damage       int    `gorm:"column:damage"`
	Hitgroup     int    `gorm:"column:hitgroup"`
	CreatedAt    time.Time
}

func (CombatEvent) TableName() string { return "mr1v1_combat_event" }

// ShootEvent 对应 mr1v1_shoot_batch 的 events[] 元素，每次开枪一行。
type ShootEvent struct {
	ID            uint   `gorm:"primaryKey"`
	MatchID       string `gorm:"column:match_id;index;size:64"`
	Ts            int64  `gorm:"column:ts"`
	Slot          int    `gorm:"column:slot"`
	Weapon        string `gorm:"column:weapon;size:32"`
	AmmoRemaining int    `gorm:"column:ammo_remaining"`
	CreatedAt     time.Time
}

func (ShootEvent) TableName() string { return "mr1v1_shoot_event" }

// PositionEvent 对应 mr1v1_position_batch 的 events[] 元素，每次采样一行。
type PositionEvent struct {
	ID        uint    `gorm:"primaryKey"`
	MatchID   string  `gorm:"column:match_id;index;size:64"`
	Ts        int64   `gorm:"column:ts"`
	Slot      int     `gorm:"column:slot"`
	X         float64 `gorm:"column:x"`
	Y         float64 `gorm:"column:y"`
	Z         float64 `gorm:"column:z"`
	Yaw       float64 `gorm:"column:yaw"`
	Pitch     float64 `gorm:"column:pitch"`
	CreatedAt time.Time
}

func (PositionEvent) TableName() string { return "mr1v1_position_event" }
