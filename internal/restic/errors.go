package restic

import (
	"errors"
	"strings"
)

// Sentinel errors for restic operations.
var (
	// ErrPasswordFailed indicates the password command failed or wrong password was provided.
	// This error should NOT be retried as it requires user intervention (e.g., unlocking 1Password).
	ErrPasswordFailed = errors.New("password command failed or wrong password")

	// ErrRepositoryNotFound indicates the repository genuinely doesn't exist.
	// This is the only error that should trigger repository initialization.
	ErrRepositoryNotFound = errors.New("repository does not exist")
)

// Restic exit codes (since restic 0.17.x).
// See: https://restic.readthedocs.io/en/latest/075_scripting.html
const (
	ExitCodeRepoNotFound  = 10 // Repository does not exist (since 0.17.0)
	ExitCodeLockFailed    = 11 // Failed to lock repository (since 0.17.0)
	ExitCodeWrongPassword = 12 // Wrong password (since 0.17.1)
)

// isPasswordError checks if restic failed due to password issues.
// It checks both exit codes (restic 0.17.1+) and stderr patterns for older versions.
func isPasswordError(exitCode int, stderr string) bool {
	// Exit code 12 is definitive for wrong password (restic 0.17.1+)
	if exitCode == ExitCodeWrongPassword {
		return true
	}

	// Check stderr for password-related error messages
	stderrLower := strings.ToLower(stderr)
	return strings.Contains(stderrLower, "resolving password failed") ||
		strings.Contains(stderrLower, "wrong password or no key found")
}

// isRepositoryNotFoundError checks if the error indicates the repository genuinely doesn't exist.
// Only returns true for actual "repo not found" scenarios, not authentication failures.
func isRepositoryNotFoundError(exitCode int, stderr string) bool {
	// Exit code 10 is definitive for repo not found (restic 0.17.0+)
	if exitCode == ExitCodeRepoNotFound {
		return true
	}

	// Fallback for older versions: check stderr
	// Only match genuine "repo doesn't exist" messages
	stderrLower := strings.ToLower(stderr)
	return strings.Contains(stderrLower, "repository does not exist") ||
		strings.Contains(stderrLower, "is there a repository at")
}
