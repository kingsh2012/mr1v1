// Package consumer 实现MQTT订阅 -> 按事件类型路由 -> 写入PostgreSQL。
package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mr1v1-server/internal/agentproto"
	"mr1v1-server/internal/config"
	"mr1v1-server/internal/envelope"
	"mr1v1-server/internal/model"
)

// Consumer 持有DB连接池和MQTT客户端。
type Consumer struct {
	cfg    *config.ConsumerConfig
	pool   *pgxpool.Pool
	client mqtt.Client
}

// New 连接PostgreSQL（并建表）与MQTT broker，订阅配置的topic。
func New(cfg *config.ConsumerConfig) (*Consumer, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		cfg.DB.Host, cfg.DB.Port, cfg.DB.User, cfg.DB.Pass, cfg.DB.DBName, cfg.DB.SSLMode, cfg.DB.Timezone)

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	for _, stmt := range model.Statements {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			pool.Close()
			return nil, fmt.Errorf("migrate tables: %w\nSQL: %s", err, stmt[:min(len(stmt), 80)])
		}
	}

	c := &Consumer{cfg: cfg, pool: pool}

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.MQTT.Broker).
		SetClientID(cfg.MQTT.ClientID).
		SetAutoReconnect(true).
		SetCleanSession(false).
		SetConnectTimeout(10 * time.Second).
		SetKeepAlive(30 * time.Second)
	if cfg.MQTT.User != "" {
		opts.SetUsername(cfg.MQTT.User)
		opts.SetPassword(cfg.MQTT.Pass)
	}
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		subs := map[string]struct {
			qos     byte
			handler mqtt.MessageHandler
		}{
			cfg.MQTT.Topic:                       {1, c.onMessage},
			agentproto.HeartbeatSubscribeFilter:  {0, c.onHeartbeat},
		}
		for topic, s := range subs {
			if token := client.Subscribe(topic, s.qos, s.handler); token.Wait() && token.Error() != nil {
				slog.Error("subscribe failed", "topic", topic, "error", token.Error())
			} else {
				slog.Info("subscribed", "topic", topic)
			}
		}
	})

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		pool.Close()
		return nil, fmt.Errorf("connect mqtt broker %s: %w", cfg.MQTT.Broker, token.Error())
	}
	c.client = client

	return c, nil
}

// Close 断开MQTT连接并关闭DB连接池。
func (c *Consumer) Close() {
	c.client.Disconnect(250)
	c.pool.Close()
}

func (c *Consumer) onMessage(_ mqtt.Client, msg mqtt.Message) {
	var env envelope.Envelope
	if err := json.Unmarshal(msg.Payload(), &env); err != nil {
		slog.Error("unmarshal envelope failed", "error", err, "topic", msg.Topic())
		return
	}

	if err := c.handle(env); err != nil {
		slog.Error("handle event failed", "error", err, "type", env.Type, "match_id", env.MatchID)
	}
}

