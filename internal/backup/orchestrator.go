package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"neubibackup/internal/config"
	"neubibackup/internal/logging"
	"neubibackup/internal/restic"
	"neubibackup/internal/state"
)

// Result represents the outcome of a backup operation.
type Result struct {
	Success   bool
	Cancelled bool
	Error     error
	LogPath   string
}

// TailscaleProvider defines the interface for Tailscale connectivity.
type TailscaleProvider interface {
	// Connect establishes a Tailscale connection and returns the proxy address.
	Connect(ctx context.Context) (proxyAddr string, err error)

	// Disconnect closes the Tailscale connection.
	Disconnect() error
}

// ProgressCallback is called with progress updates during backup.
type ProgressCallback func(progress restic.BackupProgress)

// Orchestrator coordinates the execution of a backup operation.
// It manages the backup lifecycle including:
// - Tailscale connectivity (if configured)
// - Notification delivery (healthchecks, pushover)
// - State recording (success/failure tracking)
// - Log file management
type Orchestrator struct {
	cfg        *config.Config
	state      *state.State
	notifier     Notifier
	tailscale    TailscaleProvider
	onProgress   ProgressCallback
	location     *time.Location
	logRetention int
}

// OrchestratorOption is a functional option for configuring an Orchestrator.
type OrchestratorOption func(*Orchestrator)

// WithNotifier sets the notifier for the orchestrator.
func WithNotifier(n Notifier) OrchestratorOption {
	return func(o *Orchestrator) {
		o.notifier = n
	}
}

// WithTailscale sets the Tailscale provider for the orchestrator.
func WithTailscale(t TailscaleProvider) OrchestratorOption {
	return func(o *Orchestrator) {
		o.tailscale = t
	}
}

// WithProgressCallback sets the progress callback for the orchestrator.
func WithProgressCallback(cb ProgressCallback) OrchestratorOption {
	return func(o *Orchestrator) {
		o.onProgress = cb
	}
}

// WithLocation sets the timezone location for the orchestrator.
func WithLocation(loc *time.Location) OrchestratorOption {
	return func(o *Orchestrator) {
		o.location = loc
	}
}

// WithLogRetention sets the maximum number of log files to retain after a
// successful backup. Zero or negative falls back to logging.DefaultMaxLogFiles.
func WithLogRetention(maxFiles int) OrchestratorOption {
	return func(o *Orchestrator) {
		o.logRetention = maxFiles
	}
}

// NewOrchestrator creates a new backup orchestrator.
func NewOrchestrator(cfg *config.Config, appState *state.State, opts ...OrchestratorOption) *Orchestrator {
	o := &Orchestrator{
		cfg:      cfg,
		state:    appState,
		notifier: &NullNotifier{},
	}

	for _, opt := range opts {
		opt(o)
	}

	return o
}

