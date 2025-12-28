package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"neubibackup/assets"
	"neubibackup/internal/autostart"
	"neubibackup/internal/config"
	"neubibackup/internal/healthchecks"
	"neubibackup/internal/logging"
	"neubibackup/internal/power"
	"neubibackup/internal/pushover"
	"neubibackup/internal/restic"
	"neubibackup/internal/scheduler"
	"neubibackup/internal/state"
	"neubibackup/internal/tailscale"
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
	tailscaleMgr  *tailscale.Manager
	statusTicker  *time.Ticker

	// Context for graceful shutdown
	appCtx    context.Context
	appCancel context.CancelFunc

	// Backup state
	backupMu       sync.Mutex
	backupRunning  bool
	backupCancel   context.CancelFunc
	backupProgress *restic.BackupProgress

	// Menu items for dynamic updates
	mStatus       *systray.MenuItem
	mBackupNow    *systray.MenuItem
	mStopBackup   *systray.MenuItem
	mAutostart    *systray.MenuItem
	mUpdateStatus *systray.MenuItem

	// Updater state
	appUpdater       *updater.Updater
	availableVersion string
	updateTicker     *time.Ticker

	// Auto-update state
	updateMu         sync.Mutex
	updateInProgress bool
)

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	appCtx, appCancel = context.WithCancel(context.Background())

	// Clean up old update artifacts on Windows
	cleanupOldUpdates()

	// Initialize autostart manager
	var err error
	autostartMgr, err = autostart.New()
	if err != nil {
		log.Printf("Warning: could not initialize autostart: %v", err)
	}

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

	// Load state
	appState, err = state.Load()
	if err != nil {
		log.Printf("Error loading state: %v", err)
		appState = &state.State{}
	}

	// Set initial icon based on configuration state
	updateIcon()
	systray.SetTooltip("NeubiBackup")

	// Build menu
	setupMenu()

	// Start config file watcher
	go watchConfigFile()

	// Initialize Tailscale if configured
	if cfg != nil && cfg.IsTailscaleEnabled() {
		if err := initTailscale(); err != nil {
			log.Printf("Warning: Tailscale initialization failed: %v", err)
		}
	}

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

