package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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
	updateOrch    *updater.UpdateOrchestrator

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

	slog.Info("NeubiBackup starting", "version", a.version)

	// Platform-specific cleanup
	cleanupOldUpdates()
	cleanupOldAutostartShortcut()

	// Initialize autostart manager
	a.autostartMgr, err = autostart.New()
	if err != nil {
		slog.Warn("Could not initialize autostart", "error", err)
	}

	// Load state early (needed for macOS FDA check)
	a.state, err = state.Load()
	if err != nil {
		slog.Error("Loading state failed (backup history may be lost)", "error", err)
		a.state = &state.State{}
	}

	// macOS: Prompt for Full Disk Access if not already granted
	a.handleMacOSFirstRun()

	// Check for first run
	exists, err := config.ConfigExists()
	if err != nil {
		slog.Error("Error checking config", "error", err)
	}

	if !exists {
		// First run: create config and open in editor
		if err := a.handleFirstRun(); err != nil {
			slog.Error("Error during first run setup", "error", err)
		}
	} else {
		// Load existing config
		a.cfg, err = config.Load()
		if err != nil {
			slog.Error("Error loading config", "error", err)
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
		OnOpenConfig:   a.openConfig,
		OnOpenLogs:     a.openLogs,
		OnOpenAppLog:   a.openAppLog,
		OnUpdateClick:  a.handleUpdateClick,
		OnVersionClick: a.openProjectWebsite,
		OnQuit:         a.onQuit,
	})

	// Initialize updater and orchestrator
	a.appUpdater = updater.New(a.version, "ianneub", "neubibackup")
	// Note: updateOrch is initialized in Run() after menu is fully set up

	return nil
}

// Run starts the background goroutines. Call this after Initialize().
func (a *App) Run() error {
	// Initialize update orchestrator now that menu is ready
	a.updateOrch = updater.NewUpdateOrchestrator(
		a.appUpdater,
		a.state,
		a.backupState,
		a.updateState,
		a.menu,
	)

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
	go a.updateOrch.CheckIfNeeded(a.ctx)

	// Start background update checker (every 24 hours)
	a.updateTicker = time.NewTicker(24 * time.Hour)
	go func() {
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-a.updateTicker.C:
				a.updateOrch.Check(a.ctx)
			}
		}
	}()

	return nil
}

// Shutdown cleans up all resources. Call this from systray's onExit callback.
func (a *App) Shutdown() {
	slog.Info("NeubiBackup exiting...")

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
	slog.Info("First run detected, creating config file...")

	// Create config directory and default config
	if err := config.WriteDefaultConfig(); err != nil {
		return err
	}

	// Open config in editor
	configPath, err := config.GetConfigPath()
	if err != nil {
		return err
	}

	slog.Info("Opening config file", "path", configPath)
	if err := config.OpenInEditor(configPath); err != nil {
		slog.Warn("Could not open config in editor", "error", err)
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
		slog.Error("Error getting config path", "error", err)
		return
	}
	if err := config.OpenInEditor(configPath); err != nil {
		slog.Error("Error opening config", "error", err)
	}
}

func (a *App) openLogs() {
	logsDir, err := config.GetLogsDir()
	if err != nil {
		slog.Error("Error getting logs dir", "error", err)
		return
	}
	if err := config.OpenFolder(logsDir); err != nil {
		slog.Error("Error opening logs folder", "error", err)
	}
}

func (a *App) openAppLog() {
	appLogPath, err := logging.GetAppLogPath()
	if err != nil {
		slog.Error("Error getting app log path", "error", err)
		return
	}
	if err := config.OpenInEditor(appLogPath); err != nil {
		slog.Error("Error opening app log", "error", err)
	}
}

func (a *App) handleUpdateClick() {
	a.updateOrch.HandleUpdateClick(a.ctx)
}

func (a *App) openProjectWebsite() {
	if err := config.OpenURL("https://github.com/ianneub/neubibackup"); err != nil {
		slog.Error("Error opening project website", "error", err)
	}
}

