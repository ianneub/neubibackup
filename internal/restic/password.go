package restic

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"time"

	"neubibackup/internal/config"
)

const passwordTestTimeout = 10 * time.Second

// TestPasswordCommand tests if the password command is currently working.
// This is useful for checking if a keychain is unlocked before attempting backup.
//
// Returns true if:
//   - No password command is configured (password may be in file or direct config)
//   - The password command executes successfully and returns non-empty output
//
// Returns false if:
//   - The password command fails to execute
//   - The password command returns empty output
//   - The password command times out
func TestPasswordCommand(cfg *config.Config) bool {
	if cfg.Repository.PasswordCommand == "" {
		// No password command configured - can't test
		// Return true to allow proceeding (password may be in file or direct)
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), passwordTestTimeout)
	defer cancel()

	// Execute the password command via shell to handle complex commands
	cmd := exec.CommandContext(ctx, "sh", "-c", cfg.Repository.PasswordCommand)

	output, err := cmd.Output()
	if err != nil {
		slog.Debug("Password command test failed", "error", err)
		return false
	}

	// Command succeeded - check if it returned actual output
	if len(bytes.TrimSpace(output)) == 0 {
		slog.Debug("Password command returned empty output")
		return false
	}

	slog.Debug("Password command test succeeded")
	return true
}
