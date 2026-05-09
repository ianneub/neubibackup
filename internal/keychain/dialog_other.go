//go:build !darwin && !windows

package keychain

import "errors"

// ErrPromptCancelled is returned when the user dismisses the dialog. On
// stub platforms the dialog is unavailable, so PromptDialog always
// returns ErrUnsupported instead.
var ErrPromptCancelled = errors.New("keychain: prompt cancelled")

// PromptDialog is unavailable on this platform.
func PromptDialog(title, message string) (string, error) {
	return "", ErrUnsupported
}
