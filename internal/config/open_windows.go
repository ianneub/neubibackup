//go:build windows

package config

import (
	"os/exec"
)

// OpenInEditor opens the given file in Notepad.
// On Windows, we use notepad for simplicity and universal availability.
func OpenInEditor(path string) error {
	cmd := exec.Command("notepad", path)
	return cmd.Start()
}

// OpenFolder opens the given folder in Explorer.
func OpenFolder(path string) error {
	cmd := exec.Command("explorer", path)
	return cmd.Start()
}
