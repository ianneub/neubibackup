//go:build windows

// Package autostart manages launching the application at system login.
package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const taskName = "NeubiBackup"

// createNoWindow prevents cmd windows from flashing during schtasks execution
const createNoWindow = 0x08000000

// Manager handles autostart configuration using Windows Task Scheduler.
type Manager struct {
	executable string
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

	return &Manager{executable: executable}, nil
}

// IsEnabled returns true if autostart is enabled.
func (m *Manager) IsEnabled() bool {
	cmd := exec.Command("schtasks", "/Query", "/TN", taskName)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	err := cmd.Run()
	return err == nil
}

// Enable enables autostart by creating a scheduled task.
func (m *Manager) Enable() error {
	// First remove any existing task to ensure clean state
	m.Disable()

	// Create task with elevated privileges on user logon
	// /SC ONLOGON - trigger at logon
	// /RL HIGHEST - run with highest privileges (admin)
	// /IT - run only when user is logged on interactively
	// /F - force creation (overwrite if exists)
	cmd := exec.Command("schtasks",
		"/Create",
		"/TN", taskName,
		"/TR", fmt.Sprintf(`"%s"`, m.executable),
		"/SC", "ONLOGON",
		"/RL", "HIGHEST",
		"/IT",
		"/F",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create scheduled task: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// Disable disables autostart by removing the scheduled task.
func (m *Manager) Disable() error {
	cmd := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	// Ignore errors - task may not exist
	cmd.Run()
	return nil
}

// Toggle toggles the autostart state.
func (m *Manager) Toggle() error {
	if m.IsEnabled() {
		return m.Disable()
	}
	return m.Enable()
}