func (a *App) initScheduler() {
	var err error
	a.sched, err = scheduler.New(a.cfg, a.state, a.runBackup)
	if err != nil {
		slog.Error("Error creating scheduler", "error", err)
		return
	}

	go a.sched.Start(a.ctx)
	slog.Info("Scheduler started")
}

func (a *App) onSystemWake() {
	if a.sched != nil {
		a.sched.OnWake()
	}
}

// TriggerBackup starts a backup if one is not already running.
// This is called for manual backups (via menu) and clears any retry pause.
func (a *App) TriggerBackup() {
	if a.backupState.IsRunning() {
		slog.Info("Backup already running")
		return
	}

	// Clear any pause so manual backup can proceed even after password errors
	a.state.ClearPause()

	go a.runBackup()
}

// StopBackup cancels the currently running backup.
func (a *App) StopBackup() {
	if !a.backupState.IsRunning() {
		slog.Info("No backup running")
		return
	}

	slog.Info("Stopping backup...")
	a.backupState.StopBackup()
}

func (a *App) runBackup() {
	ctx, err := a.backupState.StartBackup(a.ctx)
	if err != nil {
		slog.Info("Backup already running")
		return
	}

	// Update UI - show Stop Backup, hide Backup Now
	a.updateStatus()
	a.updateIcon()
	if a.menu != nil {
		a.menu.SetBackupRunning(true)
	}

	defer func() {
		a.backupState.Reset()
		a.updateStatus()
		a.updateIcon()
		if a.menu != nil {
			a.menu.SetBackupRunning(false)
		}
	}()

	// Create Tailscale provider if enabled
	var tailscaleProvider backup.TailscaleProvider
	if a.cfg.IsTailscaleEnabled() {
		tsDir, err := config.GetTailscaleDir()
		if err != nil {
			slog.Error("Failed to get Tailscale directory", "error", err)
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

	// Get location from scheduler for timezone-aware pause handling
	var loc *time.Location
	if a.sched != nil {
		loc = a.sched.Location()
	}
	if loc == nil {
		loc = time.Local
	}

	// Create and run orchestrator
	orchestrator := backup.NewOrchestrator(a.cfg, a.state,
		backup.WithNotifier(notifier),
		backup.WithTailscale(tailscaleProvider),
		backup.WithProgressCallback(onProgress),
		backup.WithLocation(loc),
	)

	result := orchestrator.Run(ctx)

	if result.Cancelled {
		slog.Info("Backup was cancelled by user")
		return
	}

	if !result.Success {
		slog.Error("Backup failed", "error", result.Error)
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
		a.state.Backup.ConsecutiveFailures,
		!a.state.Backup.LastSuccess.IsZero(),
	)
	a.onIconUpdate(tray.GetIconBytes(iconState))
}

func (a *App) watchConfigFile() {
	configPath, err := config.GetConfigPath()
	if err != nil {
		slog.Error("Error getting config path for watcher", "error", err)
		return
	}

	a.configWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		slog.Error("Error creating config watcher", "error", err)
		return
	}

	if err := a.configWatcher.Add(configPath); err != nil {
		slog.Error("Error watching config file", "error", err)
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
				slog.Info("Config file changed, reloading...")
				a.ReloadConfig()
			}
		case err, ok := <-a.configWatcher.Errors:
			if !ok {
				return
			}
			slog.Error("Config watcher error", "error", err)
		}
	}
}

// ReloadConfig reloads the configuration from disk.
func (a *App) ReloadConfig() {
	// Stop any running backup before reloading config
	if a.backupState.IsRunning() {
		slog.Info("Stopping running backup due to config change...")
		a.backupState.StopBackup()
	}

	newCfg, err := config.Load()
	if err != nil {
		slog.Error("Error reloading config", "error", err)
		return
	}

	a.cfg = newCfg

	// Clear any pause - user may have fixed the password or other config issue
	a.state.ClearPause()

	// Update scheduler if it exists
	if a.sched != nil {
		if err := a.sched.UpdateConfig(a.cfg); err != nil {
			slog.Error("Error updating scheduler config", "error", err)
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

	slog.Info("Config reloaded successfully")
}

// IsBackupRunning returns whether a backup is currently running.
func (a *App) IsBackupRunning() bool {
	return a.backupState.IsRunning()
}
