package main

import (
	"context"
	"io"
	"log"
	"os"
	"sync"

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
	"neubibackup/internal/tray"

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

	// Context for graceful shutdown
	appCtx    context.Context
	appCancel context.CancelFunc

	// Backup state
	backupMu      sync.Mutex
	backupRunning bool
	backupCancel  context.CancelFunc

	// Menu items for dynamic updates
	mStatus     *systray.MenuItem
	mBackupNow  *systray.MenuItem
	mAutostart  *systray.MenuItem
)

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	appCtx, appCancel = context.WithCancel(context.Background())

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

	// Initialize scheduler if configured
	if cfg != nil && cfg.IsConfigured() {
		initScheduler()
	}

	// Start power watcher
	powerWatcher = power.New(onSystemWake)
	powerWatcher.Start()
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
		backupMu.Unlock()
		updateStatus()
		updateIcon()
	}()

	// Update UI
	updateStatus()
	updateIcon()
	if mBackupNow != nil {
		mBackupNow.Disable()
	}

	defer func() {
		if mBackupNow != nil && cfg != nil && cfg.IsConfigured() {
			mBackupNow.Enable()
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

	// Run backup
	err = restic.RunBackup(ctx, cfg, logWriter)

	if err != nil {
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
	if mStatus != nil {
		mStatus.SetTitle(tray.FormatStatus(appState, backupRunning))
	}
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
	newCfg, err := config.Load()
	if err != nil {
		log.Printf("Error reloading config: %v", err)
		return
	}

	cfg = newCfg

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

	// Stop power watcher
	if powerWatcher != nil {
		powerWatcher.Stop()
	}

	// Close config watcher
	if configWatcher != nil {
		configWatcher.Close()
	}
}
