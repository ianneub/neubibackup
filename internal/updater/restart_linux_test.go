//go:build linux

package updater

import (
	"errors"
	"os"
	"slices"
	"testing"
)

func stubExec(t *testing.T, fn func(argv0 string, argv []string, envv []string) error) {
	t.Helper()
	orig := execFn
	execFn = fn
	t.Cleanup(func() { execFn = orig })
}

func TestRestartExecsCurrentExecutable(t *testing.T) {
	var gotPath string
	var gotArgs []string
	stubExec(t, func(argv0 string, argv []string, envv []string) error {
		gotPath, gotArgs = argv0, argv
		return nil
	})

	if err := Restart(); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	want, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if gotPath != want {
		t.Errorf("exec path = %q, want %q", gotPath, want)
	}
	if !slices.Equal(gotArgs, os.Args) {
		t.Errorf("exec args = %v, want %v", gotArgs, os.Args)
	}
}

func TestRestartReturnsExecError(t *testing.T) {
	stubExec(t, func(string, []string, []string) error {
		return errors.New("permission denied")
	})

	if err := Restart(); err == nil {
		t.Fatal("Restart: expected error, got nil")
	}
}
