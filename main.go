package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"neubibackup/internal/app"
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
	"github.com/getlantern/systray"
)

// version is set at build time via ldflags
var version = "dev"

// Application state
var (
	cfg           *config.Config
	appState      *state.State
	sched         *scheduler.Scheduler
	autostartMgr  *autostart.Manager
	powerWatcher  *power.Watcher
	configWatcher *fsnotify.Watcher
	statusTicker  *time.Ticker

	// Context for graceful shutdown
	appCtx    context.Context
	appCancel context.CancelFunc

	// Thread-safe backup state
	backupState = app.NewBackupState()

	// System tray menu
	menu *tray.Menu

	// Updater state
	appUpdater   *updater.Updater
	updateTicker *time.Ticker

	// Thread-safe update state
	updateState = app.NewUpdateState()
)

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	appCtx, appCancel = context.WithCancel(context.Background())

	// Set up persistent application logging (especially useful on Windows with -H windowsgui)
	cleanupAppLog, err := logging.SetupAppLog()
	if err != nil {
		// Can't log this error to app log, but continue anyway
		fmt.Fprintf(os.Stderr, "Warning: could not setup app log: %v\n", err)
	} else {
		defer cleanupAppLog()
	}

	log.Printf("NeubiBackup starting, version %s", version)

	// Clean up old update artifacts on Windows
	cleanupOldUpdates()

	// Clean up old autostart shortcut on Windows (migration from go-autostart to Task Scheduler)
	cleanupOldAutostartShortcut()

	// Initialize autostart manager
	autostartMgr, err = autostart.New()
	if err != nil {
		log.Printf("Warning: could not initialize autostart: %v", err)
	}

	// Load state early (needed for macOS FDA check)
	appState, err = state.Load()
	if err != nil {
		log.Printf("Error loading state: %v", err)
		appState = &state.State{}
	}

	// macOS: Prompt for Full Disk Access if not already granted
	handleMacOSFirstRun()

	// Check for first run
	exists, err := config.ConfigExists()
	if err != nil {
		log.Printf("Error checking config: %v", err)
	}

	if !exists {
		// First run: create config and open in editor
		if err := handleFirstRun(); err != nil {
			log.Printf("Error during first run setup: %v", err)
		}
	} else {
		// Load existing config
		cfg, err = config.Load()
		if err != nil {
			log.Printf("Error loading config: %v", err)
		}
	}

	// Set initial icon based on configuration state
	updateIcon()
	systray.SetTooltip("NeubiBackup")

	// Build menu
	menu = tray.NewMenu(tray.MenuConfig{
		Version:       version,
		ResticVersion: restic.Version,
		AppState:      func() *state.State { return appState },
		BackupState:   backupState,
		UpdateState:   updateState,
		IsConfigured:  func() bool { return cfg != nil && cfg.IsConfigured() },
		Autostart:     autostartMgr,
		OnBackupNow:   triggerBackupNow,
		OnStopBackup:  stopBackup,
		OnOpenConfig:  openConfig,
		OnOpenLogs:    openLogs,
		OnOpenAppLog:  openAppLog,
		OnUpdateClick: handleUpdateClick,
		OnQuit:        func() { systray.Quit() },
	})

	// Start config file watcher
	go watchConfigFile()

	// Note: Tailscale is now connected on-demand during backup, not at startup

	// Initialize scheduler if configured
	if cfg != nil && cfg.IsConfigured() {
		initScheduler()
	}

	// Start power watcher
	powerWatcher = power.New(onSystemWake)
	powerWatcher.Start()

	// Start status refresh ticker (updates "Last backup: X minutes ago" display)
	statusTicker = time.NewTicker(1 * time.Minute)
	go func() {
		for {
			select {
			case <-appCtx.Done():
				return
			case <-statusTicker.C:
				updateStatus()
			}
		}
	}()

	// Initialize updater
	appUpdater = updater.New(version, "ianneub", "neubibackup")

	// Check for updates on startup if needed
	go checkForUpdatesIfNeeded()

	// Start background update checker (every 24 hours)
	updateTicker = time.NewTicker(24 * time.Hour)
	go func() {
		for {
			select {
			case <-appCtx.Done():
				return
			case <-updateTicker.C:
				checkForUpdates()
			}
		}
	}()
}

func handleFirstRun() error {
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
	cfg, err = config.Load()
	if err != nil {
		return err
	}

	return nil
}

// Menu callback functions

func openConfig() {
	configPath, err := config.GetConfigPath()
	if err != nil {
		log.Printf("Error getting config path: %v", err)
		return
	}
	if err := config.OpenInEditor(configPath); err != nil {
		log.Printf("Error opening config: %v", err)
	}
}

func openLogs() {
	logsDir, err := config.GetLogsDir()
	if err != nil {
		log.Printf("Error getting logs dir: %v", err)
		return
	}
	if err := config.OpenFolder(logsDir); err != nil {
		log.Printf("Error opening logs folder: %v", err)
	}
}

