//go:build !darwin && !windows

package idle

import "time"

// getIdleTime returns 0 on unsupported platforms.
// This is a fail-open behavior: assume user is active so backups can proceed.
func getIdleTime() time.Duration {
	return 0
}