// Run executes the backup operation.
// It handles the complete backup lifecycle:
// 1. Connect Tailscale (if configured)
// 2. Send start notification
// 3. Create log file
// 4. Run restic backup
// 5. Send success/failure notification
// 6. Record state
// 7. Cleanup logs
//
// The context can be used to cancel the backup operation.
func (o *Orchestrator) Run(ctx context.Context) Result {
	slog.Info("Starting backup...")

	// Connect Tailscale on-demand if configured
	var proxyAddr string
	if o.cfg.IsTailscaleEnabled() && o.tailscale != nil {
		slog.Info("Connecting to Tailscale...")
		addr, err := o.tailscale.Connect(ctx)
		if err != nil {
			slog.Error("Tailscale connection failed", "error", err)
			o.handleTailscaleFailure(err)
			return Result{
				Success: false,
				Error:   fmt.Errorf("tailscale connection failed: %w", err),
			}
		}
		proxyAddr = addr
		slog.Info("Tailscale connected", "proxy", proxyAddr)

		// Disconnect Tailscale when backup completes
		defer func() {
			slog.Info("Disconnecting from Tailscale...")
			if err := o.tailscale.Disconnect(); err != nil {
				slog.Warn("Tailscale shutdown error", "error", err)
			}
		}()
	}

	// Send start notification
	if err := o.notifier.NotifyStart(); err != nil {
		slog.Warn("Start notification failed", "error", err)
	}

	// Create log file
	logFile, err := logging.CreateLogFile()
	if err != nil {
		slog.Error("Error creating log file", "error", err)
		o.recordFailure(err)
		return Result{
			Success: false,
			Error:   fmt.Errorf("create log file: %w", err),
		}
	}
	defer logFile.Close()

	logPath := logFile.Name()

	// Write to both log file and stdout
	logWriter := io.MultiWriter(logFile, os.Stdout)

	// Run backup
	backupErr := restic.RunBackup(ctx, o.cfg, logWriter, proxyAddr, o.wrapProgressCallback())

	if backupErr != nil {
		// Check if backup was cancelled
		if errors.Is(backupErr, context.Canceled) {
			slog.Info("Backup was cancelled by user")
			if err := o.notifier.NotifyCancelled(); err != nil {
				slog.Warn("Cancellation notification failed", "error", err)
			}
			return Result{
				Success:   false,
				Cancelled: true,
				Error:     backupErr,
				LogPath:   logPath,
			}
		}

		slog.Error("Backup failed", "error", backupErr)
		o.handleBackupFailure(backupErr, logFile)
		return Result{
			Success: false,
			Error:   backupErr,
			LogPath: logPath,
		}
	}

	// Success
	slog.Info("Backup completed successfully")
	o.handleBackupSuccess()

	// Cleanup old logs
	if err := logging.CleanupOldLogs(o.logRetention); err != nil {
		slog.Warn("Log cleanup failed", "error", err)
	}

	return Result{
		Success: true,
		LogPath: logPath,
	}
}

// wrapProgressCallback wraps the user's progress callback to match restic's signature.
func (o *Orchestrator) wrapProgressCallback() restic.ProgressCallback {
	if o.onProgress == nil {
		return nil
	}
	return func(p restic.BackupProgress) {
		o.onProgress(p)
	}
}

// handleTailscaleFailure handles a Tailscale connection failure.
func (o *Orchestrator) handleTailscaleFailure(err error) {
	errMsg := "Tailscale connection failed: " + err.Error()
	o.recordFailure(err)

	if notifyErr := o.notifier.NotifyFailure(errMsg, ""); notifyErr != nil {
		slog.Warn("Failure notification failed", "error", notifyErr)
	}
}

// handleBackupFailure handles a backup failure.
func (o *Orchestrator) handleBackupFailure(backupErr error, logFile *os.File) {
	o.state.RecordFailure(backupErr)
	if saveErr := o.state.Save(); saveErr != nil {
		slog.Error("Error saving state", "error", saveErr)
	}

	// Read logs for notification
	var logs string
	if logFile != nil {
		logFile.Seek(0, 0)
		logData, err := io.ReadAll(logFile)
		if err == nil {
			logs = string(logData)
		}
	}

	if err := o.notifier.NotifyFailure(backupErr.Error(), logs); err != nil {
		slog.Warn("Failure notification failed", "error", err)
	}
}

// handleBackupSuccess handles a successful backup.
func (o *Orchestrator) handleBackupSuccess() {
	o.state.RecordSuccess()
	if err := o.state.Save(); err != nil {
		slog.Error("Error saving state", "error", err)
	}

	if err := o.notifier.NotifySuccess("Backup completed successfully"); err != nil {
		slog.Warn("Success notification failed", "error", err)
	}
}

// recordFailure records a retryable failure in the app state.
func (o *Orchestrator) recordFailure(err error) {
	o.state.RecordFailure(err)
	if saveErr := o.state.Save(); saveErr != nil {
		slog.Error("Error saving state", "error", saveErr)
	}
}
