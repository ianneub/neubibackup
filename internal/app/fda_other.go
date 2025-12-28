//go:build !darwin

package app

// handleMacOSFirstRun is a no-op on non-macOS platforms.
func (a *App) handleMacOSFirstRun() {}
