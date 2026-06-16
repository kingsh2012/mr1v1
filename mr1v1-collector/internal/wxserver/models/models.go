package models

import "github.com/gorilla/websocket"

type Player struct {
	OpenID string
	Conn   *websocket.Conn
}

type MatchMessage struct {
	Type       string `json:"type"`
	ServerAddr string `json:"server_addr,omitempty"`
	RoomID     string `json:"room_id,omitempty"`
	Map        string `json:"map,omitempty"`
	Message    string `json:"message,omitempty"`
}
