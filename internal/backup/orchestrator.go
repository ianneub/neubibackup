package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

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
	notifier   Notifier
	tailscale  TailscaleProvider
	onProgress ProgressCallback
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
	log.Println("Starting backup...")

	// Connect Tailscale on-demand if configured
	var proxyAddr string
	if o.cfg.IsTailscaleEnabled() && o.tailscale != nil {
		log.Println("Connecting to Tailscale...")
		addr, err := o.tailscale.Connect(ctx)
		if err != nil {
			log.Printf("Tailscale connection failed: %v", err)
			o.handleTailscaleFailure(err)
			return Result{
				Success: false,
				Error:   fmt.Errorf("tailscale connection failed: %w", err),
			}
		}
		proxyAddr = addr
		log.Printf("Tailscale connected, using proxy: %s", proxyAddr)

		// Disconnect Tailscale when backup completes
		defer func() {
			log.Println("Disconnecting from Tailscale...")
			if err := o.tailscale.Disconnect(); err != nil {
				log.Printf("Warning: Tailscale shutdown error: %v", err)
			}
		}()
	}

	// Send start notification
	if err := o.notifier.NotifyStart(); err != nil {
		log.Printf("Warning: start notification failed: %v", err)
	}

	// Create log file
	logFile, err := logging.CreateLogFile()
	if err != nil {
		log.Printf("Error creating log file: %v", err)
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
			log.Println("Backup was cancelled by user")
			if err := o.notifier.NotifyCancelled(); err != nil {
				log.Printf("Warning: cancellation notification failed: %v", err)
			}
			return Result{
				Success:   false,
				Cancelled: true,
				Error:     backupErr,
				LogPath:   logPath,
			}
		}

		log.Printf("Backup failed: %v", backupErr)
		o.handleBackupFailure(backupErr, logFile)
		return Result{
			Success: false,
			Error:   backupErr,
			LogPath: logPath,
		}
	}

	// Success
	log.Println("Backup completed successfully")
	o.handleBackupSuccess()

	// Cleanup old logs
	if err := logging.CleanupOldLogs(); err != nil {
		log.Printf("Warning: log cleanup failed: %v", err)
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
		log.Printf("Warning: failure notification failed: %v", notifyErr)
	}
}

// handleBackupFailure handles a backup failure.
func (o *Orchestrator) handleBackupFailure(backupErr error, logFile *os.File) {
	o.recordFailure(backupErr)

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
		log.Printf("Warning: failure notification failed: %v", err)
	}
}

// handleBackupSuccess handles a successful backup.
func (o *Orchestrator) handleBackupSuccess() {
	o.state.RecordSuccess()
	if err := o.state.Save(); err != nil {
		log.Printf("Error saving state: %v", err)
	}

	if err := o.notifier.NotifySuccess("Backup completed successfully"); err != nil {
		log.Printf("Warning: success notification failed: %v", err)
	}
}

// recordFailure records a failure in the app state.
func (o *Orchestrator) recordFailure(err error) {
	o.state.RecordFailure(err)
	if saveErr := o.state.Save(); saveErr != nil {
		log.Printf("Error saving state: %v", saveErr)
	}
}
