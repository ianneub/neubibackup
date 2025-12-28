package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"neubibackup/internal/autostart"
	"neubibackup/internal/backup"
	"neubibackup/internal/config"
	"neubibackup/internal/logging"
	"neubibackup/internal/power"
	"neubibackup/internal/restic"
	"neubibackup/internal/scheduler"
	"neubibackup/internal/state"
	"neubibackup/internal/tray"
	"neubibackup/internal/updater"

	"github.com/fsnotify/fsnotify"
)

// App is the main application struct that manages all application state and lifecycle.
type App struct {
	// Config & state
	cfg     *config.Config
	state   *state.State
	version string

	// Managers
	sched         *scheduler.Scheduler
	autostartMgr  *autostart.Manager
	powerWatcher  *power.Watcher
	configWatcher *fsnotify.Watcher
	appUpdater    *updater.Updater

	// Tickers
	statusTicker *time.Ticker
	updateTicker *time.Ticker

	// Internal state
	backupState *BackupState
	updateState *UpdateState

	// Context
	ctx    context.Context
	cancel context.CancelFunc

	// UI
	menu *tray.Menu

	// Callbacks for systray operations
	onIconUpdate func([]byte)
	onQuit       func()
	setTooltip   func(string)

	// Cleanup function for app log
	cleanupAppLog func()
}

// Option is a functional option for configuring the App.
type Option func(*App)

// WithOnIconUpdate sets the callback for updating the system tray icon.
func WithOnIconUpdate(fn func([]byte)) Option {
	return func(a *App) {
		a.onIconUpdate = fn
	}
}

// WithOnQuit sets the callback for quitting the application.
func WithOnQuit(fn func()) Option {
	return func(a *App) {
		a.onQuit = fn
	}
}

// WithSetTooltip sets the callback for updating the system tray tooltip.
func WithSetTooltip(fn func(string)) Option {
	return func(a *App) {
		a.setTooltip = fn
	}
}

// New creates a new App instance with the given version and options.
func New(version string, opts ...Option) *App {
	a := &App{
		version:     version,
		backupState: NewBackupState(),
		updateState: NewUpdateState(),
		// Default no-op callbacks
		onIconUpdate: func([]byte) {},
		onQuit:       func() {},
		setTooltip:   func(string) {},
	}

	for _, opt := range opts {
		opt(a)
	}

	return a
}

// Initialize sets up the application. This should be called from systray's onReady callback.
func (a *App) Initialize() error {
	a.ctx, a.cancel = context.WithCancel(context.Background())

	// Set up persistent application logging
	cleanup, err := logging.SetupAppLog()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not setup app log: %v\n", err)
	} else {
		a.cleanupAppLog = cleanup
	}

	log.Printf("NeubiBackup starting, version %s", a.version)

	// Platform-specific cleanup
	cleanupOldUpdates()
	cleanupOldAutostartShortcut()

	// Initialize autostart manager
	a.autostartMgr, err = autostart.New()
	if err != nil {
		log.Printf("Warning: could not initialize autostart: %v", err)
	}

	// Load state early (needed for macOS FDA check)
	a.state, err = state.Load()
	if err != nil {
		log.Printf("Error loading state: %v", err)
		a.state = &state.State{}
	}

	// macOS: Prompt for Full Disk Access if not already granted
	a.handleMacOSFirstRun()

	// Check for first run
	exists, err := config.ConfigExists()
	if err != nil {
		log.Printf("Error checking config: %v", err)
	}

	if !exists {
		// First run: create config and open in editor
		if err := a.handleFirstRun(); err != nil {
			log.Printf("Error during first run setup: %v", err)
		}
	} else {
		// Load existing config
		a.cfg, err = config.Load()
		if err != nil {
			log.Printf("Error loading config: %v", err)
		}
	}

	// Set initial icon based on configuration state
	a.updateIcon()
	a.setTooltip("NeubiBackup")

	// Build menu
	a.menu = tray.NewMenu(tray.MenuConfig{
		Version:       a.version,
		ResticVersion: restic.Version,
		AppState:      func() *state.State { return a.state },
		BackupState:   a.backupState,
		UpdateState:   a.updateState,
		IsConfigured:  func() bool { return a.cfg != nil && a.cfg.IsConfigured() },
		Autostart:     a.autostartMgr,
		OnBackupNow:   a.TriggerBackup,
		OnStopBackup:  a.StopBackup,
		OnOpenConfig:  a.openConfig,
		OnOpenLogs:    a.openLogs,
		OnOpenAppLog:  a.openAppLog,
		OnUpdateClick: a.handleUpdateClick,
		OnQuit:        a.onQuit,
	})

	// Initialize updater
	a.appUpdater = updater.New(a.version, "ianneub", "neubibackup")

	return nil
}

