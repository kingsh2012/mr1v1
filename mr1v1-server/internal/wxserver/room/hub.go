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
	Type           string `json:"type"`
	Content        string `json:"content,omitempty"`
	Role           string `json:"role,omitempty"`
	Name           string `json:"name,omitempty"`
	Avatar         string `json:"avatar,omitempty"`
	ServerAddr     string `json:"server_addr,omitempty"`
	MatchID        string `json:"match_id,omitempty"`
	Message        string `json:"message,omitempty"`
	// 仅"state"事件使用：(re)连接时把Hub内存里的当前状态告知刚连上的客户端，
	// 弥补WS协议本身只推送"变化"、新连接/重连进来时两眼一抹黑的问题
	OtherOnline    bool   `json:"other_online,omitempty"`
	OtherConfirmed bool   `json:"other_confirmed,omitempty"`
	MyConfirmed    bool   `json:"my_confirmed,omitempty"`
}

type slot struct {
	openid    string
	name      string
	avatar    string
	steamID   string
	role      string // "creator" | "joiner"
	confirmed bool
	conn      *websocket.Conn
}

type Hub struct {
	mu             sync.Mutex
	roomID         string
	backendURL     string
	internalAPIKey string
	store          *store.Store
	slots          [2]*slot // 0=creator, 1=joiner
	history        []Event  // chat history
	onEmpty        func()   // called when both slots are disconnected
	matched        bool     // 本房间是否已经成功建服(供重连客户端补发matched事件)
	matchID        string
	serverAddr     string
}

func newHub(roomID, backendURL, internalAPIKey string, s *store.Store, onEmpty func()) *Hub {
	return &Hub{
		roomID:         roomID,
		backendURL:     backendURL,
		internalAPIKey: internalAPIKey,
		store:          s,
		onEmpty:        onEmpty,
	}
}

