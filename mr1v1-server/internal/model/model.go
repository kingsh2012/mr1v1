// Package model 定义各服务写入PostgreSQL的表结构及建表DDL。
// 每个服务只执行自己拥有的表的迁移：
//   - BackendStatements  → backend 启动时执行（控制面表，前缀 manager_）
//   - ConsumerStatements → consumer 启动时执行（遥测数据表，前缀 telemetry_）
package model

// BackendStatements 是 backend 服务拥有的表，由 backend.New() 执行。
var BackendStatements = []string{
	`CREATE TABLE IF NOT EXISTS manager_agents (
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
	`ALTER TABLE manager_agents ADD COLUMN IF NOT EXISTS running_containers TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE manager_agents ADD COLUMN IF NOT EXISTS containers_json JSONB NOT NULL DEFAULT '[]'`,
	`CREATE TABLE IF NOT EXISTS manager_rehlds_configs (
		id          BIGSERIAL    PRIMARY KEY,
		image       VARCHAR(256) NOT NULL,
		version     VARCHAR(64)  NOT NULL DEFAULT '',
		is_active   BOOLEAN      NOT NULL DEFAULT FALSE,
		create_time TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`DROP TABLE IF EXISTS mr1v1_match_status`,
	`CREATE TABLE IF NOT EXISTS manager_matches (
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
	`CREATE INDEX IF NOT EXISTS idx_manager_matches_agent ON manager_matches(agent_uuid, state)`,
	`CREATE INDEX IF NOT EXISTS idx_manager_matches_state ON manager_matches(state)`,
	`CREATE TABLE IF NOT EXISTS manager_operation_logs (
		id         BIGSERIAL    PRIMARY KEY,
		match_id   VARCHAR(64)  NOT NULL DEFAULT '',
		actor      VARCHAR(16)  NOT NULL DEFAULT '',
		action     VARCHAR(32)  NOT NULL DEFAULT '',
		detail     TEXT         NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_manager_operation_logs_match_id ON manager_operation_logs(match_id, created_at)`,
}

// ConsumerStatements 是 consumer 服务拥有的表，由 consumer.New() 执行。
var ConsumerStatements = []string{
	`CREATE TABLE IF NOT EXISTS telemetry_match_starts (
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
	`CREATE TABLE IF NOT EXISTS telemetry_round_ends (
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
	`CREATE INDEX IF NOT EXISTS idx_telemetry_round_ends_match_id ON telemetry_round_ends(match_id)`,
	`CREATE TABLE IF NOT EXISTS telemetry_match_ends (
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
	`CREATE TABLE IF NOT EXISTS telemetry_combat_events (
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
	`CREATE INDEX IF NOT EXISTS idx_telemetry_combat_events_match_id ON telemetry_combat_events(match_id)`,
	`CREATE TABLE IF NOT EXISTS telemetry_shoot_events (
		id             BIGSERIAL PRIMARY KEY,
		match_id       VARCHAR(64) NOT NULL,
		ts             BIGINT      NOT NULL DEFAULT 0,
		slot           INT         NOT NULL DEFAULT 0,
		weapon         VARCHAR(32) NOT NULL DEFAULT '',
		ammo_remaining INT         NOT NULL DEFAULT 0,
		created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_telemetry_shoot_events_match_id ON telemetry_shoot_events(match_id)`,
	`CREATE TABLE IF NOT EXISTS telemetry_position_events (
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
	`CREATE INDEX IF NOT EXISTS idx_telemetry_position_events_match_id ON telemetry_position_events(match_id)`,
}
