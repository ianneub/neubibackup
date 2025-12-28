//go:build windows

package config

import (
	"os"
	"os/exec"
	"path/filepath"
)

// OpenInEditor opens the given file in the default application.
// On Windows, we use rundll32 with the URL file protocol handler
// to open files with their associated default application.
func OpenInEditor(path string) error {
	rundll32 := filepath.Join(os.Getenv("SYSTEMROOT"), "System32", "rundll32.exe")
	cmd := exec.Command(rundll32, "url.dll,FileProtocolHandler", path)
	return cmd.Start()
}

// OpenFolder opens the given folder in Explorer.
func OpenFolder(path string) error {
	cmd := exec.Command("explorer", path)
	return cmd.Start()
}