// Run starts the background goroutines. Call this after Initialize().
func (a *App) Run() error {
	// Start config file watcher
	go a.watchConfigFile()

	// Initialize scheduler if configured
	if a.cfg != nil && a.cfg.IsConfigured() {
		a.initScheduler()
	}

	// Start power watcher
	a.powerWatcher = power.New(a.onSystemWake)
	a.powerWatcher.Start()

	// Start status refresh ticker (updates "Last backup: X minutes ago" display)
	a.statusTicker = time.NewTicker(1 * time.Minute)
	go func() {
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-a.statusTicker.C:
				a.updateStatus()
			}
		}
	}()

	// Check for updates on startup if needed
	go a.checkForUpdatesIfNeeded()

	// Start background update checker (every 24 hours)
	a.updateTicker = time.NewTicker(24 * time.Hour)
	go func() {
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-a.updateTicker.C:
				a.checkForUpdates()
			}
		}
	}()

	return nil
}

// Shutdown cleans up all resources. Call this from systray's onExit callback.
func (a *App) Shutdown() {
	log.Println("NeubiBackup exiting...")

	// Cancel app context
	if a.cancel != nil {
		a.cancel()
	}

	// Cancel any running backup
	a.backupState.StopBackup()

	// Stop power watcher
	if a.powerWatcher != nil {
		a.powerWatcher.Stop()
	}

	// Close config watcher
	if a.configWatcher != nil {
		a.configWatcher.Close()
	}

	// Stop status ticker
	if a.statusTicker != nil {
		a.statusTicker.Stop()
	}

	// Stop update ticker
	if a.updateTicker != nil {
		a.updateTicker.Stop()
	}

	// Cleanup app log
	if a.cleanupAppLog != nil {
		a.cleanupAppLog()
	}
}

// handleFirstRun creates the config file and opens it in the editor.
func (a *App) handleFirstRun() error {
	log.Println("First run detected, creating config file...")

	// Create config directory and default config
	if err := config.WriteDefaultConfig(); err != nil {
		return err
	}

	// Open config in editor
	configPath, err := config.GetConfigPath()
	if err != nil {
		return err
	}

	log.Printf("Opening config file: %s", configPath)
	if err := config.OpenInEditor(configPath); err != nil {
		log.Printf("Warning: could not open config in editor: %v", err)
	}

	// Load the new (unconfigured) config
	a.cfg, err = config.Load()
	if err != nil {
		return err
	}

	return nil
}

// Menu callback functions

func (a *App) openConfig() {
	configPath, err := config.GetConfigPath()
	if err != nil {
		log.Printf("Error getting config path: %v", err)
		return
	}
	if err := config.OpenInEditor(configPath); err != nil {
		log.Printf("Error opening config: %v", err)
	}
}

func (a *App) openLogs() {
	logsDir, err := config.GetLogsDir()
	if err != nil {
		log.Printf("Error getting logs dir: %v", err)
		return
	}
	if err := config.OpenFolder(logsDir); err != nil {
		log.Printf("Error opening logs folder: %v", err)
	}
}

func (a *App) openAppLog() {
	appLogPath, err := logging.GetAppLogPath()
	if err != nil {
		log.Printf("Error getting app log path: %v", err)
		return
	}
	if err := config.OpenInEditor(appLogPath); err != nil {
		log.Printf("Error opening app log: %v", err)
	}
}

