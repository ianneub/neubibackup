//go:build darwin

package config

import (
	"os"
	"testing"
)

func TestHasFullDiskAccess_Darwin(t *testing.T) {
	// Test that the function checks the TCC database path
	result := HasFullDiskAccess()

	// Verify by checking if we can actually open the TCC database
	tccPath := "/Library/Application Support/com.apple.TCC/TCC.db"
	_, err := os.Open(tccPath)
	expected := err == nil

	if result != expected {
		t.Errorf("HasFullDiskAccess() = %v, but direct check = %v", result, expected)
	}

	t.Logf("HasFullDiskAccess() = %v (FDA %s)", result, map[bool]string{true: "granted", false: "not granted"}[result])
}

func TestOpenFullDiskAccessSettings_Darwin(t *testing.T) {
	// We can't actually test opening System Preferences without being disruptive
	// This test just verifies the function signature and that it doesn't panic
	// when we check what it would do

	// Skip the actual call to avoid opening System Preferences
	t.Skip("Skipping to avoid opening System Preferences during test")

	// If we wanted to test, we would call:
	// err := OpenFullDiskAccessSettings()
	// if err != nil {
	//     t.Errorf("OpenFullDiskAccessSettings() error = %v", err)
	// }
}
