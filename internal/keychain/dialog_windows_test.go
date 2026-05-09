//go:build windows

package keychain

import (
	"errors"
	"testing"
)

func stubPromptScript(t *testing.T, fn func(title, message string) ([]byte, error)) {
	t.Helper()
	orig := runPromptScript
	runPromptScript = fn
	t.Cleanup(func() { runPromptScript = orig })
}

func TestPromptDialogReturnsPassword(t *testing.T) {
	stubPromptScript(t, func(title, message string) ([]byte, error) {
		if title != "NeubiBackup" {
			t.Errorf("title = %q", title)
		}
		if message != "Enter password:" {
			t.Errorf("message = %q", message)
		}
		return []byte("hunter2"), nil
	})

	pw, err := PromptDialog("NeubiBackup", "Enter password:")
	if err != nil {
		t.Fatalf("PromptDialog: %v", err)
	}
	if pw != "hunter2" {
		t.Errorf("password = %q, want hunter2", pw)
	}
}

func TestPromptDialogTrimsCRLF(t *testing.T) {
	stubPromptScript(t, func(title, message string) ([]byte, error) {
		return []byte("secret\r\n"), nil
	})

	pw, err := PromptDialog("NeubiBackup", "Enter password:")
	if err != nil {
		t.Fatalf("PromptDialog: %v", err)
	}
	if pw != "secret" {
		t.Errorf("password = %q, want secret", pw)
	}
}

func TestPromptDialogCancelExitCancels(t *testing.T) {
	stubPromptScript(t, func(title, message string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	})

	_, err := PromptDialog("NeubiBackup", "Enter password:")
	if !errors.Is(err, ErrPromptCancelled) {
		t.Errorf("err = %v, want ErrPromptCancelled", err)
	}
}

func TestPromptDialogEmptyOutputCancels(t *testing.T) {
	stubPromptScript(t, func(title, message string) ([]byte, error) {
		return []byte(""), nil
	})

	_, err := PromptDialog("NeubiBackup", "Enter password:")
	if !errors.Is(err, ErrPromptCancelled) {
		t.Errorf("err = %v, want ErrPromptCancelled", err)
	}
}
