//go:build darwin

package config

import (
	"os/exec"
)

// OpenInEditor opens the given file in the default text editor.
// On macOS, this uses the "open" command which respects the user's default app.
func OpenInEditor(path string) error {
	cmd := exec.Command("open", path)
	return cmd.Start()
}

// OpenFolder opens the given folder in Finder.
func OpenFolder(path string) error {
	cmd := exec.Command("open", path)
	return cmd.Start()
}

// OpenURL opens the given URL in the default browser.
func OpenURL(url string) error {
	cmd := exec.Command("open", url)
	return cmd.Start()
}