func openAppLog() {
	appLogPath, err := logging.GetAppLogPath()
	if err != nil {
		log.Printf("Error getting app log path: %v", err)
		return
	}
	if err := config.OpenInEditor(appLogPath); err != nil {
		log.Printf("Error opening app log: %v", err)
	}
}

func handleUpdateClick() {
	if updateState.HasUpdate() {
		go installUpdate()
	} else {
		go manualUpdateCheck()
	}
}

func initScheduler() {
	var err error
	sched, err = scheduler.New(cfg, appState, runBackup)
	if err != nil {
		log.Printf("Error creating scheduler: %v", err)
		return
	}

	go sched.Start(appCtx)
	log.Println("Scheduler started")
}

func onSystemWake() {
	if sched != nil {
		sched.OnWake()
	}
}

func triggerBackupNow() {
	if backupState.IsRunning() {
		log.Println("Backup already running")
		return
	}

	go func() {
		runBackup()
	}()
}

func stopBackup() {
	if !backupState.IsRunning() {
		log.Println("No backup running")
		return
	}

	log.Println("Stopping backup...")
	backupState.StopBackup()
}

func runBackup() {
	ctx, err := backupState.StartBackup(appCtx)
	if err != nil {
		log.Println("Backup already running")
		return
	}

	defer func() {
		backupState.Reset()
		updateStatus()
		updateIcon()
	}()

	// Update UI - show Stop Backup, hide Backup Now
	updateStatus()
	updateIcon()
	if menu != nil {
		menu.SetBackupRunning(true)
	}

	defer func() {
		// Restore menu - show Backup Now, hide Stop Backup
		if menu != nil {
			menu.SetBackupRunning(false)
		}
	}()

	// Create Tailscale provider if enabled
	var tailscaleProvider backup.TailscaleProvider
	if cfg.IsTailscaleEnabled() {
		tsDir, err := config.GetTailscaleDir()
		if err != nil {
			log.Printf("Failed to get Tailscale directory: %v", err)
		} else {
			tailscaleProvider = backup.NewTailscaleAdapter(&cfg.Tailscale, tsDir)
		}
	}

	// Create notifier
	notifier := backup.NewCompositeNotifier(backup.NotifierConfig{
		Healthchecks: cfg.Healthchecks,
		Pushover:     cfg.Pushover,
	})

	// Progress callback for UI updates
	onProgress := func(progress restic.BackupProgress) {
		backupState.SetProgress(&progress)
		updateStatus()
	}

	// Create and run orchestrator
	orchestrator := backup.NewOrchestrator(cfg, appState,
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

func updateStatus() {
	if menu != nil {
		menu.UpdateStatus()
	}
}

func updateIcon() {
	iconState := tray.DetermineIconState(
		backupState.IsRunning(),
		cfg != nil && cfg.IsConfigured(),
		appState.ConsecutiveFailures,
		!appState.LastBackupSuccess.IsZero(),
	)
	systray.SetIcon(tray.GetIconBytes(iconState))
}

func watchConfigFile() {
	configPath, err := config.GetConfigPath()
	if err != nil {
		log.Printf("Error getting config path for watcher: %v", err)
		return
	}

	configWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Error creating config watcher: %v", err)
		return
	}

	if err := configWatcher.Add(configPath); err != nil {
		log.Printf("Error watching config file: %v", err)
		return
	}

	for {
		select {
		case <-appCtx.Done():
			return
		case event, ok := <-configWatcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Println("Config file changed, reloading...")
				reloadConfig()
			}
		case err, ok := <-configWatcher.Errors:
			if !ok {
				return
			}
			log.Printf("Config watcher error: %v", err)
		}
	}
}

func reloadConfig() {
	// Stop any running backup before reloading config
	if backupState.IsRunning() {
		log.Println("Stopping running backup due to config change...")
		backupState.StopBackup()
	}

	newCfg, err := config.Load()
	if err != nil {
		log.Printf("Error reloading config: %v", err)
		return
	}

	cfg = newCfg

	// Note: Tailscale is now connected on-demand during backup,
	// so config changes take effect on the next backup run

	// Update scheduler if it exists
	if sched != nil {
		if err := sched.UpdateConfig(cfg); err != nil {
			log.Printf("Error updating scheduler config: %v", err)
		}
	} else if cfg.IsConfigured() {
		// Start scheduler if config is now valid
		initScheduler()
	}

	// Update UI
	updateIcon()
	if menu != nil {
		menu.RefreshOnConfigChange()
	}

	log.Println("Config reloaded successfully")
}

// Update checking functions

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

func checkForUpdatesIfNeeded() {
	// Skip if we checked recently (within 24 hours)
	if time.Since(appState.LastUpdateCheck) < 24*time.Hour {
		log.Println("Skipping update check - checked recently")
		return
	}
	checkForUpdates()
}

func manualUpdateCheck() {
	if menu != nil {
		menu.SetUpdateStatus("Checking for updates...", false)
	}

	checkForUpdates()

	// Re-enable the menu item if no update was found
	if !updateState.HasUpdate() && menu != nil {
		menu.SetUpdateStatus("Check for Updates", true)
	}
}

