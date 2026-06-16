package a2s

const (
	a2sRulesRequest  = 0x56
	a2sRulesResponse = 0x45
)

// RulesInfo is the result of an A2S_RULES query.
type RulesInfo struct {
	Count uint16            `json:"count"`
	Rules map[string]string `json:"rules"`
}

// QueryRules sends an A2S_RULES request and returns the server cvar map.
func (c *Client) QueryRules() (*RulesInfo, error) {
	data, immediate, err := c.getChallenge(a2sRulesRequest, a2sRulesResponse)
	if err != nil {
		return nil, err
	}
	if !immediate {
		if err := c.send([]byte{0xff, 0xff, 0xff, 0xff, a2sRulesRequest, data[0], data[1], data[2], data[3]}); err != nil {
			return nil, err
		}
		data, err = c.receive()
		if err != nil {
			return nil, err
		}
	}
	switch newReader(data).i32() {
	case -1:
		return parseRules(data)
	case -2:
		data, err = c.collectMulti(data)
		if err != nil {
			return nil, err
		}
		return parseRules(data)
	}
	return nil, ErrBadPacketHeader
}

func parseRules(data []byte) (*RulesInfo, error) {
	r := newReader(data)
	if r.i32() != -1 {
		return nil, ErrBadPacketHeader
	}
	if r.u8() != a2sRulesResponse {
		return nil, ErrBadRulesReply
	}
	info := &RulesInfo{}
	info.Count = r.u16()
	info.Rules = make(map[string]string, info.Count)
	for i := 0; i < int(info.Count); i++ {
		k, ok := r.tryStr()
		if !ok {
			break
		}
		v, ok := r.tryStr()
		if !ok {
			break
		}
		info.Rules[k] = v
	}
	return info, nil
}
