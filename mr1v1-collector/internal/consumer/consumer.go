// Package consumer 实现MQTT订阅 -> 按事件类型路由 -> 写入PostgreSQL。
package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mr1v1-collector/internal/config"
	"mr1v1-collector/internal/envelope"
	"mr1v1-collector/internal/model"
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

	if _, err := pool.Exec(context.Background(), model.DDL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate tables: %w", err)
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
		if token := client.Subscribe(cfg.MQTT.Topic, 1, c.onMessage); token.Wait() && token.Error() != nil {
			slog.Error("subscribe failed", "topic", cfg.MQTT.Topic, "error", token.Error())
		} else {
			slog.Info("subscribed", "topic", cfg.MQTT.Topic)
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
		_, err := c.pool.Exec(ctx,
			`INSERT INTO mr1v1_match_start
				(match_id,map,p0_name,p0_authid,p0_userid,p1_name,p1_authid,p1_userid,ts)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			 ON CONFLICT (match_id) DO NOTHING`,
			d.MatchID, d.Map,
			d.P0Name, d.P0AuthID, d.P0UserID,
			d.P1Name, d.P1AuthID, d.P1UserID,
			env.Timestamp,
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
		_, err := c.pool.Exec(ctx,
			`INSERT INTO mr1v1_match_end
				(match_id,end_reason,winner_slot,wins0,wins1,p0_name,p0_authid,p1_name,p1_authid,ts)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			 ON CONFLICT (match_id) DO NOTHING`,
			d.MatchID, d.EndReason, d.WinnerSlot,
			d.Wins0, d.Wins1,
			d.P0Name, d.P0AuthID, d.P1Name, d.P1AuthID,
			env.Timestamp,
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