func (a *App) handleUpdateClick() {
	if a.updateState.HasUpdate() {
		go a.installUpdate()
	} else {
		go a.manualUpdateCheck()
	}
}

func (a *App) initScheduler() {
	var err error
	a.sched, err = scheduler.New(a.cfg, a.state, a.runBackup)
	if err != nil {
		log.Printf("Error creating scheduler: %v", err)
		return
	}

	go a.sched.Start(a.ctx)
	log.Println("Scheduler started")
}

func (a *App) onSystemWake() {
	if a.sched != nil {
		a.sched.OnWake()
	}
}

// TriggerBackup starts a backup if one is not already running.
func (a *App) TriggerBackup() {
	if a.backupState.IsRunning() {
		log.Println("Backup already running")
		return
	}

	go a.runBackup()
}

// StopBackup cancels the currently running backup.
func (a *App) StopBackup() {
	if !a.backupState.IsRunning() {
		log.Println("No backup running")
		return
	}

	log.Println("Stopping backup...")
	a.backupState.StopBackup()
}

func (a *App) runBackup() {
	ctx, err := a.backupState.StartBackup(a.ctx)
	if err != nil {
		log.Println("Backup already running")
		return
	}

	defer func() {
		a.backupState.Reset()
		a.updateStatus()
		a.updateIcon()
	}()

	// Update UI - show Stop Backup, hide Backup Now
	a.updateStatus()
	a.updateIcon()
	if a.menu != nil {
		a.menu.SetBackupRunning(true)
	}

	defer func() {
		// Restore menu - show Backup Now, hide Stop Backup
		if a.menu != nil {
			a.menu.SetBackupRunning(false)
		}
	}()

	// Create Tailscale provider if enabled
	var tailscaleProvider backup.TailscaleProvider
	if a.cfg.IsTailscaleEnabled() {
		tsDir, err := config.GetTailscaleDir()
		if err != nil {
			log.Printf("Failed to get Tailscale directory: %v", err)
		} else {
			tailscaleProvider = backup.NewTailscaleAdapter(&a.cfg.Tailscale, tsDir)
		}
	}

	// Create notifier
	notifier := backup.NewCompositeNotifier(backup.NotifierConfig{
		Healthchecks: a.cfg.Healthchecks,
		Pushover:     a.cfg.Pushover,
	})

	// Progress callback for UI updates
	onProgress := func(progress restic.BackupProgress) {
		a.backupState.SetProgress(&progress)
		a.updateStatus()
	}

	// Create and run orchestrator
	orchestrator := backup.NewOrchestrator(a.cfg, a.state,
		backup.WithNotifier(notifier),
		backup.WithTailscale(tailscaleProvider),
		backup.WithProgressCallback(onProgress),
	)

	result := orchestrator.Run(ctx)

	if result.Cancelled {
		log.Println("Backup was cancelled by user")
		return
	}

	if !result.Success {
		log.Printf("Backup failed: %v", result.Error)
	}
}

func (a *App) updateStatus() {
	if a.menu != nil {
		a.menu.UpdateStatus()
	}
}

func (a *App) updateIcon() {
	iconState := tray.DetermineIconState(
		a.backupState.IsRunning(),
		a.cfg != nil && a.cfg.IsConfigured(),
		a.state.ConsecutiveFailures,
		!a.state.LastBackupSuccess.IsZero(),
	)
	a.onIconUpdate(tray.GetIconBytes(iconState))
}

func (a *App) watchConfigFile() {
	configPath, err := config.GetConfigPath()
	if err != nil {
		log.Printf("Error getting config path for watcher: %v", err)
		return
	}

	a.configWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Error creating config watcher: %v", err)
		return
	}

	if err := a.configWatcher.Add(configPath); err != nil {
		log.Printf("Error watching config file: %v", err)
		return
	}

	for {
		select {
		case <-a.ctx.Done():
			return
		case event, ok := <-a.configWatcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Println("Config file changed, reloading...")
				a.ReloadConfig()
			}
		case err, ok := <-a.configWatcher.Errors:
			if !ok {
				return
			}
			log.Printf("Config watcher error: %v", err)
		}
	}
}

