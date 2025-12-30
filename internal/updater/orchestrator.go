package updater

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// BackupChecker checks if a backup is currently running.
type BackupChecker interface {
	IsRunning() bool
}

// UpdateStateProvider manages update state.
type UpdateStateProvider interface {
	HasUpdate() bool
	GetAvailableVersion() string
	SetAvailableVersion(version string)
	TryStartUpdate() bool
	FinishUpdate()
}

// MenuUpdater updates the menu with update status.
type MenuUpdater interface {
	SetUpdateStatus(text string, enabled bool)
}

// StateProvider provides access to application state for update tracking.
type StateProvider interface {
	GetLastUpdateCheck() time.Time
	SetLastUpdateCheck(t time.Time)
	SetLastUpdateError(err string, t time.Time)
	SetLastUpdateSuccess(version string, t time.Time)
	Save() error
}

// UpdateOrchestrator coordinates update checking and installation.
type UpdateOrchestrator struct {
	updater       *Updater
	state         StateProvider
	backupChecker BackupChecker
	updateState   UpdateStateProvider
	menuUpdater   MenuUpdater
}

// NewUpdateOrchestrator creates a new UpdateOrchestrator.
func NewUpdateOrchestrator(
	updater *Updater,
	state StateProvider,
	backupChecker BackupChecker,
	updateState UpdateStateProvider,
	menuUpdater MenuUpdater,
) *UpdateOrchestrator {
	return &UpdateOrchestrator{
		updater:       updater,
		state:         state,
		backupChecker: backupChecker,
		updateState:   updateState,
		menuUpdater:   menuUpdater,
	}
}

// CheckIfNeeded checks for updates if not checked recently (within 24 hours).
func (o *UpdateOrchestrator) CheckIfNeeded(ctx context.Context) {
	if time.Since(o.state.GetLastUpdateCheck()) < 24*time.Hour {
		slog.Info("Skipping update check - checked recently")
		return
	}
	o.Check(ctx)
}

// ManualCheck performs a manual update check with UI feedback.
func (o *UpdateOrchestrator) ManualCheck(ctx context.Context) {
	if o.menuUpdater != nil {
		o.menuUpdater.SetUpdateStatus("Checking for updates...", false)
	}

	o.Check(ctx)

	// Re-enable the menu item if no update was found
	if !o.updateState.HasUpdate() && o.menuUpdater != nil {
		o.menuUpdater.SetUpdateStatus("Check for Updates", true)
	}
}

// Check checks for available updates and triggers auto-update if found.
func (o *UpdateOrchestrator) Check(ctx context.Context) {
	slog.Info("Checking for updates...")

	newVersion, available, err := o.updater.CheckForUpdate(ctx)
	if err != nil {
		slog.Error("Update check failed", "error", err)
		return
	}

	// Record the check time
	o.state.SetLastUpdateCheck(time.Now())
	if err := o.state.Save(); err != nil {
		slog.Error("Error saving state", "error", err)
	}

	if available {
		o.updateState.SetAvailableVersion(newVersion)
		if o.menuUpdater != nil {
			o.menuUpdater.SetUpdateStatus(fmt.Sprintf("Update Available (%s)", newVersion), true)
		}
		slog.Info("Update available", "version", newVersion)

		// Trigger automatic update in background
		go o.AttemptAutoUpdate(ctx, newVersion)
	} else {
		slog.Info("No update available")
	}
}

// AttemptAutoUpdate tries to apply an update automatically in the background.
// It waits for any running backup to complete before applying.
func (o *UpdateOrchestrator) AttemptAutoUpdate(ctx context.Context, version string) {
	if !o.updateState.TryStartUpdate() {
		slog.Info("Auto-update: already in progress, skipping")
		return
	}

	defer o.updateState.FinishUpdate()

	// Wait for backup to complete if running (with timeout)
	waitStart := time.Now()
	maxWait := 2 * time.Hour

	for {
		if !o.backupChecker.IsRunning() {
			break
		}

		if time.Since(waitStart) > maxWait {
			slog.Info("Auto-update: gave up waiting for backup to complete")
			return
		}

		slog.Info("Auto-update: waiting for backup to complete...")
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
			// Continue waiting
		}
	}

	// Check if we're still supposed to run
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Apply the update
	slog.Info("Auto-update: applying update", "version", version)
	if o.menuUpdater != nil {
		o.menuUpdater.SetUpdateStatus("Updating...", false)
	}

	if err := o.updater.DownloadAndApply(ctx); err != nil {
		slog.Error("Auto-update failed", "error", err)
		if o.menuUpdater != nil {
			o.menuUpdater.SetUpdateStatus(fmt.Sprintf("Update failed (%s)", version), true)
		}

		// Record error in state
		o.state.SetLastUpdateError(err.Error(), time.Now())
		if saveErr := o.state.Save(); saveErr != nil {
			slog.Error("Error saving state", "error", saveErr)
		}
		return
	}

	// Update succeeded
	slog.Info("Auto-update: successfully updated, restarting...", "version", version)

	// Record successful update
	o.state.SetLastUpdateSuccess(version, time.Now())
	if err := o.state.Save(); err != nil {
		slog.Error("Error saving state", "error", err)
	}

	// Restart the application
	if err := Restart(); err != nil {
		slog.Error("Auto-update: restart failed", "error", err)
		if o.menuUpdater != nil {
			o.menuUpdater.SetUpdateStatus("Updated - please restart manually", true)
		}
	}
}

// Install installs an available update (user-triggered).
func (o *UpdateOrchestrator) Install(ctx context.Context) {
	// Prevent update during backup
	if o.backupChecker.IsRunning() {
		slog.Info("Cannot update while backup is running")
		if o.menuUpdater != nil {
			o.menuUpdater.SetUpdateStatus("Update blocked - backup running", true)
		}
		return
	}

	availableVersion := o.updateState.GetAvailableVersion()

	if o.menuUpdater != nil {
		o.menuUpdater.SetUpdateStatus("Downloading update...", false)
	}

	slog.Info("Installing update", "version", availableVersion)

	if err := o.updater.DownloadAndApply(ctx); err != nil {
		slog.Error("Update failed", "error", err)
		if o.menuUpdater != nil {
			o.menuUpdater.SetUpdateStatus("Update failed - click to retry", true)
		}
		return
	}

	// Update succeeded
	slog.Info("Update installed successfully, restarting...")
	if o.menuUpdater != nil {
		o.menuUpdater.SetUpdateStatus("Update installed - restarting...", false)
	}

	// Record successful update
	o.state.SetLastUpdateSuccess(availableVersion, time.Now())
	if err := o.state.Save(); err != nil {
		slog.Error("Error saving state", "error", err)
	}

	// Restart the application
	if err := Restart(); err != nil {
		slog.Error("Restart failed", "error", err)
		if o.menuUpdater != nil {
			o.menuUpdater.SetUpdateStatus("Updated - please restart manually", true)
		}
	}
}

// HandleUpdateClick handles a click on the update menu item.
// If an update is available, it installs it. Otherwise, it checks for updates.
func (o *UpdateOrchestrator) HandleUpdateClick(ctx context.Context) {
	if o.updateState.HasUpdate() {
		go o.Install(ctx)
	} else {
		go o.ManualCheck(ctx)
	}
}