func checkForUpdates() {
	log.Println("Checking for updates...")

	newVersion, available, err := appUpdater.CheckForUpdate(appCtx)
	if err != nil {
		log.Printf("Update check failed: %v", err)
		return
	}

	// Record the check time
	appState.LastUpdateCheck = time.Now()
	if err := appState.Save(); err != nil {
		log.Printf("Error saving state: %v", err)
	}

	if available {
		updateState.SetAvailableVersion(newVersion)
		if menu != nil {
			menu.SetUpdateStatus(fmt.Sprintf("Update Available (%s)", newVersion), true)
		}
		log.Printf("Update available: %s", newVersion)

		// Trigger automatic update in background
		go attemptAutoUpdate(newVersion)
	} else {
		log.Println("No update available")
	}
}

// attemptAutoUpdate tries to apply an update automatically in the background.
// It waits for any running backup to complete before applying.
func attemptAutoUpdate(version string) {
	if !updateState.TryStartUpdate() {
		log.Println("Auto-update: already in progress, skipping")
		return
	}

	defer updateState.FinishUpdate()

	// Wait for backup to complete if running (with timeout)
	waitStart := time.Now()
	maxWait := 2 * time.Hour

	for {
		if !backupState.IsRunning() {
			break
		}

		if time.Since(waitStart) > maxWait {
			log.Println("Auto-update: gave up waiting for backup to complete")
			return
		}

		log.Println("Auto-update: waiting for backup to complete...")
		select {
		case <-appCtx.Done():
			return
		case <-time.After(30 * time.Second):
			// Continue waiting
		}
	}

	// Check if we're still supposed to run
	select {
	case <-appCtx.Done():
		return
	default:
	}

	// Apply the update
	log.Printf("Auto-update: applying update to %s...", version)
	if menu != nil {
		menu.SetUpdateStatus("Updating...", false)
	}

	if err := appUpdater.DownloadAndApply(appCtx); err != nil {
		log.Printf("Auto-update failed: %v", err)
		if menu != nil {
			menu.SetUpdateStatus(fmt.Sprintf("Update failed (%s)", version), true)
		}

		// Record error in state
		appState.LastUpdateError = err.Error()
		appState.LastUpdateErrorTime = time.Now()
		if saveErr := appState.Save(); saveErr != nil {
			log.Printf("Error saving state: %v", saveErr)
		}
		return
	}

	// Update succeeded
	log.Printf("Auto-update: successfully updated to %s, restarting...", version)

	// Record successful update
	appState.LastUpdateVersion = version
	appState.LastUpdateTime = time.Now()
	appState.LastUpdateError = ""
	if err := appState.Save(); err != nil {
		log.Printf("Error saving state: %v", err)
	}

	// Restart the application
	if err := updater.Restart(); err != nil {
		log.Printf("Auto-update: restart failed: %v", err)
		if menu != nil {
			menu.SetUpdateStatus("Updated - please restart manually", true)
		}
	}
}

func installUpdate() {
	// Prevent update during backup
	if backupState.IsRunning() {
		log.Println("Cannot update while backup is running")
		if menu != nil {
			menu.SetUpdateStatus("Update blocked - backup running", true)
		}
		return
	}

	availableVersion := updateState.GetAvailableVersion()

	if menu != nil {
		menu.SetUpdateStatus("Downloading update...", false)
	}

	log.Printf("Installing update to %s...", availableVersion)

	if err := appUpdater.DownloadAndApply(appCtx); err != nil {
		log.Printf("Update failed: %v", err)
		if menu != nil {
			menu.SetUpdateStatus("Update failed - click to retry", true)
		}
		return
	}

	// Update succeeded
	log.Println("Update installed successfully, restarting...")
	if menu != nil {
		menu.SetUpdateStatus("Update installed - restarting...", false)
	}

	// Record successful update
	appState.LastUpdateVersion = availableVersion
	appState.LastUpdateTime = time.Now()
	appState.LastUpdateError = ""
	if err := appState.Save(); err != nil {
		log.Printf("Error saving state: %v", err)
	}

	// Restart the application
	if err := updater.Restart(); err != nil {
		log.Printf("Restart failed: %v", err)
		if menu != nil {
			menu.SetUpdateStatus("Updated - please restart manually", true)
		}
	}
}

func onExit() {
	log.Println("NeubiBackup exiting...")

	// Cancel app context
	if appCancel != nil {
		appCancel()
	}

	// Cancel any running backup
	backupState.StopBackup()

	// Stop power watcher
	if powerWatcher != nil {
		powerWatcher.Stop()
	}

	// Close config watcher
	if configWatcher != nil {
		configWatcher.Close()
	}

	// Stop status ticker
	if statusTicker != nil {
		statusTicker.Stop()
	}

	// Stop update ticker
	if updateTicker != nil {
		updateTicker.Stop()
	}
}
