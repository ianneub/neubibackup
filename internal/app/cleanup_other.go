//go:build !windows

package app

// cleanupOldUpdates is a no-op on non-Windows platforms.
func cleanupOldUpdates() {}

// cleanupOldAutostartShortcut is a no-op on non-Windows platforms.
func cleanupOldAutostartShortcut() {}
