//go:build darwin

package app

import (
	"os"

	"neubibackup/internal/updater"
)

// cleanupOldUpdates sweeps stale .app.{old,new} siblings left over from a
// previous self-update that exited before its best-effort cleanup ran.
func cleanupOldUpdates() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	updater.CleanupStaleBundles(exe)
}

// cleanupOldAutostartShortcut is a no-op on macOS.
func cleanupOldAutostartShortcut() {}
