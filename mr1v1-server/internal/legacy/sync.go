package legacy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type apiPlayer struct {
	CserName             string  `json:"CserName"`
	Score                string  `json:"Score"`
	SteamId              string  `json:"SteamId"`
	CserNameLastGet      string  `json:"CserNameLastGet"`
	DataLastGetTime      string  `json:"DataLastGetTime"`
	CserLastGetIp        string  `json:"CserLastGetIp"`
	TotalMatch           string  `json:"TotalMatch"`
	TotalMatchWin        string  `json:"TotalMatchWin"`
	TotalMatchLose       string  `json:"TotalMatchLose"`
	TotalKill            string  `json:"TotalKill"`
	TotalDie             string  `json:"TotalDie"`
	TotalFirstKill       string  `json:"TotalFirstKill"`
	TotalKill3           string  `json:"TotalKill3"`
	TotalKill4           string  `json:"TotalKill4"`
	TotalKill5           string  `json:"TotalKill5"`
	TotalTk              string  `json:"TotalTk"`
	TotalKillAwp         string  `json:"TotalKillAwp"`
	TotalKillAk          string  `json:"TotalKillAk"`
	TotalKillM4          string  `json:"TotalKillM4"`
	TotalKillFamas       string  `json:"TotalKillFamas"`
	TotalKillGalil       string  `json:"TotalKillGalil"`
	TotalKillDeagle      string  `json:"TotalKillDeagle"`
	TotalKillUsp         string  `json:"TotalKillUsp"`
	TotalKillGlock       string  `json:"TotalKillGlock"`
	TotalKillO4          string  `json:"TotalKillO4"`
	TotalKillKnife       string  `json:"TotalKillKnife"`
	KD                   string  `json:"KD"`
	WinRate              string  `json:"WinRate"`
	IsOp                 bool    `json:"IsOp"`
	TenantName           string  `json:"TenantName"`
	TotalMatchPing       string  `json:"TotalMatchPing"`
	Sex                  string  `json:"Sex"`
	RoleName             string  `json:"RoleName"`
	TenantScoreRank      string  `json:"TenantScoreRank"`
	AverageKillPerMatch  string  `json:"AverageKillPerMatch"`
	Label                string  `json:"Label"`
	IsVoicePackageOpen   bool    `json:"IsVoicePackageOpen"`
	Rating               string  `json:"Rating"`
	WeixinPhoto          string  `json:"WeixinPhoto"`
	KdFloat              float64 `json:"KdFloat"`
	ScoreFloat           float64 `json:"ScoreFloat"`
}

type Syncer struct {
	apiURL string
	pool   *pgxpool.Pool
}

func NewSyncer(apiURL string, pool *pgxpool.Pool) *Syncer {
	return &Syncer{apiURL: apiURL, pool: pool}
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	f, _ := strconv.ParseFloat(s, 64)
	return f
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

	count := 0
	for _, p := range raw {
		if p.SteamId == "" || p.CserName == "" {
			continue
		}
		winRate := parseFloat(p.WinRate)
		_, err := s.pool.Exec(ctx, `
			INSERT INTO legacy_players (
				steam_id, name, score, name_last_get, data_last_get_time, last_ip,
				total_match, total_match_win, total_match_lose,
				total_kill, total_die, total_first_kill,
				total_kill3, total_kill4, total_kill5, total_tk,
				total_kill_awp, total_kill_ak, total_kill_m4, total_kill_famas,
				total_kill_galil, total_kill_deagle, total_kill_usp, total_kill_glock,
				total_kill_o4, total_kill_knife,
				kd, win_rate, is_op, tenant_name, total_match_ping,
				sex, role_name, tenant_score_rank,
				average_kill_per_match, label, is_voice_package_open, rating, weixin_photo,
				updated_at
			) VALUES (
				$1,$2,$3,$4,$5,$6,
				$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
				$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,
				$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,
				now()
			)
			ON CONFLICT (steam_id) DO UPDATE SET
				name                   = EXCLUDED.name,
				score                  = EXCLUDED.score,
				name_last_get          = EXCLUDED.name_last_get,
				data_last_get_time     = EXCLUDED.data_last_get_time,
				last_ip                = EXCLUDED.last_ip,
				total_match            = EXCLUDED.total_match,
				total_match_win        = EXCLUDED.total_match_win,
				total_match_lose       = EXCLUDED.total_match_lose,
				total_kill             = EXCLUDED.total_kill,
				total_die              = EXCLUDED.total_die,
				total_first_kill       = EXCLUDED.total_first_kill,
				total_kill3            = EXCLUDED.total_kill3,
				total_kill4            = EXCLUDED.total_kill4,
				total_kill5            = EXCLUDED.total_kill5,
				total_tk               = EXCLUDED.total_tk,
				total_kill_awp         = EXCLUDED.total_kill_awp,
				total_kill_ak          = EXCLUDED.total_kill_ak,
				total_kill_m4          = EXCLUDED.total_kill_m4,
				total_kill_famas       = EXCLUDED.total_kill_famas,
				total_kill_galil       = EXCLUDED.total_kill_galil,
				total_kill_deagle      = EXCLUDED.total_kill_deagle,
				total_kill_usp         = EXCLUDED.total_kill_usp,
				total_kill_glock       = EXCLUDED.total_kill_glock,
				total_kill_o4          = EXCLUDED.total_kill_o4,
				total_kill_knife       = EXCLUDED.total_kill_knife,
				kd                     = EXCLUDED.kd,
				win_rate               = EXCLUDED.win_rate,
				is_op                  = EXCLUDED.is_op,
				tenant_name            = EXCLUDED.tenant_name,
				total_match_ping       = EXCLUDED.total_match_ping,
				sex                    = EXCLUDED.sex,
				role_name              = EXCLUDED.role_name,
				tenant_score_rank      = EXCLUDED.tenant_score_rank,
				average_kill_per_match = EXCLUDED.average_kill_per_match,
				label                  = EXCLUDED.label,
				is_voice_package_open  = EXCLUDED.is_voice_package_open,
				rating                 = EXCLUDED.rating,
				weixin_photo           = EXCLUDED.weixin_photo,
				updated_at             = now()
		`,
			p.SteamId, p.CserName, p.ScoreFloat, p.CserNameLastGet, p.DataLastGetTime, p.CserLastGetIp,
			parseInt(p.TotalMatch), parseInt(p.TotalMatchWin), parseInt(p.TotalMatchLose),
			parseInt(p.TotalKill), parseInt(p.TotalDie), parseInt(p.TotalFirstKill),
			parseInt(p.TotalKill3), parseInt(p.TotalKill4), parseInt(p.TotalKill5), parseInt(p.TotalTk),
			parseInt(p.TotalKillAwp), parseInt(p.TotalKillAk), parseInt(p.TotalKillM4), parseInt(p.TotalKillFamas),
			parseInt(p.TotalKillGalil), parseInt(p.TotalKillDeagle), parseInt(p.TotalKillUsp), parseInt(p.TotalKillGlock),
			parseInt(p.TotalKillO4), parseInt(p.TotalKillKnife),
			p.KdFloat, winRate, p.IsOp, p.TenantName, parseInt(p.TotalMatchPing),
			p.Sex, p.RoleName, parseInt(p.TenantScoreRank),
			parseFloat(p.AverageKillPerMatch), p.Label, p.IsVoicePackageOpen, parseFloat(p.Rating), p.WeixinPhoto,
		)
		if err != nil {
			slog.Warn("legacy sync: upsert failed", "steam_id", p.SteamId, "err", err)
			continue
		}
		count++
	}
	slog.Info("legacy sync: done", "count", count)
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
