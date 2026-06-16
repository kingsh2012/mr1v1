package a2s

const (
	a2sPlayerRequest  = 0x55
	a2sPlayerResponse = 0x44
)

// Player holds per-player data from an A2S_PLAYER query.
type Player struct {
	Index    uint8   `json:"index"`
	Name     string  `json:"name"`
	Score    uint32  `json:"score"`
	Duration float32 `json:"duration"`
}

// PlayerInfo is the result of an A2S_PLAYER query.
type PlayerInfo struct {
	Count   uint8     `json:"count"`
	Players []*Player `json:"players"`
}

// QueryPlayers sends an A2S_PLAYER request and returns the player list.
func (c *Client) QueryPlayers() (*PlayerInfo, error) {
	data, immediate, err := c.getChallenge(a2sPlayerRequest, a2sPlayerResponse)
	if err != nil {
		return nil, err
	}
	if !immediate {
		if err := c.send([]byte{0xff, 0xff, 0xff, 0xff, a2sPlayerRequest, data[0], data[1], data[2], data[3]}); err != nil {
			return nil, err
		}
		data, err = c.receive()
		if err != nil {
			return nil, err
		}
	}
	switch newReader(data).i32() {
	case -1:
		return parsePlayers(data)
	case -2:
		data, err = c.collectMulti(data)
		if err != nil {
			return nil, err
		}
		return parsePlayers(data)
	}
	return nil, ErrBadPacketHeader
}

func parsePlayers(data []byte) (*PlayerInfo, error) {
	r := newReader(data)
	if r.i32() != -1 {
		return nil, ErrBadPacketHeader
	}
	if r.u8() != a2sPlayerResponse {
		return nil, ErrBadPlayerReply
	}
	info := &PlayerInfo{}
	info.Count = r.u8()
	for i := 0; i < int(info.Count); i++ {
		p := &Player{}
		p.Index = r.u8()
		p.Name = r.str()
		p.Score = r.u32()
		p.Duration = r.f32()
		info.Players = append(info.Players, p)
	}
	return info, nil
}
