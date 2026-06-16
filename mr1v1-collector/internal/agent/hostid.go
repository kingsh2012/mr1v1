package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadOrCreateHostID reads the persisted host id from path, or generates a
// new random one and writes it back if the file does not exist. This keeps
// an agent's identity stable across restarts.
func loadOrCreateHostID(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read host id file %s: %w", path, err)
	}

	id, err := randomHostID()
	if err != nil {
		return "", err
	}

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create host id dir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(id), 0o644); err != nil {
		return "", fmt.Errorf("write host id file %s: %w", path, err)
	}
	return id, nil
}

func randomHostID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate host id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