func (c *Consumer) handle(env envelope.Envelope) error {
	ctx := context.Background()
	switch env.Type {
	case envelope.TypeMatchStart:
		var d envelope.MatchStart
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		if _, err := c.pool.Exec(ctx,
			`INSERT INTO mr1v1_match_start
				(match_id,map,p0_name,p0_authid,p0_userid,p1_name,p1_authid,p1_userid,ts)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			 ON CONFLICT (match_id) DO NOTHING`,
			d.MatchID, d.Map,
			d.P0Name, d.P0AuthID, d.P0UserID,
			d.P1Name, d.P1AuthID, d.P1UserID,
			env.Timestamp,
		); err != nil {
			return err
		}
		if _, err := c.pool.Exec(ctx,
			`UPDATE mr1v1_match SET state='playing', update_time=NOW()
			 WHERE match_id=$1 AND state IN ('creating','waiting')`,
			d.MatchID,
		); err != nil {
			return err
		}
		_, err := c.pool.Exec(ctx,
			`INSERT INTO mr1v1_operation_log (match_id, actor, action, detail)
			 VALUES ($1,'game','match_started',$2)`,
			d.MatchID,
			fmt.Sprintf(`{"map":"%s","p0":"%s","p1":"%s"}`, d.Map, d.P0AuthID, d.P1AuthID),
		)
		return err

	case envelope.TypeRoundEnd:
		var d envelope.RoundEnd
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		_, err := c.pool.Exec(ctx,
			`INSERT INTO mr1v1_round_end
				(match_id,round,phase,winner_slot,wins0,wins1,p0_damage,p0_hits,p1_damage,p1_hits,ts)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			d.MatchID, d.Round, d.Phase, d.WinnerSlot,
			d.Wins0, d.Wins1,
			d.P0Damage, d.P0Hits, d.P1Damage, d.P1Hits,
			env.Timestamp,
		)
		return err

	case envelope.TypeMatchEnd:
		var d envelope.MatchEnd
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		if _, err := c.pool.Exec(ctx,
			`INSERT INTO mr1v1_match_end
				(match_id,end_reason,winner_slot,wins0,wins1,p0_name,p0_authid,p1_name,p1_authid,ts)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			 ON CONFLICT (match_id) DO NOTHING`,
			d.MatchID, d.EndReason, d.WinnerSlot,
			d.Wins0, d.Wins1,
			d.P0Name, d.P0AuthID, d.P1Name, d.P1AuthID,
			env.Timestamp,
		); err != nil {
			return err
		}
		if _, err := c.pool.Exec(ctx,
			`UPDATE mr1v1_match SET state='finished', update_time=NOW()
			 WHERE match_id=$1 AND state != 'finished'`,
			d.MatchID,
		); err != nil {
			return err
		}
		_, err := c.pool.Exec(ctx,
			`INSERT INTO mr1v1_operation_log (match_id, actor, action, detail)
			 VALUES ($1,'game','match_ended',$2)`,
			d.MatchID,
			fmt.Sprintf(`{"winner_slot":%d,"wins0":%d,"wins1":%d,"end_reason":"%s"}`,
				d.WinnerSlot, d.Wins0, d.Wins1, d.EndReason),
		)
		return err

	case envelope.TypeCombatBatch:
		var d envelope.CombatBatch
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		if len(d.Events) == 0 {
			return nil
		}
		rows := make([][]any, 0, len(d.Events))
		for _, ev := range d.Events {
			rows = append(rows, []any{d.MatchID, ev.Ts, ev.AttackerSlot, ev.VictimSlot, ev.Weapon, ev.Damage, ev.Hitgroup})
		}
		_, err := c.pool.CopyFrom(ctx,
			pgx.Identifier{"mr1v1_combat_event"},
			[]string{"match_id", "ts", "attacker_slot", "victim_slot", "weapon", "damage", "hitgroup"},
			pgx.CopyFromRows(rows),
		)
		return err

	case envelope.TypeShootBatch:
		var d envelope.ShootBatch
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		if len(d.Events) == 0 {
			return nil
		}
		rows := make([][]any, 0, len(d.Events))
		for _, ev := range d.Events {
			rows = append(rows, []any{d.MatchID, ev.Ts, ev.Slot, ev.Weapon, ev.AmmoRemaining})
		}
		_, err := c.pool.CopyFrom(ctx,
			pgx.Identifier{"mr1v1_shoot_event"},
			[]string{"match_id", "ts", "slot", "weapon", "ammo_remaining"},
			pgx.CopyFromRows(rows),
		)
		return err

	case envelope.TypePositionBatch:
		var d envelope.PositionBatch
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		if len(d.Events) == 0 {
			return nil
		}
		rows := make([][]any, 0, len(d.Events))
		for _, ev := range d.Events {
			rows = append(rows, []any{d.MatchID, ev.Ts, ev.Slot, ev.X, ev.Y, ev.Z, ev.Yaw, ev.Pitch})
		}
		_, err := c.pool.CopyFrom(ctx,
			pgx.Identifier{"mr1v1_position_event"},
			[]string{"match_id", "ts", "slot", "x", "y", "z", "yaw", "pitch"},
			pgx.CopyFromRows(rows),
		)
		return err

	default:
		slog.Warn("unknown event type, skipped", "type", env.Type, "match_id", env.MatchID)
		return nil
	}
}

func (c *Consumer) onHeartbeat(_ mqtt.Client, msg mqtt.Message) {
	var hb agentproto.Heartbeat
	if err := json.Unmarshal(msg.Payload(), &hb); err != nil {
		slog.Error("decode heartbeat failed", "error", err)
		return
	}
	if hb.UUID == "" {
		return
	}
	if err := c.upsertAgent(hb); err != nil {
		slog.Error("upsert agent failed", "error", err, "uuid", hb.UUID)
	}
}

func (c *Consumer) upsertAgent(hb agentproto.Heartbeat) error {
	cpuCount, _ := strconv.Atoi(hb.CPU)
	runningContainers := strings.Join(hb.RunningMatches, ",")
	containersJSON, _ := json.Marshal(hb.Containers)
	if len(containersJSON) == 0 {
		containersJSON = []byte("[]")
	}
	_, err := c.pool.Exec(context.Background(), `
		INSERT INTO mr1v1_agent
			(uuid, hostname, public_ip, local_ip, cpu, mem_mb, disk_gb,
			 status, rehlds_run_max, rehlds_port_range, running_containers, containers_json,
			 create_time, update_time, heartbeat_time)
		VALUES ($1,$2,$3,$4,$5,$6,$7, 'enabled', $8, '', $9, $10, NOW(), NOW(), NOW())
		ON CONFLICT (uuid) DO UPDATE SET
			heartbeat_time     = NOW(),
			running_containers = EXCLUDED.running_containers,
			containers_json    = EXCLUDED.containers_json,
			update_time = CASE
				WHEN mr1v1_agent.hostname  != EXCLUDED.hostname
				  OR mr1v1_agent.public_ip != EXCLUDED.public_ip
				  OR mr1v1_agent.local_ip  != EXCLUDED.local_ip
				  OR mr1v1_agent.cpu       != EXCLUDED.cpu
				  OR mr1v1_agent.mem_mb    != EXCLUDED.mem_mb
				  OR mr1v1_agent.disk_gb   != EXCLUDED.disk_gb
				THEN NOW()
				ELSE mr1v1_agent.update_time
			END,
			hostname  = EXCLUDED.hostname,
			public_ip = EXCLUDED.public_ip,
			local_ip  = EXCLUDED.local_ip,
			cpu       = EXCLUDED.cpu,
			mem_mb    = EXCLUDED.mem_mb,
			disk_gb   = EXCLUDED.disk_gb,
			rehlds_run_max = CASE
				WHEN mr1v1_agent.rehlds_run_max = 0 THEN $8
				ELSE mr1v1_agent.rehlds_run_max
			END
	`, hb.UUID, hb.Hostname, hb.PublicIP, hb.LocalIP, hb.CPU, hb.MemMB, hb.DiskGB, cpuCount, runningContainers, containersJSON)
	return err
}
