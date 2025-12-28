package config

import (
	"runtime"
	"testing"
)

func TestHasFullDiskAccess(t *testing.T) {
	// This function should return a boolean without panicking
	result := HasFullDiskAccess()

	if runtime.GOOS != "darwin" {
		// On non-macOS platforms, should always return true
		if !result {
			t.Error("HasFullDiskAccess() = false on non-macOS, want true")
		}
	}
	// On macOS, we can't predict the result - it depends on actual permissions
	// Just verify it returns without error
	t.Logf("HasFullDiskAccess() = %v (GOOS=%s)", result, runtime.GOOS)
}

func TestOpenFullDiskAccessSettings(t *testing.T) {
	if runtime.GOOS != "darwin" {
		// On non-macOS platforms, should be a no-op returning nil
		err := OpenFullDiskAccessSettings()
		if err != nil {
			t.Errorf("OpenFullDiskAccessSettings() error = %v on non-macOS, want nil", err)
		}
		return
	}

	// On macOS, we skip actually opening System Preferences in tests
	// as it would be disruptive. Just verify the function exists and is callable.
	t.Skip("Skipping OpenFullDiskAccessSettings test on macOS to avoid opening System Preferences")
}
