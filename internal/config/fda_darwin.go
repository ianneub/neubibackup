//go:build darwin

package config

import (
	"os"
	"os/exec"
)

// HasFullDiskAccess checks if the app has Full Disk Access permission.
// It does this by attempting to read the TCC database, which requires FDA.
func HasFullDiskAccess() bool {
	// The TCC database requires Full Disk Access to read
	tccPath := "/Library/Application Support/com.apple.TCC/TCC.db"
	_, err := os.Open(tccPath)
	return err == nil
}

// OpenFullDiskAccessSettings opens System Settings to the Full Disk Access pane.
func OpenFullDiskAccessSettings() error {
	return exec.Command("open", "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles").Start()
}