// ReloadConfig reloads the configuration from disk.
func (a *App) ReloadConfig() {
	// Stop any running backup before reloading config
	if a.backupState.IsRunning() {
		log.Println("Stopping running backup due to config change...")
		a.backupState.StopBackup()
	}

	newCfg, err := config.Load()
	if err != nil {
		log.Printf("Error reloading config: %v", err)
		return
	}

	a.cfg = newCfg

	// Update scheduler if it exists
	if a.sched != nil {
		if err := a.sched.UpdateConfig(a.cfg); err != nil {
			log.Printf("Error updating scheduler config: %v", err)
		}
	} else if a.cfg.IsConfigured() {
		// Start scheduler if config is now valid
		a.initScheduler()
	}

	// Update UI
	a.updateIcon()
	if a.menu != nil {
		a.menu.RefreshOnConfigChange()
	}

	log.Println("Config reloaded successfully")
}

// Update checking functions

func (a *App) checkForUpdatesIfNeeded() {
	// Skip if we checked recently (within 24 hours)
	if time.Since(a.state.LastUpdateCheck) < 24*time.Hour {
		log.Println("Skipping update check - checked recently")
		return
	}
	a.checkForUpdates()
}

func (a *App) manualUpdateCheck() {
	if a.menu != nil {
		a.menu.SetUpdateStatus("Checking for updates...", false)
	}

	a.checkForUpdates()

	// Re-enable the menu item if no update was found
	if !a.updateState.HasUpdate() && a.menu != nil {
		a.menu.SetUpdateStatus("Check for Updates", true)
	}
}

func (a *App) checkForUpdates() {
	log.Println("Checking for updates...")

	newVersion, available, err := a.appUpdater.CheckForUpdate(a.ctx)
	if err != nil {
		log.Printf("Update check failed: %v", err)
		return
	}

	// Record the check time
	a.state.LastUpdateCheck = time.Now()
	if err := a.state.Save(); err != nil {
		log.Printf("Error saving state: %v", err)
	}

	if available {
		a.updateState.SetAvailableVersion(newVersion)
		if a.menu != nil {
			a.menu.SetUpdateStatus(fmt.Sprintf("Update Available (%s)", newVersion), true)
		}
		log.Printf("Update available: %s", newVersion)

		// Trigger automatic update in background
		go a.attemptAutoUpdate(newVersion)
	} else {
		log.Println("No update available")
	}
}

// attemptAutoUpdate tries to apply an update automatically in the background.
// It waits for any running backup to complete before applying.
func (a *App) attemptAutoUpdate(version string) {
	if !a.updateState.TryStartUpdate() {
		log.Println("Auto-update: already in progress, skipping")
		return
	}

	defer a.updateState.FinishUpdate()

	// Wait for backup to complete if running (with timeout)
	waitStart := time.Now()
	maxWait := 2 * time.Hour

	for {
		if !a.backupState.IsRunning() {
			break
		}

		if time.Since(waitStart) > maxWait {
			log.Println("Auto-update: gave up waiting for backup to complete")
			return
		}

		log.Println("Auto-update: waiting for backup to complete...")
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(30 * time.Second):
			// Continue waiting
		}
	}

	// Check if we're still supposed to run
	select {
	case <-a.ctx.Done():
		return
	default:
	}

	// Apply the update
	log.Printf("Auto-update: applying update to %s...", version)
	if a.menu != nil {
		a.menu.SetUpdateStatus("Updating...", false)
	}

	if err := a.appUpdater.DownloadAndApply(a.ctx); err != nil {
		log.Printf("Auto-update failed: %v", err)
		if a.menu != nil {
			a.menu.SetUpdateStatus(fmt.Sprintf("Update failed (%s)", version), true)
		}

		// Record error in state
		a.state.LastUpdateError = err.Error()
		a.state.LastUpdateErrorTime = time.Now()
		if saveErr := a.state.Save(); saveErr != nil {
			log.Printf("Error saving state: %v", saveErr)
		}
		return
	}

	// Update succeeded
	log.Printf("Auto-update: successfully updated to %s, restarting...", version)

	// Record successful update
	a.state.LastUpdateVersion = version
	a.state.LastUpdateTime = time.Now()
	a.state.LastUpdateError = ""
	if err := a.state.Save(); err != nil {
		log.Printf("Error saving state: %v", err)
	}

	// Restart the application
	if err := updater.Restart(); err != nil {
		log.Printf("Auto-update: restart failed: %v", err)
		if a.menu != nil {
			a.menu.SetUpdateStatus("Updated - please restart manually", true)
		}
	}
}

