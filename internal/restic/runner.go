package restic

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
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

	fmt.Fprintf(logWriter, "Running: restic %v\n", args)
	fmt.Fprintf(logWriter, "Repository: %s\n\n", cfg.Repository.Path)

	cmd := exec.CommandContext(ctx, binaryPath, args...)

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

	// Add global args
	args = append(args, cfg.ResticArgs.Global...)

	// Repository
	args = append(args, "-r", cfg.Repository.Path)

	// Password source
	if cfg.Repository.PasswordFile != "" {
		args = append(args, "--password-file", cfg.Repository.PasswordFile)
	} else if cfg.Repository.PasswordCommand != "" {
		args = append(args, "--password-command", cfg.Repository.PasswordCommand)
	}

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
	hardcodedFlags := []string{"--one-file-system", "--exclude-caches"}
	if runtime.GOOS == "windows" {
		hardcodedFlags = append(hardcodedFlags, "--use-fs-snapshot")
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

// RunCommand runs an arbitrary restic command (for testing, init, etc).
// If proxyAddr is non-empty, HTTP_PROXY and HTTPS_PROXY will be set.
func RunCommand(ctx context.Context, cfg *config.Config, logWriter io.Writer, proxyAddr string, command string, extraArgs ...string) error {
	binaryPath, err := GetBinaryPath()
	if err != nil {
		return fmt.Errorf("get restic binary: %w", err)
	}

	args := []string{command}
	args = append(args, cfg.ResticArgs.Global...)
	args = append(args, "-r", cfg.Repository.Path)

	if cfg.Repository.PasswordFile != "" {
		args = append(args, "--password-file", cfg.Repository.PasswordFile)
	} else if cfg.Repository.PasswordCommand != "" {
		args = append(args, "--password-command", cfg.Repository.PasswordCommand)
	}

	args = append(args, extraArgs...)

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = append(os.Environ(), buildEnv(cfg, proxyAddr)...)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	return cmd.Run()
}

// ensureRepositoryExists checks if the repository exists and initializes it if not.
func ensureRepositoryExists(ctx context.Context, cfg *config.Config, logWriter io.Writer, proxyAddr string) error {
	binaryPath, err := GetBinaryPath()
	if err != nil {
		return fmt.Errorf("get restic binary: %w", err)
	}

	// Build args for "snapshots" command to check if repo exists
	args := []string{"snapshots", "--json"}
	args = append(args, cfg.ResticArgs.Global...)
	args = append(args, "-r", cfg.Repository.Path)

	if cfg.Repository.PasswordFile != "" {
		args = append(args, "--password-file", cfg.Repository.PasswordFile)
	} else if cfg.Repository.PasswordCommand != "" {
		args = append(args, "--password-command", cfg.Repository.PasswordCommand)
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = append(os.Environ(), buildEnv(cfg, proxyAddr)...)

	// Run silently - we only care about exit code
	err = cmd.Run()
	if err == nil {
		// Repository exists
		return nil
	}

	// Repository doesn't exist or is inaccessible - try to initialize
	fmt.Fprintf(logWriter, "Repository not found, initializing...\n")

	initArgs := []string{"init"}
	initArgs = append(initArgs, cfg.ResticArgs.Global...)
	initArgs = append(initArgs, "-r", cfg.Repository.Path)

	if cfg.Repository.PasswordFile != "" {
		initArgs = append(initArgs, "--password-file", cfg.Repository.PasswordFile)
	} else if cfg.Repository.PasswordCommand != "" {
		initArgs = append(initArgs, "--password-command", cfg.Repository.PasswordCommand)
	}

	initCmd := exec.CommandContext(ctx, binaryPath, initArgs...)
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
