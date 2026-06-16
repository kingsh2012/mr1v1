package a2s

const (
	a2sInfoHeaderSource   = 0x49 // 'I' — Source Engine
	a2sInfoHeaderGoldsrc  = 0x6D // 'm' — GoldSrc / CS 1.6 / ReHLDS
)

// ServerInfo contains the result of an A2S_INFO query,
// normalised across both Source Engine and GoldSrc formats.
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
// Handles both the modern Source Engine format (header 'I') and the
// legacy GoldSrc format (header 'm') used by CS 1.6 / ReHLDS.
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
	switch r.u8() {
	case a2sInfoHeaderSource:
		return parseSourceInfo(r)
	case a2sInfoHeaderGoldsrc:
		return parseGoldsrcInfo(r)
	default:
		return nil, ErrUnsupportedHeader
	}
}

// parseSourceInfo parses the Source Engine A2S_INFO response (header already consumed).
func parseSourceInfo(r *PacketReader) (*ServerInfo, error) {
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

// parseGoldsrcInfo parses the legacy GoldSrc A2S_INFO response (header 'm' already consumed).
// Field order: Address, Name, Map, Folder, Game, Players, MaxPlayers,
//              Protocol, ServerType, OS, Password, IsMod, Secure, Bots
func parseGoldsrcInfo(r *PacketReader) (*ServerInfo, error) {
	info := &ServerInfo{}
	_ = r.str()               // Address (IP:port string, not needed)
	info.Name = r.str()
	info.Map = r.str()
	info.Folder = r.str()
	info.Game = r.str()
	info.Players = r.u8()
	info.MaxPlayers = r.u8()
	info.Protocol = r.u8()
	info.ServerType = ParseServerType(r.u8())
	info.ServerOS = ParseServerOS(r.u8())
	info.Visibility = r.u8() == 1
	isMod := r.u8()
	if isMod == 1 && r.more() {
		// Skip mod info fields: URL, NullURL, Version, Size, Type, DLL
		_ = r.str() // mod URL
		_ = r.str() // null URL
		if r.more() { r.u32() } // version
		if r.more() { r.u32() } // size
		if r.more() { r.u8()  } // type
		if r.more() { r.u8()  } // dll
	}
	if r.more() {
		info.VAC = r.u8() == 1
	}
	if r.more() {
		info.Bots = r.u8()
	}
	return info, nil
}
