//go:build darwin

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

// runOsascript invokes osascript with the provided AppleScript source.
// Factored into a package-level var so tests can stub the system call
// instead of firing a real GUI dialog.
var runOsascript = func(script string) ([]byte, error) {
	return exec.Command("osascript", "-e", script).Output()
}

// PromptDialog shows a native password dialog and returns what the user
// typed. Returns ErrPromptCancelled if the user cancels or supplies an
// empty value.
//
// title: dialog window title (kept short)
// message: prompt text shown above the input field
func PromptDialog(title, message string) (string, error) {
	// AppleScript escapes: replace " with \" inside our literals.
	escTitle := strings.ReplaceAll(title, `"`, `\"`)
	escMsg := strings.ReplaceAll(message, `"`, `\"`)

	script := fmt.Sprintf(
		`display dialog "%s" with title "%s" default answer "" with hidden answer buttons {"Cancel","OK"} default button "OK"`,
		escMsg, escTitle,
	)

	out, err := runOsascript(script)
	if err != nil {
		// User clicking Cancel → osascript exits non-zero with
		// "User canceled. (-128)" on stderr. Treat any failure as cancel
		// (we don't want to leak osascript internals to the user).
		return "", ErrPromptCancelled
	}

	// osascript output looks like:
	//   button returned:OK, text returned:thepassword
	s := strings.TrimRight(string(out), "\r\n")
	const marker = "text returned:"
	idx := strings.Index(s, marker)
	if idx < 0 {
		return "", ErrPromptCancelled
	}
	pw := s[idx+len(marker):]
	if pw == "" {
		return "", ErrPromptCancelled
	}
	return pw, nil
}
