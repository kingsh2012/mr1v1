package legacy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"mr1v1-server/internal/wxserver/store"
)

type apiPlayer struct {
	CserName string `json:"CserName"`
	SteamID  string `json:"SteamId"`
}

type Syncer struct {
	apiURL string
	db     *store.Store
}

func NewSyncer(apiURL string, db *store.Store) *Syncer {
	return &Syncer{apiURL: apiURL, db: db}
}

func (s *Syncer) sync(ctx context.Context) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(s.apiURL)
	if err != nil {
		slog.Warn("legacy sync: fetch failed", "err", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("legacy sync: read body failed", "err", err)
		return
	}

	cleaned := strings.ReplaceAll(string(body), "～", "~")
	cleaned = strings.ReplaceAll(cleaned, "非数字", "0.0")

	var raw []apiPlayer
	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		slog.Warn("legacy sync: json parse failed", "err", err)
		return
	}

	players := make([]store.LegacyPlayer, 0, len(raw))
	for _, r := range raw {
		if r.SteamID == "" || r.CserName == "" {
			continue
		}
		players = append(players, store.LegacyPlayer{SteamID: r.SteamID, Name: r.CserName})
	}

	if err := s.db.UpsertLegacyPlayers(ctx, players); err != nil {
		slog.Warn("legacy sync: upsert failed", "err", err)
		return
	}
	slog.Info("legacy sync: done", "count", len(players))
}

func (s *Syncer) Start(ctx context.Context, interval time.Duration) {
	s.sync(ctx)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.sync(ctx)
		}
	}
}
