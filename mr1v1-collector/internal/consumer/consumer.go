// Package consumer 实现MQTT订阅 -> 按事件类型路由 -> 写入PostgreSQL。
package consumer

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"mr1v1-collector/internal/config"
	"mr1v1-collector/internal/envelope"
	"mr1v1-collector/internal/model"
)

const batchSize = 100

// Consumer 持有DB连接和MQTT客户端。
type Consumer struct {
	cfg    *config.ConsumerConfig
	db     *gorm.DB
	client mqtt.Client
}

// New 连接PostgreSQL（并AutoMigrate）与MQTT broker，订阅配置的topic。
func New(cfg *config.ConsumerConfig) (*Consumer, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		cfg.DB.Host, cfg.DB.Port, cfg.DB.User, cfg.DB.Pass, cfg.DB.DBName, cfg.DB.SSLMode, cfg.DB.Timezone)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	if err := db.AutoMigrate(model.AllTables...); err != nil {
		return nil, fmt.Errorf("automigrate: %w", err)
	}

	c := &Consumer{cfg: cfg, db: db}

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
		return nil, fmt.Errorf("connect mqtt broker %s: %w", cfg.MQTT.Broker, token.Error())
	}
	c.client = client

	return c, nil
}

// Close 断开MQTT连接。
func (c *Consumer) Close() {
	c.client.Disconnect(250)
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
	switch env.Type {
	case envelope.TypeMatchStart:
		var d envelope.MatchStart
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		row := model.MatchStart{
			MatchID: d.MatchID, Map: d.Map,
			P0Name: d.P0Name, P0AuthID: d.P0AuthID, P0UserID: d.P0UserID,
			P1Name: d.P1Name, P1AuthID: d.P1AuthID, P1UserID: d.P1UserID,
			Ts: env.Timestamp,
		}
		return c.db.Create(&row).Error

	case envelope.TypeRoundEnd:
		var d envelope.RoundEnd
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		row := model.RoundEnd{
			MatchID: d.MatchID, Round: d.Round, Phase: d.Phase, WinnerSlot: d.WinnerSlot,
			Wins0: d.Wins0, Wins1: d.Wins1,
			P0Damage: d.P0Damage, P0Hits: d.P0Hits, P1Damage: d.P1Damage, P1Hits: d.P1Hits,
			Ts: env.Timestamp,
		}
		return c.db.Create(&row).Error

	case envelope.TypeMatchEnd:
		var d envelope.MatchEnd
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		row := model.MatchEnd{
			MatchID: d.MatchID, EndReason: d.EndReason, WinnerSlot: d.WinnerSlot,
			Wins0: d.Wins0, Wins1: d.Wins1,
			P0Name: d.P0Name, P0AuthID: d.P0AuthID, P1Name: d.P1Name, P1AuthID: d.P1AuthID,
			Ts: env.Timestamp,
		}
		return c.db.Create(&row).Error

	case envelope.TypeCombatBatch:
		var d envelope.CombatBatch
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		rows := make([]model.CombatEvent, 0, len(d.Events))
		for _, ev := range d.Events {
			rows = append(rows, model.CombatEvent{
				MatchID: d.MatchID, Ts: ev.Ts,
				AttackerSlot: ev.AttackerSlot, VictimSlot: ev.VictimSlot,
				Weapon: ev.Weapon, Damage: ev.Damage, Hitgroup: ev.Hitgroup,
			})
		}
		if len(rows) == 0 {
			return nil
		}
		return c.db.CreateInBatches(rows, batchSize).Error

	case envelope.TypeShootBatch:
		var d envelope.ShootBatch
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		rows := make([]model.ShootEvent, 0, len(d.Events))
		for _, ev := range d.Events {
			rows = append(rows, model.ShootEvent{
				MatchID: d.MatchID, Ts: ev.Ts, Slot: ev.Slot,
				Weapon: ev.Weapon, AmmoRemaining: ev.AmmoRemaining,
			})
		}
		if len(rows) == 0 {
			return nil
		}
		return c.db.CreateInBatches(rows, batchSize).Error

	case envelope.TypePositionBatch:
		var d envelope.PositionBatch
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		rows := make([]model.PositionEvent, 0, len(d.Events))
		for _, ev := range d.Events {
			rows = append(rows, model.PositionEvent{
				MatchID: d.MatchID, Ts: ev.Ts, Slot: ev.Slot,
				X: ev.X, Y: ev.Y, Z: ev.Z, Yaw: ev.Yaw, Pitch: ev.Pitch,
			})
		}
		if len(rows) == 0 {
			return nil
		}
		return c.db.CreateInBatches(rows, batchSize).Error

	default:
		slog.Warn("unknown event type, skipped", "type", env.Type, "match_id", env.MatchID)
		return nil
	}
}
