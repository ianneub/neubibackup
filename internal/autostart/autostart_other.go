//go:build !windows

// Package autostart manages launching the application at system login.
package autostart

import (
	"os"
	"path/filepath"

	"github.com/emersion/go-autostart"
)

// Manager handles autostart configuration.
type Manager struct {
	app *autostart.App
}

// New creates a new autostart manager.
func New() (*Manager, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}

	// Resolve symlinks to get the real path
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, err
	}

	app := &autostart.App{
		Name:        "NeubiBackup",
		DisplayName: "NeubiBackup",
		Exec:        []string{executable},
	}

	return &Manager{app: app}, nil
}

// IsEnabled returns true if autostart is enabled.
func (m *Manager) IsEnabled() bool {
	return m.app.IsEnabled()
}

// Enable enables autostart.
func (m *Manager) Enable() error {
	return m.app.Enable()
}

// Disable disables autostart.
func (m *Manager) Disable() error {
	return m.app.Disable()
}

// Toggle toggles the autostart state.
func (m *Manager) Toggle() error {
	if m.IsEnabled() {
		return m.Disable()
	}
	return m.Enable()
}