// Connect attaches a WebSocket connection to the hub.
// role must be "creator" or "joiner".
func (h *Hub) Connect(conn *websocket.Conn, openid, name, avatar, steamID, role string) {
	h.mu.Lock()
	idx := 0
	if role == "joiner" {
		idx = 1
	}
	h.slots[idx] = &slot{openid: openid, name: name, avatar: avatar, steamID: steamID, role: role, conn: conn}
	other := h.slots[1-idx]
	history := append([]Event(nil), h.history...)
	matched, matchID, serverAddr := h.matched, h.matchID, h.serverAddr
	h.mu.Unlock()

	// send chat history to reconnecting player
	if len(history) > 0 {
		h.send(conn, Event{Type: "history", Content: func() string {
			data, _ := json.Marshal(history)
			return string(data)
		}()})
	}

	// (重)连接时把当前已知状态告知这个客户端本身——WS协议本身只推送"变化"，
	// 新连接/掉线重连进来时两眼一抹黑，靠这条把对手在线/确认状态补回去
	h.send(conn, Event{
		Type:           "state",
		OtherOnline:    other != nil,
		OtherConfirmed: other != nil && other.confirmed,
		MyConfirmed:    false, // 每次(re)连接都会创建新slot，确认状态需要重新确认一次，这里显式回传false避免前端误用旧值
	})

	// 如果重连发生在比赛已经建好之后(原来的matched广播错过了)，直接补发一次
	if matched {
		h.send(conn, Event{Type: "matched", ServerAddr: serverAddr, MatchID: matchID})
	}

	// notify the other player that someone joined/is-present
	if other != nil && role == "joiner" {
		h.send(other.conn, Event{Type: "player_joined", Name: name, Avatar: avatar, Role: "joiner"})
	}

	// read loop
	explicitClose := false
readLoop:
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
			e := Event{Type: "chat", Role: role, Name: name, Content: msg.Content}
			h.mu.Lock()
			h.history = append(h.history, e)
			h.mu.Unlock()
			h.broadcast(e)
		case "confirm":
			h.handleConfirm(idx, openid, name, steamID, role)
		case "cancel_confirm":
			h.handleCancelConfirm(idx, role, name)
		case "close_room":
			if role == "creator" {
				explicitClose = true
				break readLoop
			}
		}
	}

	// disconnected（主动关闭或断线）
	h.mu.Lock()
	h.slots[idx] = nil
	remaining := h.slots[1-idx]
	h.mu.Unlock()

	h.mu.Lock()
	alreadyMatched := h.matched
	h.mu.Unlock()

	switch {
	case alreadyMatched:
		// 比赛已经建好之后的断开（无论房主/joiner）：双方本来就该去连游戏服务器了，
		// 不做任何房间状态变更——尤其不能把joiner清空/把房间打回waiting，
		// 否则会跟"已经matched"的事实自相矛盾，且match_ended会负责后续的真正收尾
	case role == "creator" && explicitClose:
		// 房主主动点击"关闭房间" → 软删除并踢出对手
		_ = h.store.DeleteRoom(context.Background(), h.roomID)
		if remaining != nil {
			h.send(remaining.conn, Event{Type: "room_closed", Message: "房主已关闭房间"})
			remaining.conn.Close()
		}
	case role == "creator":
		// 房主仅断线（切页/切后台/网络抖动），不销毁房间，房主可重新连接
		if remaining != nil {
			h.send(remaining.conn, Event{Type: "player_left", Role: "creator", Name: name})
		}
	default:
		// joiner 离开（主动或断线一视同仁，且尚未matched）→ 清空 joiner，房间回到 waiting
		_ = h.store.LeaveRoom(context.Background(), h.roomID, openid)
		if remaining != nil {
			h.send(remaining.conn, Event{Type: "player_left", Role: "joiner", Name: name})
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

	h.mu.Lock()
	myAvatar := ""
	if h.slots[idx] != nil {
		myAvatar = h.slots[idx].avatar
	}
	h.mu.Unlock()
	h.broadcast(Event{Type: "confirmed", Role: role, Name: name, Avatar: myAvatar})

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

	h.mu.Lock()
	h.matched = true
	h.matchID = matchID
	h.serverAddr = serverAddr
	h.mu.Unlock()

	_ = h.store.SetRoomMatched(context.Background(), h.roomID, matchID, serverAddr)
	h.broadcast(Event{Type: "matched", ServerAddr: serverAddr, MatchID: matchID})
}

// CloseByCreator 由 REST 接口触发（创建者在房间列表页直接销毁自己的房间，
// 不一定正连着该房间的 WS）。踢出对手并断开双方现有连接。
func (h *Hub) CloseByCreator() {
	h.mu.Lock()
	creator := h.slots[0]
	joiner := h.slots[1]
	h.slots[0] = nil
	h.slots[1] = nil
	h.mu.Unlock()

	if joiner != nil {
		h.send(joiner.conn, Event{Type: "room_closed", Message: "房主已关闭房间"})
		joiner.conn.Close()
	}
	if creator != nil {
		creator.conn.Close()
	}
	if h.onEmpty != nil {
		h.onEmpty()
	}
}

// NotifyMatchEnded 由 manager-backend/consumer 同步通知触发（比赛被手动销毁/
// 超时/异常停止/正常完赛），告知仍停留在房间页的玩家，并断开双方连接。
func (h *Hub) NotifyMatchEnded(message string) {
	h.mu.Lock()
	s0, s1 := h.slots[0], h.slots[1]
	h.slots[0] = nil
	h.slots[1] = nil
	h.mu.Unlock()

	e := Event{Type: "match_ended", Message: message}
	if s0 != nil {
		h.send(s0.conn, e)
		s0.conn.Close()
	}
	if s1 != nil {
		h.send(s1.conn, e)
		s1.conn.Close()
	}
	if h.onEmpty != nil {
		h.onEmpty()
	}
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

// manager-backend 的所有响应统一包了一层 {code, data}（见 internal/resp），
// 这里是服务间调用，需要按同样的格式解包才能拿到真实数据。
type createMatchEnvelope struct {
	Code int              `json:"code"`
	Data createMatchResp  `json:"data"`
}

func (h *Hub) createMatch(p0SteamID, p1SteamID string) (matchID, serverAddr string, err error) {
	body, _ := json.Marshal(map[string]string{
		"p0_steamid": p0SteamID,
		"p1_steamid": p1SteamID,
	})
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodPost, h.backendURL+"/api/manager/matches", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if h.internalAPIKey != "" {
		req.Header.Set("X-API-Key", h.internalAPIKey)
	}
	resp, err := client.Do(req) //nolint:gosec
	if err != nil {
		return "", "", fmt.Errorf("call backend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("backend %d: %s", resp.StatusCode, string(raw))
	}
	var env createMatchEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return "", "", err
	}
	r := env.Data
	return r.MatchID, fmt.Sprintf("%s:%d", r.PublicIP, r.Port), nil
}
