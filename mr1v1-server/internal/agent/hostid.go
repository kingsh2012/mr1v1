package agent

import (
	"os"
	"strings"
)

// biosUUID 读取 BIOS UUID，优先从 /sys/class/dmi/id/product_uuid，
// fallback 到 /etc/machine-id。
func biosUUID() string {
	for _, path := range []string{
		"/sys/class/dmi/id/product_uuid",
		"/etc/machine-id",
	} {
		if data, err := os.ReadFile(path); err == nil {
			id := strings.TrimSpace(string(data))
			if id != "" {
				return id
			}
		}
	}
	return "unknown"
}