func (a *App) installUpdate() {
	// Prevent update during backup
	if a.backupState.IsRunning() {
		log.Println("Cannot update while backup is running")
		if a.menu != nil {
			a.menu.SetUpdateStatus("Update blocked - backup running", true)
		}
		return
	}

	availableVersion := a.updateState.GetAvailableVersion()

	if a.menu != nil {
		a.menu.SetUpdateStatus("Downloading update...", false)
	}

	log.Printf("Installing update to %s...", availableVersion)

	if err := a.appUpdater.DownloadAndApply(a.ctx); err != nil {
		log.Printf("Update failed: %v", err)
		if a.menu != nil {
			a.menu.SetUpdateStatus("Update failed - click to retry", true)
		}
		return
	}

	// Update succeeded
	log.Println("Update installed successfully, restarting...")
	if a.menu != nil {
		a.menu.SetUpdateStatus("Update installed - restarting...", false)
	}

	// Record successful update
	a.state.LastUpdateVersion = availableVersion
	a.state.LastUpdateTime = time.Now()
	a.state.LastUpdateError = ""
	if err := a.state.Save(); err != nil {
		log.Printf("Error saving state: %v", err)
	}

	// Restart the application
	if err := updater.Restart(); err != nil {
		log.Printf("Restart failed: %v", err)
		if a.menu != nil {
			a.menu.SetUpdateStatus("Updated - please restart manually", true)
		}
	}
}

// IsBackupRunning returns whether a backup is currently running.
func (a *App) IsBackupRunning() bool {
	return a.backupState.IsRunning()
}

// Platform-specific cleanup functions

// cleanupOldUpdates removes old update artifacts left by go-selfupdate on Windows.
// On Windows, the old executable is renamed to .old rather than deleted.
func cleanupOldUpdates() {
	if runtime.GOOS != "windows" {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		return
	}

	dir := filepath.Dir(exe)
	pattern := filepath.Join(dir, ".*.old")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}

	for _, old := range matches {
		if err := os.Remove(old); err != nil {
			// File might still be locked, that's OK - we'll try again next time
			log.Printf("Could not remove old update file %s: %v", old, err)
		} else {
			log.Printf("Removed old update file: %s", old)
		}
	}
}

// cleanupOldAutostartShortcut removes the old Startup folder shortcut on Windows.
// Previous versions used go-autostart which creates a .lnk file in the Startup folder,
// but that doesn't work with apps requiring admin privileges. We now use Task Scheduler.
func cleanupOldAutostartShortcut() {
	if runtime.GOOS != "windows" {
		return
	}

	appData := os.Getenv("APPDATA")
	if appData == "" {
		return
	}

	shortcutPath := filepath.Join(appData,
		"Microsoft", "Windows", "Start Menu", "Programs", "Startup",
		"NeubiBackup.lnk")

	if _, err := os.Stat(shortcutPath); err == nil {
		if err := os.Remove(shortcutPath); err != nil {
			log.Printf("Warning: could not remove old autostart shortcut: %v", err)
		} else {
			log.Println("Removed old autostart shortcut from Startup folder")
		}
	}
}
