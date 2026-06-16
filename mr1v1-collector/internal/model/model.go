// Package model 定义consumer写入PostgreSQL的6张表结构及建表DDL。
package model

// DDL 包含所有建表语句（IF NOT EXISTS），consumer启动时执行。
const DDL = `
CREATE TABLE IF NOT EXISTS mr1v1_match_start (
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
);

CREATE TABLE IF NOT EXISTS mr1v1_round_end (
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
);
CREATE INDEX IF NOT EXISTS idx_round_end_match_id ON mr1v1_round_end(match_id);

CREATE TABLE IF NOT EXISTS mr1v1_match_end (
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
);

CREATE TABLE IF NOT EXISTS mr1v1_combat_event (
	id            BIGSERIAL PRIMARY KEY,
	match_id      VARCHAR(64) NOT NULL,
	ts            BIGINT      NOT NULL DEFAULT 0,
	attacker_slot INT         NOT NULL DEFAULT 0,
	victim_slot   INT         NOT NULL DEFAULT 0,
	weapon        VARCHAR(32) NOT NULL DEFAULT '',
	damage        INT         NOT NULL DEFAULT 0,
	hitgroup      INT         NOT NULL DEFAULT 0,
	created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_combat_event_match_id ON mr1v1_combat_event(match_id);

CREATE TABLE IF NOT EXISTS mr1v1_shoot_event (
	id             BIGSERIAL PRIMARY KEY,
	match_id       VARCHAR(64) NOT NULL,
	ts             BIGINT      NOT NULL DEFAULT 0,
	slot           INT         NOT NULL DEFAULT 0,
	weapon         VARCHAR(32) NOT NULL DEFAULT '',
	ammo_remaining INT         NOT NULL DEFAULT 0,
	created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_shoot_event_match_id ON mr1v1_shoot_event(match_id);

CREATE TABLE IF NOT EXISTS mr1v1_position_event (
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
);
CREATE INDEX IF NOT EXISTS idx_position_event_match_id ON mr1v1_position_event(match_id);
`
