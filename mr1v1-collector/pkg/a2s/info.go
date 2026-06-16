package a2s

const a2sInfoHeader = 0x49

// ServerInfo contains the result of an A2S_INFO query.
type ServerInfo struct {
	Protocol   uint8      `json:"protocol"`
	Name       string     `json:"name"`
	Map        string     `json:"map"`
	Folder     string     `json:"folder"`
	Game       string     `json:"game"`
	AppID      uint16     `json:"app_id"`
	Players    uint8      `json:"players"`
	MaxPlayers uint8      `json:"max_players"`
	Bots       uint8      `json:"bots"`
	ServerType ServerType `json:"server_type"`
	ServerOS   ServerOS   `json:"server_os"`
	Visibility bool       `json:"visibility"`
	VAC        bool       `json:"vac"`
	Version    string     `json:"version"`
}

// QueryInfo sends an A2S_INFO request and returns server information.
func (c *Client) QueryInfo() (*ServerInfo, error) {
	var b PacketBuilder
	b.WriteBytes([]byte{0xff, 0xff, 0xff, 0xff, 0x54})
	b.WriteCString("Source Engine Query")
	if err := c.send(b.Bytes()); err != nil {
		return nil, err
	}
	data, err := c.receive()
	if err != nil {
		return nil, err
	}
	r := newReader(data)
	if r.i32() != -1 {
		return nil, ErrBadPacketHeader
	}
	if r.u8() != a2sInfoHeader {
		return nil, ErrUnsupportedHeader
	}
	info := &ServerInfo{}
	info.Protocol = r.u8()
	info.Name = r.str()
	info.Map = r.str()
	info.Folder = r.str()
	info.Game = r.str()
	info.AppID = r.u16()
	info.Players = r.u8()
	info.MaxPlayers = r.u8()
	info.Bots = r.u8()
	info.ServerType = ParseServerType(r.u8())
	info.ServerOS = ParseServerOS(r.u8())
	info.Visibility = r.u8() == 1
	info.VAC = r.u8() == 1
	if r.more() {
		info.Version = r.str()
	}
	return info, nil
}
