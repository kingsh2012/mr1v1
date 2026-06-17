package room

import (
	"sync"

	"mr1v1-server/internal/wxserver/store"
)

type Manager struct {
	mu             sync.Mutex
	hubs           map[string]*Hub
	backendURL     string
	internalAPIKey string
	store          *store.Store
}

func NewManager(backendURL, internalAPIKey string, s *store.Store) *Manager {
	return &Manager{
		hubs:           make(map[string]*Hub),
		backendURL:     backendURL,
		internalAPIKey: internalAPIKey,
		store:          s,
	}
}

func (m *Manager) GetOrCreate(roomID string) *Hub {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.hubs[roomID]; ok {
		return h
	}
	h := newHub(roomID, m.backendURL, m.internalAPIKey, m.store, func() {
		m.mu.Lock()
		delete(m.hubs, roomID)
		m.mu.Unlock()
	})
	m.hubs[roomID] = h
	return h
}

// IsActive 房间当前是否有人在线连接。Hub 在双方都断开时会从 hubs 中移除
// （见 GetOrCreate 的 onEmpty 回调），所以存在即代表至少一侧仍连接着。
func (m *Manager) IsActive(roomID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.hubs[roomID]
	return ok
}

// GetIfExists 仅在 hub 已存在时返回，不会像 GetOrCreate 一样创建新的。
func (m *Manager) GetIfExists(roomID string) (*Hub, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hubs[roomID]
	return h, ok
}
