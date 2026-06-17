// Package model 定义各服务写入PostgreSQL的表结构及建表DDL。
// 每个服务只执行自己拥有的表的迁移：
//   - BackendStatements  → backend 启动时执行（控制面表）
//   - ConsumerStatements → consumer 启动时执行（遥测数据表）
package model

// BackendStatements 是 backend 服务拥有的表，由 backend.New() 执行。
var BackendStatements = []string{
	`CREATE TABLE IF NOT EXISTS mr1v1_agent (
		uuid               VARCHAR(64)  PRIMARY KEY,
		hostname           VARCHAR(128) NOT NULL DEFAULT '',
		public_ip          VARCHAR(64)  NOT NULL DEFAULT '',
		local_ip           VARCHAR(64)  NOT NULL DEFAULT '',
		cpu                VARCHAR(128) NOT NULL DEFAULT '',
		mem_mb             BIGINT       NOT NULL DEFAULT 0,
		disk_gb            BIGINT       NOT NULL DEFAULT 0,
		status             VARCHAR(16)  NOT NULL DEFAULT 'enabled',
		rehlds_run_max     INT          NOT NULL DEFAULT 0,
		rehlds_port_range  VARCHAR(32)  NOT NULL DEFAULT '',
		running_containers TEXT         NOT NULL DEFAULT '',
		create_time        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
		update_time        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
		heartbeat_time     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`ALTER TABLE mr1v1_agent ADD COLUMN IF NOT EXISTS running_containers TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE mr1v1_agent ADD COLUMN IF NOT EXISTS containers_json JSONB NOT NULL DEFAULT '[]'`,
	`CREATE TABLE IF NOT EXISTS mr1v1_rehlds_config (
		id          BIGSERIAL    PRIMARY KEY,
		image       VARCHAR(256) NOT NULL,
		version     VARCHAR(64)  NOT NULL DEFAULT '',
		is_active   BOOLEAN      NOT NULL DEFAULT FALSE,
		create_time TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`DROP TABLE IF EXISTS mr1v1_match_status`,
	`CREATE TABLE IF NOT EXISTS mr1v1_match (
		match_id    VARCHAR(64)  PRIMARY KEY,
		p0_steamid  VARCHAR(64)  NOT NULL DEFAULT '',
		p1_steamid  VARCHAR(64)  NOT NULL DEFAULT '',
		server_name VARCHAR(128) NOT NULL DEFAULT '',
		agent_uuid  VARCHAR(64)  NOT NULL DEFAULT '',
		port        INT          NOT NULL DEFAULT 0,
		image       VARCHAR(256) NOT NULL DEFAULT '',
		state       VARCHAR(16)  NOT NULL DEFAULT 'creating',
		create_time TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
		update_time TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_match_agent ON mr1v1_match(agent_uuid, state)`,
	`CREATE INDEX IF NOT EXISTS idx_match_state ON mr1v1_match(state)`,
	`CREATE TABLE IF NOT EXISTS mr1v1_operation_log (
		id         BIGSERIAL    PRIMARY KEY,
		match_id   VARCHAR(64)  NOT NULL DEFAULT '',
		actor      VARCHAR(16)  NOT NULL DEFAULT '',
		action     VARCHAR(32)  NOT NULL DEFAULT '',
		detail     TEXT         NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_op_log_match_id ON mr1v1_operation_log(match_id, created_at)`,
}

// ConsumerStatements 是 consumer 服务拥有的表，由 consumer.New() 执行。
var ConsumerStatements = []string{
	`CREATE TABLE IF NOT EXISTS mr1v1_match_start (
		id         BIGSERIAL PRIMARY KEY,
		match_id   VARCHAR(64) NOT NULL UNIQUE,
		map        VARCHAR(64) NOT NULL DEFAULT '',
		p0_name    VARCHAR(64) NOT NULL DEFAULT '',
		p0_authid  VARCHAR(64) NOT NULL DEFAULT '',
		p0_userid  INT         NOT NULL DEFAULT 0,
		p1_name    VARCHAR(64) NOT NULL DEFAULT '',
		p1_authid  VARCHAR(64) NOT NULL DEFAULT '',
		p1_userid  INT         NOT NULL DEFAULT 0,
		ts         BIGINT      NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE TABLE IF NOT EXISTS mr1v1_round_end (
		id          BIGSERIAL PRIMARY KEY,
		match_id    VARCHAR(64) NOT NULL,
		round       INT         NOT NULL DEFAULT 0,
		phase       INT         NOT NULL DEFAULT 0,
		winner_slot INT         NOT NULL DEFAULT 0,
		wins0       INT         NOT NULL DEFAULT 0,
		wins1       INT         NOT NULL DEFAULT 0,
		p0_damage   INT         NOT NULL DEFAULT 0,
		p0_hits     INT         NOT NULL DEFAULT 0,
		p1_damage   INT         NOT NULL DEFAULT 0,
		p1_hits     INT         NOT NULL DEFAULT 0,
		ts          BIGINT      NOT NULL DEFAULT 0,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_round_end_match_id ON mr1v1_round_end(match_id)`,
	`CREATE TABLE IF NOT EXISTS mr1v1_match_end (
		id          BIGSERIAL PRIMARY KEY,
		match_id    VARCHAR(64) NOT NULL UNIQUE,
		end_reason  VARCHAR(32) NOT NULL DEFAULT '',
		winner_slot INT         NOT NULL DEFAULT 0,
		wins0       INT         NOT NULL DEFAULT 0,
		wins1       INT         NOT NULL DEFAULT 0,
		p0_name     VARCHAR(64) NOT NULL DEFAULT '',
		p0_authid   VARCHAR(64) NOT NULL DEFAULT '',
		p1_name     VARCHAR(64) NOT NULL DEFAULT '',
		p1_authid   VARCHAR(64) NOT NULL DEFAULT '',
		ts          BIGINT      NOT NULL DEFAULT 0,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE TABLE IF NOT EXISTS mr1v1_combat_event (
		id            BIGSERIAL PRIMARY KEY,
		match_id      VARCHAR(64) NOT NULL,
		ts            BIGINT      NOT NULL DEFAULT 0,
		attacker_slot INT         NOT NULL DEFAULT 0,
		victim_slot   INT         NOT NULL DEFAULT 0,
		weapon        VARCHAR(32) NOT NULL DEFAULT '',
		damage        INT         NOT NULL DEFAULT 0,
		hitgroup      INT         NOT NULL DEFAULT 0,
		created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_combat_event_match_id ON mr1v1_combat_event(match_id)`,
	`CREATE TABLE IF NOT EXISTS mr1v1_shoot_event (
		id             BIGSERIAL PRIMARY KEY,
		match_id       VARCHAR(64) NOT NULL,
		ts             BIGINT      NOT NULL DEFAULT 0,
		slot           INT         NOT NULL DEFAULT 0,
		weapon         VARCHAR(32) NOT NULL DEFAULT '',
		ammo_remaining INT         NOT NULL DEFAULT 0,
		created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_shoot_event_match_id ON mr1v1_shoot_event(match_id)`,
	`CREATE TABLE IF NOT EXISTS mr1v1_position_event (
		id         BIGSERIAL PRIMARY KEY,
		match_id   VARCHAR(64) NOT NULL,
		ts         BIGINT      NOT NULL DEFAULT 0,
		slot       INT         NOT NULL DEFAULT 0,
		x          DOUBLE PRECISION NOT NULL DEFAULT 0,
		y          DOUBLE PRECISION NOT NULL DEFAULT 0,
		z          DOUBLE PRECISION NOT NULL DEFAULT 0,
		yaw        DOUBLE PRECISION NOT NULL DEFAULT 0,
		pitch      DOUBLE PRECISION NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_position_event_match_id ON mr1v1_position_event(match_id)`,
}
