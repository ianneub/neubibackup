//go:build darwin

package app

import (
	"log/slog"

	"neubibackup/internal/config"
)

// handleMacOSFirstRun prompts for Full Disk Access if not already granted.
func (a *App) handleMacOSFirstRun() {
	if config.HasFullDiskAccess() {
		slog.Info("Full Disk Access already granted")
		return
	}

	slog.Info("Full Disk Access not granted - opening System Settings...")
	if err := config.OpenFullDiskAccessSettings(); err != nil {
		slog.Warn("Could not open Full Disk Access settings", "error", err)
	}
}
