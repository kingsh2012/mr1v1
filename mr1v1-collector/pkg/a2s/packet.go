package a2s

import (
	"bytes"
	"compress/bzip2"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
)

var (
	ErrBadPacketHeader      = errors.New("packet header mismatch")
	ErrUnsupportedHeader    = errors.New("unsupported protocol header")
	ErrBadChallengeResponse = errors.New("bad challenge response")
	ErrBadPlayerReply       = errors.New("bad player reply")
	ErrBadRulesReply        = errors.New("bad rules reply")
	ErrPacketOutOfBound     = errors.New("packet out of bound")
	ErrDuplicatePacket      = errors.New("received duplicate packet")
	ErrWrongBz2Size         = errors.New("bad bz2 decompression size")
	ErrMismatchBz2Checksum  = errors.New("bz2 checksum mismatch")
)

// PacketBuilder wraps bytes.Buffer with CString helper.
type PacketBuilder struct{ bytes.Buffer }

func (b *PacketBuilder) WriteCString(s string) { b.WriteString(s); b.WriteByte(0) }
func (b *PacketBuilder) WriteBytes(p []byte)   { b.Write(p) }

// PacketReader reads binary fields from a raw UDP payload.
type PacketReader struct {
	buf []byte
	pos int
}

func newReader(b []byte) *PacketReader { return &PacketReader{buf: b} }

func (r *PacketReader) more() bool    { return r.pos < len(r.buf) }
func (r *PacketReader) pos_() int     { return r.pos }

func (r *PacketReader) u8() uint8  { b := r.buf[r.pos]; r.pos++; return b }
func (r *PacketReader) i32() int32 { return int32(r.u32()) }
func (r *PacketReader) u16() uint16 {
	v := binary.LittleEndian.Uint16(r.buf[r.pos:])
	r.pos += 2
	return v
}
func (r *PacketReader) u32() uint32 {
	v := binary.LittleEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return v
}
func (r *PacketReader) u64() uint64 {
	v := binary.LittleEndian.Uint64(r.buf[r.pos:])
	r.pos += 8
	return v
}
func (r *PacketReader) f32() float32 { return math.Float32frombits(r.u32()) }
func (r *PacketReader) str() string {
	start := r.pos
	for r.buf[r.pos] != 0 {
		r.pos++
	}
	s := string(r.buf[start:r.pos])
	r.pos++
	return s
}
func (r *PacketReader) tryStr() (string, bool) {
	start := r.pos
	for r.pos < len(r.buf) {
		if r.buf[r.pos] == 0 {
			s := string(r.buf[start:r.pos])
			r.pos++
			return s, true
		}
		r.pos++
	}
	return "", false
}

// multi-packet reassembly

const challengeReplyHeader = 0x41

func (c *Client) getChallenge(reqHeader, fullHeader byte) ([]byte, bool, error) {
	if err := c.send([]byte{0xff, 0xff, 0xff, 0xff, reqHeader, 0xff, 0xff, 0xff, 0xff}); err != nil {
		return nil, false, err
	}
	data, err := c.receive()
	if err != nil {
		return nil, false, err
	}
	r := newReader(data)
	switch r.i32() {
	case -2:
		return data, true, nil
	case -1:
	default:
		return nil, false, ErrBadPacketHeader
	}
	switch r.u8() {
	case challengeReplyHeader:
		pos := r.pos_()
		return data[pos : pos+4], false, nil
	case fullHeader:
		return data, true, nil
	}
	return nil, false, ErrBadChallengeResponse
}

func (c *Client) collectMulti(data []byte) ([]byte, error) {
	type mpkt struct {
		id         uint32
		total      uint8
		number     uint8
		compressed bool
		payload    []byte
	}
	parse := func(d []byte) (*mpkt, error) {
		r := newReader(d)
		if r.i32() != -2 {
			return nil, ErrBadPacketHeader
		}
		p := &mpkt{}
		p.id = r.u32()
		if c.goldsrc {
			// GoldSrc: total and number packed into one byte as (number<<4)|total
			packed := r.u8()
			p.total = packed & 0x0F
			p.number = packed >> 4
		} else {
			p.compressed = (p.id & 0x80000000) != 0
			p.total = r.u8()
			p.number = r.u8()
			r.u16() // SplitSize (Source Engine only)
		}
		p.payload = d[r.pos_():]
		return p, nil
	}

	first, err := parse(data)
	if err != nil {
		return nil, err
	}
	pkts := make([]*mpkt, first.total)
	pkts[first.number] = first
	received := 1
	for received < int(first.total) {
		d, err := c.receive()
		if err != nil {
			return nil, err
		}
		p, err := parse(d)
		if err != nil {
			return nil, err
		}
		if int(p.number) >= len(pkts) {
			return nil, ErrPacketOutOfBound
		}
		if pkts[p.number] != nil {
			return nil, ErrDuplicatePacket
		}
		pkts[p.number] = p
		received++
	}

	var full []byte
	for _, p := range pkts {
		full = append(full, p.payload...)
	}

	if first.compressed {
		r := newReader(full)
		decompSize := r.u32()
		checkSum := r.u32()
		if decompSize > 1024*1024 {
			return nil, ErrWrongBz2Size
		}
		decompressed := make([]byte, decompSize)
		n, err := bzip2.NewReader(bytes.NewReader(full[r.pos_():])).Read(decompressed)
		if err != nil {
			return nil, err
		}
		if uint32(n) != decompSize {
			return nil, ErrWrongBz2Size
		}
		if crc32.ChecksumIEEE(decompressed) != checkSum {
			return nil, ErrMismatchBz2Checksum
		}
		return decompressed, nil
	}
	return full, nil
}
