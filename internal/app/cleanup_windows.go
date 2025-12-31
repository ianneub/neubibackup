//go:build windows

package app

import (
	"log/slog"
	"os"
	"path/filepath"
)

// cleanupOldUpdates removes old update artifacts left by go-selfupdate on Windows.
// On Windows, the old executable is renamed to .old rather than deleted.
func cleanupOldUpdates() {
	exe, err := os.Executable()
	if err != nil {
		return
	}

	dir := filepath.Dir(exe)
	pattern := filepath.Join(dir, ".*.old")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}

	for _, old := range matches {
		if err := os.Remove(old); err != nil {
			// File might still be locked, that's OK - we'll try again next time
			slog.Warn("Could not remove old update file", "file", old, "error", err)
		} else {
			slog.Info("Removed old update file", "file", old)
		}
	}
}

// cleanupOldAutostartShortcut removes the old Startup folder shortcut on Windows.
// Previous versions used go-autostart which creates a .lnk file in the Startup folder,
// but that doesn't work with apps requiring admin privileges. We now use Task Scheduler.
func cleanupOldAutostartShortcut() {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return
	}

	shortcutPath := filepath.Join(appData,
		"Microsoft", "Windows", "Start Menu", "Programs", "Startup",
		"NeubiBackup.lnk")

	if _, err := os.Stat(shortcutPath); err == nil {
		if err := os.Remove(shortcutPath); err != nil {
			slog.Warn("Could not remove old autostart shortcut", "error", err)
		} else {
			slog.Info("Removed old autostart shortcut from Startup folder")
		}
	}
}
