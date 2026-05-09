//go:build darwin

package keychain

import (
	"errors"
	"strings"
	"testing"
)

// stubOsascript replaces the package-level osascript runner for the
// duration of a test, restoring the original on cleanup. This is the
// only way to exercise PromptDialog's parsing/escape logic without
// firing a real macOS dialog.
func stubOsascript(t *testing.T, fn func(script string) ([]byte, error)) {
	t.Helper()
	orig := runOsascript
	runOsascript = fn
	t.Cleanup(func() { runOsascript = orig })
}

func TestPromptDialogReturnsPassword(t *testing.T) {
	stubOsascript(t, func(script string) ([]byte, error) {
		return []byte("button returned:OK, text returned:hunter2\n"), nil
	})

	pw, err := PromptDialog("NeubiBackup", "Enter password:")
	if err != nil {
		t.Fatalf("PromptDialog: %v", err)
	}
	if pw != "hunter2" {
		t.Errorf("password = %q, want hunter2", pw)
	}
}

func TestPromptDialogEmptyAnswerCancels(t *testing.T) {
	stubOsascript(t, func(script string) ([]byte, error) {
		return []byte("button returned:OK, text returned:\n"), nil
	})

	_, err := PromptDialog("NeubiBackup", "Enter password:")
	if !errors.Is(err, ErrPromptCancelled) {
		t.Errorf("err = %v, want ErrPromptCancelled", err)
	}
}

func TestPromptDialogOsascriptErrorCancels(t *testing.T) {
	stubOsascript(t, func(script string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	})

	_, err := PromptDialog("NeubiBackup", "Enter password:")
	if !errors.Is(err, ErrPromptCancelled) {
		t.Errorf("err = %v, want ErrPromptCancelled", err)
	}
}

func TestPromptDialogMissingMarkerCancels(t *testing.T) {
	stubOsascript(t, func(script string) ([]byte, error) {
		return []byte("unexpected\n"), nil
	})

	_, err := PromptDialog("NeubiBackup", "Enter password:")
	if !errors.Is(err, ErrPromptCancelled) {
		t.Errorf("err = %v, want ErrPromptCancelled", err)
	}
}

func TestPromptDialogPreservesPasswordWithSpecialChars(t *testing.T) {
	stubOsascript(t, func(script string) ([]byte, error) {
		// Real osascript output preserves spaces, punctuation, and any
		// trailing CRLF — we strip the CRLF but keep everything else.
		return []byte("button returned:OK, text returned:p@ss w0rd! 🔐\r\n"), nil
	})

	pw, err := PromptDialog("NeubiBackup", "Enter password:")
	if err != nil {
		t.Fatalf("PromptDialog: %v", err)
	}
	if pw != "p@ss w0rd! 🔐" {
		t.Errorf("password = %q, want %q", pw, "p@ss w0rd! 🔐")
	}
}

func TestPromptDialogEscapesQuotesInTitleAndMessage(t *testing.T) {
	var captured string
	stubOsascript(t, func(script string) ([]byte, error) {
		captured = script
		return []byte("button returned:OK, text returned:x\n"), nil
	})

	if _, err := PromptDialog(`Tit"le`, `Mes"sage`); err != nil {
		t.Fatalf("PromptDialog: %v", err)
	}

	// The double quotes from the inputs must appear escaped in the
	// AppleScript so the dialog command parses correctly.
	if !strings.Contains(captured, `Tit\"le`) {
		t.Errorf("title not escaped in script: %q", captured)
	}
	if !strings.Contains(captured, `Mes\"sage`) {
		t.Errorf("message not escaped in script: %q", captured)
	}
}
