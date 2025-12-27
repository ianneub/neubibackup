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
func RunBackup(ctx context.Context, cfg *config.Config, logWriter io.Writer) error {
	// Check if repository exists, initialize if needed
	if err := ensureRepositoryExists(ctx, cfg, logWriter); err != nil {
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

		err := runBackupOnce(ctx, cfg, logWriter)
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

func runBackupOnce(ctx context.Context, cfg *config.Config, logWriter io.Writer) error {
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
	cmd.Env = append(os.Environ(), buildEnv(cfg)...)

	// Capture output
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	err = cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("restic exited with code %d", exitErr.ExitCode())
		}
		return err
	}

	fmt.Fprintf(logWriter, "\nBackup completed successfully\n")
	return nil
}

func buildBackupArgs(cfg *config.Config) []string {
	args := []string{"backup"}

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

	// Backup-specific args
	args = append(args, cfg.ResticArgs.Backup...)

	// Add VSS snapshot flag on Windows
	if runtime.GOOS == "windows" {
		hasVSS := false
		for _, arg := range cfg.ResticArgs.Backup {
			if arg == "--use-fs-snapshot" {
				hasVSS = true
				break
			}
		}
		if !hasVSS {
			args = append(args, "--use-fs-snapshot")
		}
	}

	// Paths to backup (must be last)
	args = append(args, cfg.Backup.Paths...)

	return args
}

func buildEnv(cfg *config.Config) []string {
	var env []string

	// Set RESTIC_REPOSITORY if not using -r flag
	// (we use -r flag, but setting env doesn't hurt)
	env = append(env, "RESTIC_REPOSITORY="+cfg.Repository.Path)

	// Set password via environment if using direct password
	if cfg.Repository.Password != "" {
		env = append(env, "RESTIC_PASSWORD="+cfg.Repository.Password)
	}

	return env
}

// RunCommand runs an arbitrary restic command (for testing, init, etc).
func RunCommand(ctx context.Context, cfg *config.Config, logWriter io.Writer, command string, extraArgs ...string) error {
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
	cmd.Env = append(os.Environ(), buildEnv(cfg)...)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	return cmd.Run()
}

// ensureRepositoryExists checks if the repository exists and initializes it if not.
func ensureRepositoryExists(ctx context.Context, cfg *config.Config, logWriter io.Writer) error {
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
	cmd.Env = append(os.Environ(), buildEnv(cfg)...)

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
	initCmd.Env = append(os.Environ(), buildEnv(cfg)...)
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
