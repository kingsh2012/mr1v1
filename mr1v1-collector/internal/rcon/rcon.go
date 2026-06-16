// Package rcon implements the GoldSrc (HLDS / CS 1.6) UDP RCON protocol:
// challenge request/response followed by a signed command, used by the
// agent to trigger the in-game match-destroy countdown before tearing down
// a container. This is NOT the newer Source-engine TCP RCON protocol.
package rcon

import (
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	packetPrefix   = "\xff\xff\xff\xff"
	defaultTimeout = 5 * time.Second
	readBufSize    = 4096
)

// Client talks to a single GoldSrc server's RCON UDP endpoint.
type Client struct {
	addr     string
	password string
	timeout  time.Duration
}

// New returns a client for the server listening on addr (host:port), using
// password as the rcon_password.
func New(addr, password string) *Client {
	return &Client{addr: addr, password: password, timeout: defaultTimeout}
}

// Execute requests an rcon challenge and then runs command, returning the
// server's response text.
func (c *Client) Execute(command string) (string, error) {
	conn, err := net.Dial("udp", c.addr)
	if err != nil {
		return "", fmt.Errorf("dial %s: %w", c.addr, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return "", err
	}

	challenge, err := c.requestChallenge(conn)
	if err != nil {
		return "", err
	}

	cmd := fmt.Sprintf("%srcon %s \"%s\" %s\n", packetPrefix, challenge, c.password, command)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return "", fmt.Errorf("send rcon command: %w", err)
	}

	buf := make([]byte, readBufSize)
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("read rcon response: %w", err)
	}

	return parseResponse(buf[:n]), nil
}

func (c *Client) requestChallenge(conn net.Conn) (string, error) {
	if _, err := conn.Write([]byte(packetPrefix + "challenge rcon\n")); err != nil {
		return "", fmt.Errorf("send challenge request: %w", err)
	}

	buf := make([]byte, readBufSize)
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("read challenge response: %w", err)
	}

	resp := parseResponse(buf[:n])
	const marker = "challenge rcon"
	idx := strings.Index(resp, marker)
	if idx == -1 {
		return "", fmt.Errorf("unexpected challenge response: %q", resp)
	}
	challenge := strings.TrimSpace(resp[idx+len(marker):])
	if challenge == "" {
		return "", fmt.Errorf("empty challenge in response: %q", resp)
	}
	return challenge, nil
}

// parseResponse strips the 4-byte 0xFFFFFFFF prefix, an optional leading
// 'l' (used by multi-packet console responses), and trailing NUL bytes.
func parseResponse(data []byte) string {
	s := string(data)
	s = strings.TrimPrefix(s, packetPrefix)
	s = strings.TrimPrefix(s, "l")
	return strings.Trim(s, "\x00")
}
