package models

import "github.com/gorilla/websocket"

type Player struct {
	OpenID  string
	SteamID string
	Conn    *websocket.Conn
}

type MatchMessage struct {
	Type       string `json:"type"`
	MatchID    string `json:"match_id,omitempty"`
	ServerAddr string `json:"server_addr,omitempty"` // host:port，供小程序连接 CS 服务器
	Message    string `json:"message,omitempty"`
}
