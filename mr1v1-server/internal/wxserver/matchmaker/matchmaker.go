package matchmaker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"mr1v1-server/internal/wxserver/models"
)

type Matchmaker struct {
	mu             sync.Mutex
	queue          []*models.Player
	backendURL     string
	internalAPIKey string
}

func New(backendURL, internalAPIKey string) *Matchmaker {
	return &Matchmaker{backendURL: backendURL, internalAPIKey: internalAPIKey}
}

func (m *Matchmaker) Join(p *models.Player) {
	m.mu.Lock()
	m.queue = append(m.queue, p)
	queueLen := len(m.queue)
	m.mu.Unlock()

	if queueLen >= 2 {
		m.tryMatch()
	}
}

func (m *Matchmaker) Leave(p *models.Player) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, player := range m.queue {
		if player.OpenID == p.OpenID {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return
		}
	}
}

func (m *Matchmaker) tryMatch() {
	m.mu.Lock()
	if len(m.queue) < 2 {
		m.mu.Unlock()
		return
	}
	p1 := m.queue[0]
	p2 := m.queue[1]
	m.queue = m.queue[2:]
	m.mu.Unlock()

	matchID, serverAddr, err := m.createMatch(p1.SteamID, p2.SteamID)
	if err != nil {
		slog.Error("create match failed", "error", err,
			"p1", p1.OpenID, "p2", p2.OpenID)
		errMsg, _ := json.Marshal(models.MatchMessage{Type: "error", Message: "建房失败，请重试"})
		go p1.Conn.WriteMessage(1, errMsg)
		go p2.Conn.WriteMessage(1, errMsg)
		// 重新入队
		m.mu.Lock()
		m.queue = append([]*models.Player{p1, p2}, m.queue...)
		m.mu.Unlock()
		return
	}

	msg := models.MatchMessage{
		Type:       "matched",
		MatchID:    matchID,
		ServerAddr: serverAddr,
	}
	data, _ := json.Marshal(msg)

	slog.Info("match created", "match_id", matchID, "server_addr", serverAddr,
		"p1", p1.OpenID, "p2", p2.OpenID)

	go p1.Conn.WriteMessage(1, data)
	go p2.Conn.WriteMessage(1, data)
}

type createMatchResp struct {
	MatchID  string `json:"match_id"`
	PublicIP string `json:"public_ip"`
	Port     int    `json:"port"`
}

func (m *Matchmaker) createMatch(p0SteamID, p1SteamID string) (matchID, serverAddr string, err error) {
	body, _ := json.Marshal(map[string]string{
		"p0_steamid": p0SteamID,
		"p1_steamid": p1SteamID,
	})
	req, err := http.NewRequest(http.MethodPost, m.backendURL+"/api/manager/matches", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if m.internalAPIKey != "" {
		req.Header.Set("X-API-Key", m.internalAPIKey)
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec
	if err != nil {
		return "", "", fmt.Errorf("call backend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("backend returned %d: %s", resp.StatusCode, string(raw))
	}
	var r createMatchResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", fmt.Errorf("decode response: %w", err)
	}
	return r.MatchID, fmt.Sprintf("%s:%d", r.PublicIP, r.Port), nil
}
