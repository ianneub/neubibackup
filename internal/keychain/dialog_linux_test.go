//go:build linux

package keychain

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"testing"
)

// stubZenity replaces the package-level zenity runner for the duration of a
// test, restoring the original on cleanup, so no real dialog is opened.
func stubZenity(t *testing.T, fn func(args ...string) ([]byte, error)) {
	t.Helper()
	orig := runZenity
	runZenity = fn
	t.Cleanup(func() { runZenity = orig })
}

func TestPromptDialogReturnsPassword(t *testing.T) {
	stubZenity(t, func(args ...string) ([]byte, error) {
		return []byte("hunter2\n"), nil
	})

	pw, err := PromptDialog("NeubiBackup", "Enter password:")
	if err != nil {
		t.Fatalf("PromptDialog: %v", err)
	}
	if pw != "hunter2" {
		t.Errorf("password = %q, want hunter2", pw)
	}
}

func TestPromptDialogKeepsSurroundingSpaces(t *testing.T) {
	stubZenity(t, func(args ...string) ([]byte, error) {
		return []byte(" pass word \n"), nil
	})

	pw, err := PromptDialog("NeubiBackup", "Enter password:")
	if err != nil {
		t.Fatalf("PromptDialog: %v", err)
	}
	if pw != " pass word " {
		t.Errorf("password = %q, want %q", pw, " pass word ")
	}
}

func TestPromptDialogPassesTitleAndMessage(t *testing.T) {
	var got []string
	stubZenity(t, func(args ...string) ([]byte, error) {
		got = args
		return []byte("pw\n"), nil
	})

	if _, err := PromptDialog(`Neubi "Backup"`, "Enter the password:"); err != nil {
		t.Fatalf("PromptDialog: %v", err)
	}
	want := []string{"--entry", "--hide-text", `--title=Neubi "Backup"`, "--text=Enter the password:"}
	if !slices.Equal(got, want) {
		t.Errorf("zenity args = %q, want %q", got, want)
	}
}

func TestPromptDialogEmptyAnswerCancels(t *testing.T) {
	stubZenity(t, func(args ...string) ([]byte, error) {
		return []byte("\n"), nil
	})

	_, err := PromptDialog("NeubiBackup", "Enter password:")
	if !errors.Is(err, ErrPromptCancelled) {
		t.Errorf("err = %v, want ErrPromptCancelled", err)
	}
}

func TestPromptDialogCancelButtonCancels(t *testing.T) {
	stubZenity(t, func(args ...string) ([]byte, error) {
		return nil, errors.New("exit status 1")
	})

	_, err := PromptDialog("NeubiBackup", "Enter password:")
	if !errors.Is(err, ErrPromptCancelled) {
		t.Errorf("err = %v, want ErrPromptCancelled", err)
	}
}

func TestPromptDialogMissingZenityIsUnsupported(t *testing.T) {
	stubZenity(t, func(args ...string) ([]byte, error) {
		return nil, fmt.Errorf("exec: %q: %w", "zenity", exec.ErrNotFound)
	})

	_, err := PromptDialog("NeubiBackup", "Enter password:")
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}
