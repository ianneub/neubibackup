//go:build darwin

// Package updater provides automatic update checking and installation.
package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Restart relaunches the application after an update.
// On macOS, we use "open -n" to launch the .app bundle, then exit.
func Restart() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	// For .app bundles, executable is inside Contents/MacOS
	// We need to find the .app path and use "open" to launch it
	// Example: /Applications/NeubiBackup.app/Contents/MacOS/neubibackup
	appPath := findAppBundle(executable)
	if appPath != "" {
		// Launch the .app bundle asynchronously with -n (new instance)
		cmd := exec.Command("open", "-n", appPath)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("launching app bundle: %w", err)
		}
		// Exit current process
		os.Exit(0)
	}

	// Fallback: if not in an .app bundle, just restart the executable directly
	cmd := exec.Command(executable)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching executable: %w", err)
	}
	os.Exit(0)

	return nil // Never reached
}

// findAppBundle walks up the path looking for a .app bundle.
// Returns empty string if not found.
func findAppBundle(execPath string) string {
	// Walk up the path looking for .app
	// /Applications/NeubiBackup.app/Contents/MacOS/neubibackup
	//              ^-- we want this
	dir := execPath
	for i := 0; i < 4; i++ {
		dir = filepath.Dir(dir)
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
	}
	return ""
}