func initTailscale() error {
	tsDir, err := config.GetTailscaleDir()
	if err != nil {
		return err
	}

	tailscaleMgr, err = tailscale.New(&cfg.Tailscale, tsDir)
	if err != nil {
		return err
	}

	// Start with a timeout
	if err := tailscaleMgr.Start(appCtx); err != nil {
		tailscaleMgr = nil
		return err
	}

	return nil
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

func setupMenu() {
	isConfigured := cfg != nil && cfg.IsConfigured()

	// Status line
	if !isConfigured {
		mStatus = systray.AddMenuItem("⚠️ Configuration required...", "Please edit config.yaml")
	} else {
		mStatus = systray.AddMenuItem(tray.FormatStatus(appState, backupRunning), "Backup status")
	}
	mStatus.Disable()

	systray.AddSeparator()

	// Backup Now
	mBackupNow = systray.AddMenuItem("Backup Now", "Start a backup immediately")
	if !isConfigured {
		mBackupNow.Disable()
	}

	// Stop Backup (hidden by default)
	mStopBackup = systray.AddMenuItem("Stop Backup", "Stop the running backup")
	mStopBackup.Hide()

	systray.AddSeparator()

	// Open Config File
	mOpenConfig := systray.AddMenuItem("Open Config File", "Edit configuration")

	// Open Logs Folder
	mOpenLogs := systray.AddMenuItem("Open Logs Folder", "View backup logs")

	systray.AddSeparator()

	// Start at Login
	autostartEnabled := autostartMgr != nil && autostartMgr.IsEnabled()
	mAutostart = systray.AddMenuItemCheckbox("Start at Login", "Launch at login", autostartEnabled)

	systray.AddSeparator()

	// Update status
	mUpdateStatus = systray.AddMenuItem("Check for Updates", "Check for new versions")

	// Version
	mVersion := systray.AddMenuItem("Version "+version+" (restic "+restic.Version+")", "")
	mVersion.Disable()

	// Quit
	mQuit := systray.AddMenuItem("Quit", "Quit NeubiBackup")

	// Handle menu clicks
	go func() {
		for {
			select {
			case <-mBackupNow.ClickedCh:
				triggerBackupNow()

			case <-mStopBackup.ClickedCh:
				stopBackup()

			case <-mAutostart.ClickedCh:
				toggleAutostart()

			case <-mOpenConfig.ClickedCh:
				configPath, err := config.GetConfigPath()
				if err != nil {
					log.Printf("Error getting config path: %v", err)
					continue
				}
				if err := config.OpenInEditor(configPath); err != nil {
					log.Printf("Error opening config: %v", err)
				}

			case <-mOpenLogs.ClickedCh:
				logsDir, err := config.GetLogsDir()
				if err != nil {
					log.Printf("Error getting logs dir: %v", err)
					continue
				}
				if err := config.OpenFolder(logsDir); err != nil {
					log.Printf("Error opening logs folder: %v", err)
				}

			case <-mUpdateStatus.ClickedCh:
				if availableVersion != "" {
					// User clicked to install update
					go installUpdate()
				} else {
					// User clicked to check for updates
					go manualUpdateCheck()
				}

			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
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
	backupMu.Lock()
	if backupRunning {
		backupMu.Unlock()
		log.Println("Backup already running")
		return
	}
	backupRunning = true
	backupMu.Unlock()

	go func() {
		runBackup()
	}()
}

func stopBackup() {
	backupMu.Lock()
	defer backupMu.Unlock()

	if !backupRunning {
		log.Println("No backup running")
		return
	}

	if backupCancel != nil {
		log.Println("Stopping backup...")
		backupCancel()
	}
}

func runBackup() {
	backupMu.Lock()
	backupRunning = true
	var ctx context.Context
	ctx, backupCancel = context.WithCancel(appCtx)
	backupMu.Unlock()

	defer func() {
		backupMu.Lock()
		backupRunning = false
		backupCancel = nil
		backupProgress = nil
		backupMu.Unlock()
		updateStatus()
		updateIcon()
	}()

	// Update UI - show Stop Backup, hide Backup Now
	updateStatus()
	updateIcon()
	if mBackupNow != nil {
		mBackupNow.Hide()
	}
	if mStopBackup != nil {
		mStopBackup.Show()
	}

	defer func() {
		// Restore menu - show Backup Now, hide Stop Backup
		if mStopBackup != nil {
			mStopBackup.Hide()
		}
		if mBackupNow != nil {
			mBackupNow.Show()
			if cfg != nil && cfg.IsConfigured() {
				mBackupNow.Enable()
			}
		}
	}()

	log.Println("Starting backup...")

	// Create healthchecks client
	var hc *healthchecks.Client
	if cfg.Healthchecks.Enabled && cfg.Healthchecks.PingURL != "" {
		hc = healthchecks.New(cfg.Healthchecks.PingURL)
	}

	// Ping start
	if hc != nil {
		if err := hc.Start(); err != nil {
			log.Printf("Warning: healthchecks start ping failed: %v", err)
		}
	}

	// Create log file
	logFile, err := logging.CreateLogFile()
	if err != nil {
		log.Printf("Error creating log file: %v", err)
		recordFailure(err, hc)
		return
	}
	defer logFile.Close()

	// Write to both log file and stdout
	logWriter := io.MultiWriter(logFile, os.Stdout)

	// Record attempt time
	appState.LastBackupAttempt = appState.LastBackupSuccess // Will be updated after

	// Get Tailscale proxy address if available
	var proxyAddr string
	if tailscaleMgr != nil && tailscaleMgr.IsStarted() {
		proxyAddr = tailscaleMgr.ProxyAddr()
		if proxyAddr != "" {
			log.Printf("Using Tailscale proxy: %s", proxyAddr)
		}
	}

	// Progress callback for UI updates
	onProgress := func(progress restic.BackupProgress) {
		backupMu.Lock()
		backupProgress = &progress
		backupMu.Unlock()
		updateStatus()
	}

	// Run backup
	err = restic.RunBackup(ctx, cfg, logWriter, proxyAddr, onProgress)

	if err != nil {
		// Check if backup was manually cancelled by user
		if errors.Is(err, context.Canceled) {
			log.Println("Backup was cancelled by user")
			return
		}

		log.Printf("Backup failed: %v", err)
		recordFailure(err, hc)

		// Send logs on failure if configured
		if hc != nil && cfg.Healthchecks.SendLogsOnFailure {
			logFile.Seek(0, 0)
			logData, _ := io.ReadAll(logFile)
			hc.Fail(string(logData))
		} else if hc != nil {
			hc.Fail("")
		}

		// Send Pushover notification
		if cfg.Pushover.Enabled && cfg.Pushover.OnFailure {
			po := pushover.New(cfg.Pushover.APIToken, cfg.Pushover.UserKey)
			if err := po.SendFailure(err.Error()); err != nil {
				log.Printf("Warning: pushover notification failed: %v", err)
			}
		}

		return
	}

	// Success
	log.Println("Backup completed successfully")
	appState.RecordSuccess()
	if err := appState.Save(); err != nil {
		log.Printf("Error saving state: %v", err)
	}

	if hc != nil {
		if err := hc.Success(); err != nil {
			log.Printf("Warning: healthchecks success ping failed: %v", err)
		}
	}

	// Send success notification if configured
	if cfg.Pushover.Enabled && cfg.Pushover.OnSuccess {
		po := pushover.New(cfg.Pushover.APIToken, cfg.Pushover.UserKey)
		if err := po.SendSuccess("Backup completed successfully"); err != nil {
			log.Printf("Warning: pushover notification failed: %v", err)
		}
	}

	// Cleanup old logs
	if err := logging.CleanupOldLogs(); err != nil {
		log.Printf("Warning: log cleanup failed: %v", err)
	}
}

func recordFailure(err error, hc *healthchecks.Client) {
	appState.RecordFailure(err)
	if saveErr := appState.Save(); saveErr != nil {
		log.Printf("Error saving state: %v", saveErr)
	}
}

func updateStatus() {
	if mStatus == nil {
		return
	}

	backupMu.Lock()
	running := backupRunning
	progress := backupProgress
	backupMu.Unlock()

	var title string
	if running && progress != nil {
		title = tray.FormatProgress(progress)
	} else {
		title = tray.FormatStatus(appState, running)
	}

	mStatus.SetTitle(title)
}

func updateIcon() {
	if backupRunning {
		systray.SetIcon(assets.IconRunning)
		return
	}

	if cfg == nil || !cfg.IsConfigured() {
		systray.SetIcon(assets.IconError)
		return
	}

	if appState.ConsecutiveFailures > 0 {
		systray.SetIcon(assets.IconError)
		return
	}

	if !appState.LastBackupSuccess.IsZero() {
		systray.SetIcon(assets.IconSuccess)
		return
	}

	systray.SetIcon(assets.IconIdle)
}

func toggleAutostart() {
	if autostartMgr == nil {
		log.Println("Autostart not available")
		return
	}

	if err := autostartMgr.Toggle(); err != nil {
		log.Printf("Error toggling autostart: %v", err)
		return
	}

	if autostartMgr.IsEnabled() {
		mAutostart.Check()
		log.Println("Start at Login: enabled")
	} else {
		mAutostart.Uncheck()
		log.Println("Start at Login: disabled")
	}
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
	backupMu.Lock()
	if backupRunning && backupCancel != nil {
		log.Println("Stopping running backup due to config change...")
		backupCancel()
	}
	backupMu.Unlock()

	oldTsEnabled := cfg != nil && cfg.IsTailscaleEnabled()

	newCfg, err := config.Load()
	if err != nil {
		log.Printf("Error reloading config: %v", err)
		return
	}

	cfg = newCfg
	newTsEnabled := cfg.IsTailscaleEnabled()

	// Handle Tailscale config changes
	if !oldTsEnabled && newTsEnabled {
		// Tailscale newly enabled
		log.Println("Tailscale enabled, initializing...")
		if err := initTailscale(); err != nil {
			log.Printf("Warning: Tailscale initialization failed: %v", err)
		}
	} else if oldTsEnabled && !newTsEnabled {
		// Tailscale disabled
		log.Println("Tailscale disabled, shutting down...")
		if tailscaleMgr != nil {
			tailscaleMgr.Close()
			tailscaleMgr = nil
		}
	}
	// Note: Auth key changes require app restart

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
	updateStatus()

	if cfg.IsConfigured() {
		if mBackupNow != nil {
			mBackupNow.Enable()
		}
		if mStatus != nil {
			mStatus.SetTitle(tray.FormatStatus(appState, backupRunning))
		}
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

func checkForUpdatesIfNeeded() {
	// Skip if we checked recently (within 24 hours)
	if time.Since(appState.LastUpdateCheck) < 24*time.Hour {
		log.Println("Skipping update check - checked recently")
		return
	}
	checkForUpdates()
}

func manualUpdateCheck() {
	mUpdateStatus.SetTitle("Checking for updates...")
	mUpdateStatus.Disable()

	checkForUpdates()

	// Re-enable the menu item if no update was found
	if availableVersion == "" {
		mUpdateStatus.SetTitle("Check for Updates")
		mUpdateStatus.Enable()
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
		availableVersion = newVersion
		mUpdateStatus.SetTitle(fmt.Sprintf("Update Available (%s)", newVersion))
		mUpdateStatus.Enable()
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
	updateMu.Lock()
	if updateInProgress {
		updateMu.Unlock()
		log.Println("Auto-update: already in progress, skipping")
		return
	}
	updateInProgress = true
	updateMu.Unlock()

	defer func() {
		updateMu.Lock()
		updateInProgress = false
		updateMu.Unlock()
	}()

	// Wait for backup to complete if running (with timeout)
	waitStart := time.Now()
	maxWait := 2 * time.Hour

	for {
		backupMu.Lock()
		running := backupRunning
		backupMu.Unlock()

		if !running {
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
	mUpdateStatus.SetTitle("Updating...")
	mUpdateStatus.Disable()

	if err := appUpdater.DownloadAndApply(appCtx); err != nil {
		log.Printf("Auto-update failed: %v", err)
		mUpdateStatus.SetTitle(fmt.Sprintf("Update failed (%s)", version))
		mUpdateStatus.Enable()

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
		mUpdateStatus.SetTitle("Updated - please restart manually")
		mUpdateStatus.Enable()
	}
}

func installUpdate() {
	// Prevent update during backup
	backupMu.Lock()
	if backupRunning {
		backupMu.Unlock()
		log.Println("Cannot update while backup is running")
		mUpdateStatus.SetTitle("Update blocked - backup running")
		return
	}
	backupMu.Unlock()

	mUpdateStatus.SetTitle("Downloading update...")
	mUpdateStatus.Disable()

	log.Printf("Installing update to %s...", availableVersion)

	if err := appUpdater.DownloadAndApply(appCtx); err != nil {
		log.Printf("Update failed: %v", err)
		mUpdateStatus.SetTitle("Update failed - click to retry")
		mUpdateStatus.Enable()
		return
	}

	// Update succeeded
	log.Println("Update installed successfully, restarting...")
	mUpdateStatus.SetTitle("Update installed - restarting...")

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
		mUpdateStatus.SetTitle("Updated - please restart manually")
		mUpdateStatus.Enable()
	}
}

func onExit() {
	log.Println("NeubiBackup exiting...")

	// Cancel app context
	if appCancel != nil {
		appCancel()
	}

	// Cancel any running backup
	backupMu.Lock()
	if backupCancel != nil {
		backupCancel()
	}
	backupMu.Unlock()

	// Close Tailscale
	if tailscaleMgr != nil {
		if err := tailscaleMgr.Close(); err != nil {
			log.Printf("Warning: Tailscale shutdown error: %v", err)
		}
	}

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
