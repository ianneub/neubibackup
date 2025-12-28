//go:build windows

package autostart

import (
	"testing"
)

func TestNew(t *testing.T) {
	m, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if m == nil {
		t.Fatal("New() returned nil")
	}
	if m.executable == "" {
		t.Error("executable path should not be empty")
	}
}

func TestTaskNameConstant(t *testing.T) {
	if taskName != "NeubiBackup" {
		t.Errorf("taskName = %q, want %q", taskName, "NeubiBackup")
	}
}

// Integration tests require admin privileges to run.
// Run with: go test -v ./internal/autostart
// Skip in CI or when running without admin: go test -short ./internal/autostart

func TestEnableDisable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	m, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Ensure clean state
	m.Disable()

	if m.IsEnabled() {
		t.Error("IsEnabled() should be false after Disable()")
	}

	if err := m.Enable(); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}

	if !m.IsEnabled() {
		t.Error("IsEnabled() should be true after Enable()")
	}

	// Clean up
	if err := m.Disable(); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}

	if m.IsEnabled() {
		t.Error("IsEnabled() should be false after final Disable()")
	}
}

func TestToggle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	m, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Ensure clean state
	m.Disable()
	initial := m.IsEnabled()

	if err := m.Toggle(); err != nil {
		t.Fatalf("Toggle() error = %v", err)
	}

	if m.IsEnabled() == initial {
		t.Error("Toggle() did not change state")
	}

	// Clean up
	m.Disable()
}
