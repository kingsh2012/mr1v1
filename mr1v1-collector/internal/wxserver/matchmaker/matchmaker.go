package matchmaker

import (
	"encoding/json"
	"math/rand"
	"sync"

	"github.com/google/uuid"
	"mr1v1-collector/internal/wxserver/models"
)

var maps = []string{"草原", "沙漠", "雪地", "城市", "丛林"}

type Matchmaker struct {
	mu         sync.Mutex
	queue      []*models.Player
	gameServer string
}

func New(gameServerAddr string) *Matchmaker {
	return &Matchmaker{gameServer: gameServerAddr}
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

	roomID := uuid.New().String()[:8]
	selectedMap := maps[rand.Intn(len(maps))]

	msg := models.MatchMessage{
		Type:       "matched",
		ServerAddr: m.gameServer,
		RoomID:     roomID,
		Map:        selectedMap,
	}
	data, _ := json.Marshal(msg)

	go p1.Conn.WriteMessage(1, data)
	go p2.Conn.WriteMessage(1, data)
}
