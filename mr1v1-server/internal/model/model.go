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
		containers_json    JSONB        NOT NULL DEFAULT '[]',
		created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
		updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
		heartbeat_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	// 旧列名迁移
	`DO $$ BEGIN
		IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='manager_agents' AND column_name='create_time') THEN
			ALTER TABLE manager_agents RENAME COLUMN create_time TO created_at;
		END IF;
	END $$`,
	`DO $$ BEGIN
		IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='manager_agents' AND column_name='update_time') THEN
			ALTER TABLE manager_agents RENAME COLUMN update_time TO updated_at;
		END IF;
	END $$`,
	`DO $$ BEGIN
		IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='manager_agents' AND column_name='heartbeat_time') THEN
			ALTER TABLE manager_agents RENAME COLUMN heartbeat_time TO heartbeat_at;
		END IF;
	END $$`,
	`CREATE TABLE IF NOT EXISTS manager_rehlds_configs (
		id          BIGSERIAL    PRIMARY KEY,
		image       VARCHAR(256) NOT NULL,
		version     VARCHAR(64)  NOT NULL DEFAULT '',
		is_active   BOOLEAN      NOT NULL DEFAULT FALSE,
		created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`DO $$ BEGIN
		IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='manager_rehlds_configs' AND column_name='create_time') THEN
			ALTER TABLE manager_rehlds_configs RENAME COLUMN create_time TO created_at;
		END IF;
	END $$`,
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
		created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
		updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`DO $$ BEGIN
		IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='manager_matches' AND column_name='create_time') THEN
			ALTER TABLE manager_matches RENAME COLUMN create_time TO created_at;
		END IF;
	END $$`,
	`DO $$ BEGIN
		IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='manager_matches' AND column_name='update_time') THEN
			ALTER TABLE manager_matches RENAME COLUMN update_time TO updated_at;
		END IF;
	END $$`,
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
	`CREATE TABLE IF NOT EXISTS legacy_players (
		steam_id               TEXT PRIMARY KEY,
		name                   TEXT NOT NULL DEFAULT '',
		score                  NUMERIC(10,2) NOT NULL DEFAULT 0,
		name_last_get          TEXT NOT NULL DEFAULT '',
		data_last_get_time     TEXT NOT NULL DEFAULT '',
		last_ip                TEXT NOT NULL DEFAULT '',
		total_match            INT  NOT NULL DEFAULT 0,
		total_match_win        INT  NOT NULL DEFAULT 0,
		total_match_lose       INT  NOT NULL DEFAULT 0,
		total_kill             INT  NOT NULL DEFAULT 0,
		total_die              INT  NOT NULL DEFAULT 0,
		total_first_kill       INT  NOT NULL DEFAULT 0,
		total_kill3            INT  NOT NULL DEFAULT 0,
		total_kill4            INT  NOT NULL DEFAULT 0,
		total_kill5            INT  NOT NULL DEFAULT 0,
		total_tk               INT  NOT NULL DEFAULT 0,
		total_kill_awp         INT  NOT NULL DEFAULT 0,
		total_kill_ak          INT  NOT NULL DEFAULT 0,
		total_kill_m4          INT  NOT NULL DEFAULT 0,
		total_kill_famas       INT  NOT NULL DEFAULT 0,
		total_kill_galil       INT  NOT NULL DEFAULT 0,
		total_kill_deagle      INT  NOT NULL DEFAULT 0,
		total_kill_usp         INT  NOT NULL DEFAULT 0,
		total_kill_glock       INT  NOT NULL DEFAULT 0,
		total_kill_o4          INT  NOT NULL DEFAULT 0,
		total_kill_knife       INT  NOT NULL DEFAULT 0,
		kd                     NUMERIC(8,4) NOT NULL DEFAULT 0,
		win_rate               NUMERIC(6,2) NOT NULL DEFAULT 0,
		is_op                  BOOLEAN NOT NULL DEFAULT FALSE,
		tenant_name            TEXT NOT NULL DEFAULT '',
		total_match_ping       INT  NOT NULL DEFAULT 0,
		sex                    TEXT NOT NULL DEFAULT '',
		role_name              TEXT NOT NULL DEFAULT '',
		tenant_score_rank      INT  NOT NULL DEFAULT 0,
		average_kill_per_match NUMERIC(8,2) NOT NULL DEFAULT 0,
		label                  TEXT NOT NULL DEFAULT '',
		is_voice_package_open  BOOLEAN NOT NULL DEFAULT FALSE,
		rating                 NUMERIC(8,4) NOT NULL DEFAULT 0,
		weixin_photo           TEXT NOT NULL DEFAULT '',
		updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
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
