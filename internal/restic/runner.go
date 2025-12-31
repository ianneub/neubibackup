package restic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"neubibackup/internal/config"
)

// Retry configuration
var (
	MaxRetries    = 5
	RetryDelays   = []time.Duration{1 * time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute}
)

// RunBackup executes a restic backup with retry logic.
// It writes output to the provided writer and returns an error only if all retries fail.
// On the first attempt, it will initialize the repository if it doesn't exist.
// If proxyAddr is non-empty, HTTP_PROXY and HTTPS_PROXY will be set to route through it.
// If progressCb is non-nil, it will be called with progress updates during backup.
func RunBackup(ctx context.Context, cfg *config.Config, logWriter io.Writer, proxyAddr string, progressCb ProgressCallback) error {
	// Check if repository exists, initialize if needed
	if err := ensureRepositoryExists(ctx, cfg, logWriter, proxyAddr); err != nil {
		return fmt.Errorf("ensure repository exists: %w", err)
	}

	var lastErr error

	for attempt := 0; attempt < MaxRetries; attempt++ {
		if attempt > 0 {
			delay := RetryDelays[attempt-1]
			fmt.Fprintf(logWriter, "\n--- Retry attempt %d/%d in %v ---\n\n", attempt+1, MaxRetries, delay)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				// Continue with retry
			}
		}

		err := runBackupOnce(ctx, cfg, logWriter, proxyAddr, progressCb)
		if err == nil {
			return nil // Success
		}

		lastErr = err
		fmt.Fprintf(logWriter, "\nBackup failed: %v\n", err)

		// Don't retry password errors - they require user intervention
		if errors.Is(err, ErrPasswordFailed) {
			return err
		}

		// Check if context was cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return fmt.Errorf("backup failed after %d attempts: %w", MaxRetries, lastErr)
}

func runBackupOnce(ctx context.Context, cfg *config.Config, logWriter io.Writer, proxyAddr string, progressCb ProgressCallback) error {
	binaryPath, err := GetBinaryPath()
	if err != nil {
		return fmt.Errorf("get restic binary: %w", err)
	}

	// Build command arguments
	args := buildBackupArgs(cfg)

	fmt.Fprintf(logWriter, "Running: restic %v\n", sanitizeArgsForLogging(args))
	fmt.Fprintf(logWriter, "Repository: %s\n\n", sanitizeURLForLogging(cfg.Repository.Path))

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	configureCmd(cmd)

	// Set environment
	cmd.Env = append(os.Environ(), buildEnv(cfg, proxyAddr)...)

	// Capture output - wrap with progress writer if callback provided
	var stdoutWriter io.Writer = logWriter
	if progressCb != nil {
		stdoutWriter = NewProgressWriter(logWriter, progressCb, 500*time.Millisecond)
	}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = logWriter

	err = cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			// Exit code 3 means some files couldn't be read but snapshot was created
			if exitCode == 3 {
				fmt.Fprintf(logWriter, "\nBackup completed with warnings (some files could not be read)\n")
				return nil
			}
			return fmt.Errorf("restic exited with code %d", exitCode)
		}
		return err
	}

	fmt.Fprintf(logWriter, "\nBackup completed successfully\n")
	return nil
}

func buildBackupArgs(cfg *config.Config) []string {
	args := []string{"backup", "--json"}

	// Add repository args (global args, repo path, password)
	args = append(args, buildRepoArgs(cfg)...)

	// Excludes
	for _, exclude := range cfg.Backup.Excludes {
		args = append(args, "--exclude", exclude)
	}
	if cfg.Backup.ExcludeFile != "" {
		args = append(args, "--exclude-file", cfg.Backup.ExcludeFile)
	}

	// Backup-specific args from config
	args = append(args, cfg.ResticArgs.Backup...)

	// Add hardcoded flags if not already present in user config
	hardcodedFlags := []string{"--exclude-caches"}
	if runtime.GOOS == "windows" {
		hardcodedFlags = append(hardcodedFlags, "--use-fs-snapshot")
	} else {
		hardcodedFlags = append(hardcodedFlags, "--one-file-system")
	}

	for _, flag := range hardcodedFlags {
		if !containsArg(cfg.ResticArgs.Backup, flag) {
			args = append(args, flag)
		}
	}

	// Paths to backup (must be last)
	args = append(args, cfg.Backup.Paths...)

	return args
}

