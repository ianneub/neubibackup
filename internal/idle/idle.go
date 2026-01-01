// Package idle provides user activity detection by measuring time since last input.
package idle

import "time"

// GetIdleTime returns the duration since the last user input (keyboard/mouse).
// This is used to detect when a user has returned to their machine after being away.
//
// Platform behavior:
//   - macOS: Uses IOKit's HIDIdleTime property
//   - Windows: Uses GetLastInputInfo API
//   - Other platforms: Returns 0 (assume user is active, fail-open behavior)
//
// On error, returns 0 to assume user is active (fail-safe).
func GetIdleTime() time.Duration {
	return getIdleTime()
}
