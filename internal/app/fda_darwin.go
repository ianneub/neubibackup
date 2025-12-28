//go:build darwin

package app

import (
	"log"

	"neubibackup/internal/config"
)

// handleMacOSFirstRun prompts for Full Disk Access if not already granted.
func (a *App) handleMacOSFirstRun() {
	if config.HasFullDiskAccess() {
		log.Println("Full Disk Access already granted")
		return
	}

	log.Println("Full Disk Access not granted - opening System Settings...")
	if err := config.OpenFullDiskAccessSettings(); err != nil {
		log.Printf("Warning: could not open Full Disk Access settings: %v", err)
	}
}
