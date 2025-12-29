package restic

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsPasswordError(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		stderr   string
		want     bool
	}{
		{
			name:     "exit code 12 is password error",
			exitCode: 12,
			stderr:   "",
			want:     true,
		},
		{
			name:     "resolving password failed in stderr",
			exitCode: 1,
			stderr:   "Fatal: Resolving password failed: exit status 1",
			want:     true,
		},
		{
			name:     "wrong password or no key found",
			exitCode: 1,
			stderr:   "Fatal: wrong password or no key found",
			want:     true,
		},
		{
			name:     "1password authorization timeout",
			exitCode: 1,
			stderr:   "[ERROR] could not read secret: error initializing client: authorization timeout\nFatal: Resolving password failed: exit status 1",
			want:     true,
		},
		{
			name:     "exit code 10 is not password error",
			exitCode: 10,
			stderr:   "",
			want:     false,
		},
		{
			name:     "generic error is not password error",
			exitCode: 1,
			stderr:   "connection refused",
			want:     false,
		},
		{
			name:     "case insensitive match for resolving password",
			exitCode: 1,
			stderr:   "RESOLVING PASSWORD FAILED",
			want:     true,
		},
		{
			name:     "case insensitive match for wrong password",
			exitCode: 1,
			stderr:   "WRONG PASSWORD OR NO KEY FOUND",
			want:     true,
		},
		{
			name:     "exit code 0 with password text is not error",
			exitCode: 0,
			stderr:   "",
			want:     false,
		},
		{
			name:     "network error is not password error",
			exitCode: 1,
			stderr:   "unable to open repository: client.Head: dial tcp: no such host",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPasswordError(tt.exitCode, tt.stderr)
			if got != tt.want {
				t.Errorf("isPasswordError(%d, %q) = %v, want %v",
					tt.exitCode, tt.stderr, got, tt.want)
			}
		})
	}
}

func TestIsRepositoryNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		stderr   string
		want     bool
	}{
		{
			name:     "exit code 10 is repo not found",
			exitCode: 10,
			stderr:   "",
			want:     true,
		},
		{
			name:     "exit code 12 is not repo not found",
			exitCode: 12,
			stderr:   "",
			want:     false,
		},
		{
			name:     "repository does not exist message",
			exitCode: 1,
			stderr:   "Fatal: repository does not exist",
			want:     true,
		},
		{
			name:     "is there a repository message",
			exitCode: 1,
			stderr:   "Is there a repository at /path/to/repo?",
			want:     true,
		},
		{
			name:     "password error is not repo not found",
			exitCode: 1,
			stderr:   "wrong password or no key found",
			want:     false,
		},
		{
			name:     "generic error is not repo not found",
			exitCode: 1,
			stderr:   "connection refused",
			want:     false,
		},
		{
			name:     "case insensitive match",
			exitCode: 1,
			stderr:   "REPOSITORY DOES NOT EXIST",
			want:     true,
		},
		{
			name:     "network error is not repo not found",
			exitCode: 1,
			stderr:   "unable to open repository: client.Head: dial tcp: no such host",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRepositoryNotFoundError(tt.exitCode, tt.stderr)
			if got != tt.want {
				t.Errorf("isRepositoryNotFoundError(%d, %q) = %v, want %v",
					tt.exitCode, tt.stderr, got, tt.want)
			}
		})
	}
}

func TestErrPasswordFailed_ErrorsIs(t *testing.T) {
	// Test that wrapped errors are properly detected with errors.Is
	wrapped := fmt.Errorf("backup failed: %w", ErrPasswordFailed)

	if !errors.Is(wrapped, ErrPasswordFailed) {
		t.Error("errors.Is should detect wrapped ErrPasswordFailed")
	}

	// Double wrapped
	doubleWrapped := fmt.Errorf("outer: %w", wrapped)
	if !errors.Is(doubleWrapped, ErrPasswordFailed) {
		t.Error("errors.Is should detect double-wrapped ErrPasswordFailed")
	}

	// Unrelated error should not match
	other := errors.New("some other error")
	if errors.Is(other, ErrPasswordFailed) {
		t.Error("unrelated error should not match ErrPasswordFailed")
	}

	// ErrRepositoryNotFound should not match ErrPasswordFailed
	if errors.Is(ErrRepositoryNotFound, ErrPasswordFailed) {
		t.Error("ErrRepositoryNotFound should not match ErrPasswordFailed")
	}
}

func TestErrRepositoryNotFound_ErrorsIs(t *testing.T) {
	wrapped := fmt.Errorf("check failed: %w", ErrRepositoryNotFound)

	if !errors.Is(wrapped, ErrRepositoryNotFound) {
		t.Error("errors.Is should detect wrapped ErrRepositoryNotFound")
	}

	if errors.Is(ErrPasswordFailed, ErrRepositoryNotFound) {
		t.Error("ErrPasswordFailed should not match ErrRepositoryNotFound")
	}
}
