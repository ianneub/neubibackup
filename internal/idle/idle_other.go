//go:build !darwin && !windows

package idle

import (
	"log/slog"
	"time"
)

// getIdleTime returns 0 on unsupported platforms.
// This is a fail-open behavior: assume user is active so backups can proceed.
func getIdleTime() time.Duration {
	slog.Debug("Idle time detection not supported on this platform, returning 0")
	return 0
}