// containsArg checks if a slice of arguments contains a specific flag.
func containsArg(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

// buildRepoArgs returns the common arguments for repository access:
// global args, repository path, and password source.
func buildRepoArgs(cfg *config.Config) []string {
	args := append([]string{}, cfg.ResticArgs.Global...)
	args = append(args, "-r", cfg.Repository.Path)

	if cfg.Repository.PasswordFile != "" {
		args = append(args, "--password-file", cfg.Repository.PasswordFile)
	} else if cfg.Repository.PasswordCommand != "" {
		args = append(args, "--password-command", cfg.Repository.PasswordCommand)
	}

	return args
}

func buildEnv(cfg *config.Config, proxyAddr string) []string {
	var env []string

	// Set RESTIC_REPOSITORY if not using -r flag
	// (we use -r flag, but setting env doesn't hurt)
	env = append(env, "RESTIC_REPOSITORY="+cfg.Repository.Path)

	// Set password via environment if using direct password
	if cfg.Repository.Password != "" {
		env = append(env, "RESTIC_PASSWORD="+cfg.Repository.Password)
	}

	// Route through Tailscale SOCKS5 proxy if provided
	if proxyAddr != "" {
		env = append(env, "HTTP_PROXY=socks5://"+proxyAddr)
		env = append(env, "HTTPS_PROXY=socks5://"+proxyAddr)
	}

	return env
}

// sanitizeURLForLogging masks passwords in URLs for safe logging.
// Handles both standard URLs (rest:https://user:pass@host) and other formats.
func sanitizeURLForLogging(path string) string {
	// Handle restic REST backend format: rest:https://user:pass@host/path
	if strings.HasPrefix(path, "rest:") {
		prefix := "rest:"
		urlPart := strings.TrimPrefix(path, prefix)
		sanitized := maskPasswordInURL(urlPart)
		return prefix + sanitized
	}

	// Handle other URL formats (sftp, s3, etc. with embedded credentials)
	return maskPasswordInURL(path)
}

// maskPasswordInURL attempts to parse and mask password in a URL string.
func maskPasswordInURL(urlStr string) string {
	// Try to parse as a URL
	parsed, err := url.Parse(urlStr)
	if err != nil {
		// If it doesn't parse as URL, try regex as fallback
		return maskPasswordWithRegex(urlStr)
	}

	// If URL has user info with password, mask it
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			// Use the raw userinfo from the URL which preserves encoding
			// Format is: scheme://userinfo@host/path
			// Find the userinfo portion and replace the password part
			username := parsed.User.Username()
			// Reconstruct URL with masked password
			// We need to find everything between "username:" and "@"
			schemeEnd := strings.Index(urlStr, "://")
			if schemeEnd == -1 {
				return maskPasswordWithRegex(urlStr)
			}
			afterScheme := urlStr[schemeEnd+3:]
			atIdx := strings.Index(afterScheme, "@")
			if atIdx == -1 {
				return urlStr
			}
			userInfo := afterScheme[:atIdx]
			colonIdx := strings.Index(userInfo, ":")
			if colonIdx == -1 {
				return urlStr
			}
			// Replace userinfo with masked version
			maskedUserInfo := username + ":****"
			return urlStr[:schemeEnd+3] + maskedUserInfo + afterScheme[atIdx:]
		}
	}

	return urlStr
}

// maskPasswordWithRegex is a fallback for non-standard URL formats.
var passwordInURLRegex = regexp.MustCompile(`(://[^:]+:)([^@]+)(@)`)

func maskPasswordWithRegex(s string) string {
	return passwordInURLRegex.ReplaceAllString(s, "${1}****${3}")
}

// sanitizeArgsForLogging returns a copy of args with sensitive values masked.
func sanitizeArgsForLogging(args []string) []string {
	result := make([]string, len(args))
	copy(result, args)

	for i := 0; i < len(result); i++ {
		// Mask the value after -r or --repo flag
		if (result[i] == "-r" || result[i] == "--repo") && i+1 < len(result) {
			result[i+1] = sanitizeURLForLogging(result[i+1])
			i++ // Skip the next arg since we just processed it
			continue
		}

		// Handle --repo=value format
		if strings.HasPrefix(result[i], "--repo=") {
			value := strings.TrimPrefix(result[i], "--repo=")
			result[i] = "--repo=" + sanitizeURLForLogging(value)
			continue
		}

		// Handle -r=value format (less common but possible)
		if strings.HasPrefix(result[i], "-r=") {
			value := strings.TrimPrefix(result[i], "-r=")
			result[i] = "-r=" + sanitizeURLForLogging(value)
		}
	}

	return result
}

// ensureRepositoryExists checks if the repository exists and initializes it if not.
// Returns ErrPasswordFailed if the password command fails (should not be retried).
func ensureRepositoryExists(ctx context.Context, cfg *config.Config, logWriter io.Writer, proxyAddr string) error {
	binaryPath, err := GetBinaryPath()
	if err != nil {
		return fmt.Errorf("get restic binary: %w", err)
	}

	// Build args for "snapshots" command to check if repo exists
	args := []string{"snapshots", "--json"}
	args = append(args, buildRepoArgs(cfg)...)

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	configureCmd(cmd)
	cmd.Env = append(os.Environ(), buildEnv(cfg, proxyAddr)...)

	// Capture stderr to detect password errors
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	if err == nil {
		// Repository exists and is accessible
		return nil
	}

	// Determine exit code and stderr content
	exitCode := 1
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}
	stderr := stderrBuf.String()

	// Check for password errors first - these should not be retried
	if isPasswordError(exitCode, stderr) {
		return fmt.Errorf("%w: %s", ErrPasswordFailed, strings.TrimSpace(stderr))
	}

	// Only try to initialize if the repository genuinely doesn't exist
	if !isRepositoryNotFoundError(exitCode, stderr) {
		// Some other error (network, permissions, etc.) - don't try to init
		return fmt.Errorf("repository check failed (exit code %d): %s", exitCode, strings.TrimSpace(stderr))
	}

	// Repository doesn't exist - try to initialize
	fmt.Fprintf(logWriter, "Repository not found, initializing...\n")

	initArgs := []string{"init"}
	initArgs = append(initArgs, buildRepoArgs(cfg)...)

	initCmd := exec.CommandContext(ctx, binaryPath, initArgs...)
	configureCmd(initCmd)
	initCmd.Env = append(os.Environ(), buildEnv(cfg, proxyAddr)...)
	initCmd.Stdout = logWriter
	initCmd.Stderr = logWriter

	if err := initCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("restic init exited with code %d", exitErr.ExitCode())
		}
		return fmt.Errorf("restic init failed: %w", err)
	}

	fmt.Fprintf(logWriter, "Repository initialized successfully\n\n")
	return nil
}
