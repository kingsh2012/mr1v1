package room

import (
	"sync"

	"mr1v1-server/internal/wxserver/store"
)

type Manager struct {
	mu         sync.Mutex
	hubs       map[string]*Hub
	backendURL string
	store      *store.Store
}

func NewManager(backendURL string, s *store.Store) *Manager {
	return &Manager{
		hubs:       make(map[string]*Hub),
		backendURL: backendURL,
		store:      s,
	}
}

func (m *Manager) GetOrCreate(roomID string) *Hub {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.hubs[roomID]; ok {
		return h
	}
	h := newHub(roomID, m.backendURL, m.store, func() {
		m.mu.Lock()
		delete(m.hubs, roomID)
		m.mu.Unlock()
	})
	m.hubs[roomID] = h
	return h
}
