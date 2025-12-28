package tray

import "neubibackup/assets"

// IconState represents the priority-ordered icon states for the system tray.
type IconState int

const (
	// IconStateIdle indicates no backup has run yet.
	IconStateIdle IconState = iota
	// IconStateSuccess indicates the last backup succeeded.
	IconStateSuccess
	// IconStateError indicates a configuration issue or backup failure.
	IconStateError
	// IconStateRunning indicates a backup is in progress.
	IconStateRunning
)

// DetermineIconState returns the appropriate icon state based on app state.
// Priority order (highest to lowest):
//  1. Running - backup is in progress
//  2. Error (not configured) - config is missing/invalid
//  3. Error (failures) - consecutive failures > 0
//  4. Success - last backup succeeded
//  5. Idle - no backup has run yet
func DetermineIconState(isRunning, isConfigured bool, consecutiveFailures int, hasSuccessfulBackup bool) IconState {
	if isRunning {
		return IconStateRunning
	}
	if !isConfigured {
		return IconStateError
	}
	if consecutiveFailures > 0 {
		return IconStateError
	}
	if hasSuccessfulBackup {
		return IconStateSuccess
	}
	return IconStateIdle
}

// GetIconBytes returns the icon data for the given state.
func GetIconBytes(iconState IconState) []byte {
	switch iconState {
	case IconStateRunning:
		return assets.IconRunning
	case IconStateError:
		return assets.IconError
	case IconStateSuccess:
		return assets.IconSuccess
	default:
		return assets.IconIdle
	}
}
