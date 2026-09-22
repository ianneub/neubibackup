//go:build linux

package keychain

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrPromptCancelled is returned when the user dismisses the dialog
// without entering a password (Cancel button or empty input).
var ErrPromptCancelled = errors.New("keychain: prompt cancelled")

// runZenity invokes zenity with the provided arguments and returns stdout.
// Factored into a package-level var so tests can stub the system call
// instead of opening a real GUI dialog.
var runZenity = func(args ...string) ([]byte, error) {
	return exec.Command("zenity", args...).Output()
}

// PromptDialog shows a password dialog via zenity and returns what the user
// typed. Returns ErrPromptCancelled if the user cancels or supplies an empty
// value, and wraps ErrUnsupported if zenity isn't installed.
//
// title: dialog window title (kept short)
// message: prompt text shown above the input field
func PromptDialog(title, message string) (string, error) {
	// --entry --hide-text rather than --password: zenity's password dialog
	// can't show custom text. Arguments go straight to exec, so no escaping.
	out, err := runZenity("--entry", "--hide-text", "--title="+title, "--text="+message)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("zenity is required for the password dialog: %w", ErrUnsupported)
		}
		// Cancel/close exits 1; treat any other failure the same way.
		return "", ErrPromptCancelled
	}

	// zenity prints the entry followed by a newline. Trim only that, so
	// passwords with leading/trailing spaces survive intact.
	pw := strings.TrimSuffix(strings.TrimSuffix(string(out), "\n"), "\r")
	if pw == "" {
		return "", ErrPromptCancelled
	}
	return pw, nil
}
