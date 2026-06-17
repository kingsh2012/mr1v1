package room

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"mr1v1-server/internal/wxserver/store"
)

type Event struct {
	Type      string `json:"type"`
	Content   string `json:"content,omitempty"`
	Role      string `json:"role,omitempty"`
	Name      string `json:"name,omitempty"`
	ServerAddr string `json:"server_addr,omitempty"`
	MatchID   string `json:"match_id,omitempty"`
	Message   string `json:"message,omitempty"`
}

type slot struct {
	openid    string
	name      string
	steamID   string
	role      string // "creator" | "joiner"
	confirmed bool
	conn      *websocket.Conn
}

type Hub struct {
	mu         sync.Mutex
	roomID     string
	backendURL string
	store      *store.Store
	slots      [2]*slot // 0=creator, 1=joiner
	onEmpty    func()   // called when both slots are disconnected
}

func newHub(roomID, backendURL string, s *store.Store, onEmpty func()) *Hub {
	return &Hub{
		roomID:     roomID,
		backendURL: backendURL,
		store:      s,
		onEmpty:    onEmpty,
	}
}

// Connect attaches a WebSocket connection to the hub.
// role must be "creator" or "joiner".
func (h *Hub) Connect(conn *websocket.Conn, openid, name, steamID, role string) {
	h.mu.Lock()
	idx := 0
	if role == "joiner" {
		idx = 1
	}
	h.slots[idx] = &slot{openid: openid, name: name, steamID: steamID, role: role, conn: conn}
	other := h.slots[1-idx]
	h.mu.Unlock()

	// notify the other player that someone joined/is-present
	if other != nil && role == "joiner" {
		h.send(other.conn, Event{Type: "player_joined", Name: name, Role: "joiner"})
	}

	// read loop
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg Event
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "chat":
			h.broadcast(Event{Type: "chat", Role: role, Name: name, Content: msg.Content})
		case "confirm":
			h.handleConfirm(idx, openid, name, steamID, role)
		case "cancel_confirm":
			h.handleCancelConfirm(idx, role, name)
		}
	}

	// disconnected
	h.mu.Lock()
	h.slots[idx] = nil
	remaining := h.slots[1-idx]
	h.mu.Unlock()

	_ = h.store.LeaveRoom(context.Background(), h.roomID, openid)

	if remaining != nil {
		if role == "creator" {
			h.send(remaining.conn, Event{Type: "room_closed", Message: "房主已关闭房间"})
			remaining.conn.Close()
		} else {
			h.send(remaining.conn, Event{Type: "player_left", Role: "joiner"})
		}
	}

	h.mu.Lock()
	bothGone := h.slots[0] == nil && h.slots[1] == nil
	h.mu.Unlock()
	if bothGone && h.onEmpty != nil {
		h.onEmpty()
	}
}

func (h *Hub) handleConfirm(idx int, openid, name, steamID, role string) {
	h.mu.Lock()
	if h.slots[idx] != nil {
		h.slots[idx].confirmed = true
	}
	creator := h.slots[0]
	joiner := h.slots[1]
	h.mu.Unlock()

	h.broadcast(Event{Type: "confirmed", Role: role, Name: name})

	if creator != nil && joiner != nil && creator.confirmed && joiner.confirmed {
		h.triggerMatch(creator, joiner)
	}
}

func (h *Hub) handleCancelConfirm(idx int, role, name string) {
	h.mu.Lock()
	if h.slots[idx] != nil {
		h.slots[idx].confirmed = false
	}
	h.mu.Unlock()
	h.broadcast(Event{Type: "cancelled", Role: role, Name: name})
}

func (h *Hub) triggerMatch(creator, joiner *slot) {
	matchID, serverAddr, err := h.createMatch(creator.steamID, joiner.steamID)
	if err != nil {
		slog.Error("room create match failed", "room", h.roomID, "err", err)
		h.broadcast(Event{Type: "error", Message: "建服失败，请重试"})
		// reset confirms
		h.mu.Lock()
		if h.slots[0] != nil {
			h.slots[0].confirmed = false
		}
		if h.slots[1] != nil {
			h.slots[1].confirmed = false
		}
		h.mu.Unlock()
		return
	}

	_ = h.store.SetRoomMatched(context.Background(), h.roomID, matchID, serverAddr)
	h.broadcast(Event{Type: "matched", ServerAddr: serverAddr, MatchID: matchID})
}

func (h *Hub) broadcast(e Event) {
	h.mu.Lock()
	s0, s1 := h.slots[0], h.slots[1]
	h.mu.Unlock()
	if s0 != nil {
		h.send(s0.conn, e)
	}
	if s1 != nil {
		h.send(s1.conn, e)
	}
}

func (h *Hub) send(conn *websocket.Conn, e Event) {
	data, _ := json.Marshal(e)
	conn.WriteMessage(websocket.TextMessage, data)
}

type createMatchResp struct {
	MatchID  string `json:"match_id"`
	PublicIP string `json:"public_ip"`
	Port     int    `json:"port"`
}

func (h *Hub) createMatch(p0SteamID, p1SteamID string) (matchID, serverAddr string, err error) {
	body, _ := json.Marshal(map[string]string{
		"p0_steamid": p0SteamID,
		"p1_steamid": p1SteamID,
	})
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(h.backendURL+"/api/matches", "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		return "", "", fmt.Errorf("call backend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("backend %d: %s", resp.StatusCode, string(raw))
	}
	var r createMatchResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", err
	}
	return r.MatchID, fmt.Sprintf("%s:%d", r.PublicIP, r.Port), nil
}
