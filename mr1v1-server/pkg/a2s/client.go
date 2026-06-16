// Package a2s implements the Steam A2S server query protocol (UDP).
// Copied from PROCS.PRO-REHLDS-COLLECTION-SYSTEM/pkg/go-a2s.
package a2s

import (
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	DefaultTimeout = 3 * time.Second
	DefaultPort    = 27015
	MaxPacketSize  = 1400
)

// Client holds the UDP connection to a game server.
// GoldSrc (CS 1.6 / ReHLDS) uses a different multi-packet format:
// total+number are packed into a single byte as (number<<4)|total,
// and there is no SplitSize field. Set goldsrc=true for these servers.
type Client struct {
	conn    net.Conn
	timeout time.Duration
	goldsrc bool // true for GoldSrc / pre-Orange Box servers (e.g. CS 1.6)
}

// NewClient dials a game server. Set goldsrc=true for CS 1.6 / ReHLDS servers.
func NewClient(addr string, timeout time.Duration, goldsrc bool) (*Client, error) {
	if !strings.Contains(addr, ":") {
		addr = fmt.Sprintf("%s:%d", addr, DefaultPort)
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, timeout: timeout, goldsrc: goldsrc}, nil
}

func (c *Client) Close() { c.conn.Close() }

func (c *Client) send(data []byte) error {
	c.conn.SetWriteDeadline(time.Now().Add(c.timeout))
	_, err := c.conn.Write(data)
	return err
}

func (c *Client) receive() ([]byte, error) {
	c.conn.SetReadDeadline(time.Now().Add(c.timeout))
	buf := make([]byte, MaxPacketSize)
	n, err := c.conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}
