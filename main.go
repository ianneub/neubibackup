package main

import (
	"log"

	"neubibackup/assets"
	"neubibackup/internal/config"

	"github.com/getlantern/systray"
)

// version is set at build time via ldflags
var version = "dev"

// Application state
var (
	cfg          *config.Config
	isConfigured bool
)

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
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
		} else {
			isConfigured = cfg.IsConfigured()
		}
	}

	// Set initial icon based on configuration state
	if isConfigured {
		systray.SetIcon(assets.IconIdle)
	} else {
		systray.SetIcon(assets.IconError) // Red to indicate action needed
	}
	systray.SetTooltip("NeubiBackup")

	// Build menu
	setupMenu()
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

	isConfigured = false
	return nil
}

func setupMenu() {
	// Status line
	var mStatus *systray.MenuItem
	if !isConfigured {
		mStatus = systray.AddMenuItem("⚠️ Configuration required...", "Please edit config.yaml")
	} else {
		mStatus = systray.AddMenuItem("Status: Not yet backed up", "Backup status")
	}
	mStatus.Disable()

	systray.AddSeparator()

	// Backup Now
	mBackupNow := systray.AddMenuItem("Backup Now", "Start a backup immediately")
	if !isConfigured {
		mBackupNow.Disable()
	}

	systray.AddSeparator()

	// Open Config File
	mOpenConfig := systray.AddMenuItem("Open Config File", "Edit configuration")

	// Open Logs Folder
	mOpenLogs := systray.AddMenuItem("Open Logs Folder", "View backup logs")

	systray.AddSeparator()

	// Start at Login (placeholder - will be implemented in Phase 7)
	// On macOS, checked items show a checkmark (✓) prefix, not a checkbox
	mAutostart := systray.AddMenuItemCheckbox("Start at Login", "Launch at login", false)

	systray.AddSeparator()

	// Version
	mVersion := systray.AddMenuItem("Version "+version, "")
	mVersion.Disable()

	// Quit
	mQuit := systray.AddMenuItem("Quit", "Quit NeubiBackup")

	// Handle menu clicks
	go func() {
		for {
			select {
			case <-mBackupNow.ClickedCh:
				log.Println("Backup Now clicked")
				// TODO: implement in Phase 4

			case <-mAutostart.ClickedCh:
				// Toggle the checkbox state (visual only for now)
				if mAutostart.Checked() {
					mAutostart.Uncheck()
					log.Println("Start at Login: disabled")
				} else {
					mAutostart.Check()
					log.Println("Start at Login: enabled")
				}
				// TODO: implement actual autostart in Phase 7

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

func onExit() {
	log.Println("NeubiBackup exiting...")
}
