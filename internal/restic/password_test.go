package restic

import (
	"testing"

	"neubibackup/internal/config"
)

func TestTestPasswordCommand_NoPasswordCommand(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:     "/tmp/repo",
			Password: "test", // Direct password, no command
		},
	}

	// Should return true when no password command is configured
	if !TestPasswordCommand(cfg) {
		t.Error("TestPasswordCommand should return true when no password command is configured")
	}
}

func TestTestPasswordCommand_SuccessfulCommand(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:            "/tmp/repo",
			PasswordCommand: "echo 'test-password'",
		},
	}

	if !TestPasswordCommand(cfg) {
		t.Error("TestPasswordCommand should return true for successful command")
	}
}

func TestTestPasswordCommand_FailingCommand(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:            "/tmp/repo",
			PasswordCommand: "exit 1",
		},
	}

	if TestPasswordCommand(cfg) {
		t.Error("TestPasswordCommand should return false for failing command")
	}
}

func TestTestPasswordCommand_EmptyOutput(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:            "/tmp/repo",
			PasswordCommand: "printf ''", // Empty output
		},
	}

	if TestPasswordCommand(cfg) {
		t.Error("TestPasswordCommand should return false for empty output")
	}
}

func TestTestPasswordCommand_NonexistentCommand(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:            "/tmp/repo",
			PasswordCommand: "nonexistent-command-that-does-not-exist-12345",
		},
	}

	if TestPasswordCommand(cfg) {
		t.Error("TestPasswordCommand should return false for nonexistent command")
	}
}

func TestTestPasswordCommand_ComplexCommand(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path:            "/tmp/repo",
			PasswordCommand: "printf '%s' 'my-password'", // Complex command with pipes
		},
	}

	if !TestPasswordCommand(cfg) {
		t.Error("TestPasswordCommand should return true for complex successful command")
	}
}
