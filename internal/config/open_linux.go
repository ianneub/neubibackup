//go:build linux

package config

import (
	"os/exec"
)

// omarchyEditor is Omarchy's launcher for the user's chosen editor. It opens
// terminal editors (the default, nvim) in a terminal window.
const omarchyEditor = "omarchy-launch-editor"

// lookPath is exec.LookPath, swappable in tests.
var lookPath = exec.LookPath

// editorCommand returns the command used to open path for editing.
// On Omarchy this is the user's configured editor; elsewhere it falls back to
// xdg-open, which picks the default application for the file's MIME type.
func editorCommand(path string) []string {
	if _, err := lookPath(omarchyEditor); err == nil {
		return []string{omarchyEditor, path}
	}
	return []string{"xdg-open", path}
}

// OpenInEditor opens the given file in the user's editor.
func OpenInEditor(path string) error {
	return start(editorCommand(path))
}

// OpenFolder opens the given folder in the default file manager.
func OpenFolder(path string) error {
	return start([]string{"xdg-open", path})
}

// OpenURL opens the given URL in the default browser.
func OpenURL(url string) error {
	return start([]string{"xdg-open", url})
}

// start launches argv without waiting for it, reaping it in the background
// so the finished launcher doesn't linger as a zombie.
func start(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
