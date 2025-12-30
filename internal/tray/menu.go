package tray

import (
	"log/slog"

	"neubibackup/internal/restic"
	"neubibackup/internal/state"

	"github.com/getlantern/systray"
)

// BackupStateProvider provides backup state information.
type BackupStateProvider interface {
	IsRunning() bool
	GetProgress() *restic.BackupProgress
}

// UpdateStateProvider provides update state information.
type UpdateStateProvider interface {
	HasUpdate() bool
	GetAvailableVersion() string
}

// AutostartProvider provides autostart state and control.
type AutostartProvider interface {
	IsEnabled() bool
	Toggle() error
}

// MenuConfig configures the menu behavior.
type MenuConfig struct {
	Version       string
	ResticVersion string

	// State providers (read-only access)
	AppState     func() *state.State
	BackupState  BackupStateProvider
	UpdateState  UpdateStateProvider
	IsConfigured func() bool
	Autostart    AutostartProvider

	// Action callbacks
	OnBackupNow   func()
	OnStopBackup  func()
	OnOpenConfig  func()
	OnOpenLogs    func()
	OnOpenAppLog  func()
	OnUpdateClick func()
	OnQuit        func()
}

// Menu manages the system tray menu.
type Menu struct {
	cfg MenuConfig

	// Menu items (internal references for dynamic updates)
	mStatus       *systray.MenuItem
	mBackupNow    *systray.MenuItem
	mStopBackup   *systray.MenuItem
	mAutostart    *systray.MenuItem
	mUpdateStatus *systray.MenuItem
}

// NewMenu creates and initializes the system tray menu.
// It sets up all menu items and starts the click handler goroutine.
func NewMenu(cfg MenuConfig) *Menu {
	m := &Menu{cfg: cfg}
	m.setup()
	return m
}

// setup creates all menu items and starts the event loop.
func (m *Menu) setup() {
	isConfigured := m.cfg.IsConfigured()

	// Status line
	if !isConfigured {
		m.mStatus = systray.AddMenuItem("Configuration required...", "Please edit config.yaml")
	} else {
		m.mStatus = systray.AddMenuItem(FormatStatus(m.cfg.AppState(), m.cfg.BackupState.IsRunning()), "Backup status")
	}
	m.mStatus.Disable()

	systray.AddSeparator()

	// Backup Now
	m.mBackupNow = systray.AddMenuItem("Backup Now", "Start a backup immediately")
	if !isConfigured {
		m.mBackupNow.Disable()
	}

	// Stop Backup (hidden by default)
	m.mStopBackup = systray.AddMenuItem("Stop Backup", "Stop the running backup")
	m.mStopBackup.Hide()

	systray.AddSeparator()

	// Open Config File
	mOpenConfig := systray.AddMenuItem("Open Config File", "Edit configuration")

	// Open Logs Folder
	mOpenLogs := systray.AddMenuItem("Open Logs Folder", "View backup logs")

	// Open App Log
	mOpenAppLog := systray.AddMenuItem("Open App Log", "View application log")

	systray.AddSeparator()

	// Start at Login
	autostartEnabled := m.cfg.Autostart != nil && m.cfg.Autostart.IsEnabled()
	m.mAutostart = systray.AddMenuItemCheckbox("Start at Login", "Launch at login", autostartEnabled)

	systray.AddSeparator()

	// Update status
	m.mUpdateStatus = systray.AddMenuItem("Check for Updates", "Check for new versions")

	// Version
	mVersion := systray.AddMenuItem("Version "+m.cfg.Version+" (restic "+m.cfg.ResticVersion+")", "")
	mVersion.Disable()

	// Quit
	mQuit := systray.AddMenuItem("Quit", "Quit NeubiBackup")

	// Start event loop
	go m.eventLoop(mOpenConfig, mOpenLogs, mOpenAppLog, mQuit)
}

// eventLoop handles menu item clicks.
func (m *Menu) eventLoop(mOpenConfig, mOpenLogs, mOpenAppLog, mQuit *systray.MenuItem) {
	for {
		select {
		case <-m.mBackupNow.ClickedCh:
			if m.cfg.OnBackupNow != nil {
				m.cfg.OnBackupNow()
			}

		case <-m.mStopBackup.ClickedCh:
			if m.cfg.OnStopBackup != nil {
				m.cfg.OnStopBackup()
			}

		case <-m.mAutostart.ClickedCh:
			m.toggleAutostart()

		case <-mOpenConfig.ClickedCh:
			if m.cfg.OnOpenConfig != nil {
				m.cfg.OnOpenConfig()
			}

		case <-mOpenLogs.ClickedCh:
			if m.cfg.OnOpenLogs != nil {
				m.cfg.OnOpenLogs()
			}

		case <-mOpenAppLog.ClickedCh:
			if m.cfg.OnOpenAppLog != nil {
				m.cfg.OnOpenAppLog()
			}

		case <-m.mUpdateStatus.ClickedCh:
			if m.cfg.OnUpdateClick != nil {
				m.cfg.OnUpdateClick()
			}

		case <-mQuit.ClickedCh:
			if m.cfg.OnQuit != nil {
				m.cfg.OnQuit()
			}
			return
		}
	}
}

// toggleAutostart handles the autostart checkbox toggle.
func (m *Menu) toggleAutostart() {
	if m.cfg.Autostart == nil {
		slog.Info("Autostart not available")
		return
	}

	if err := m.cfg.Autostart.Toggle(); err != nil {
		slog.Error("Error toggling autostart", "error", err)
		return
	}

	if m.cfg.Autostart.IsEnabled() {
		m.mAutostart.Check()
		slog.Info("Start at Login: enabled")
	} else {
		m.mAutostart.Uncheck()
		slog.Info("Start at Login: disabled")
	}
}

// UpdateStatus refreshes the status menu item.
func (m *Menu) UpdateStatus() {
	if m.mStatus == nil {
		return
	}

	running := m.cfg.BackupState.IsRunning()
	progress := m.cfg.BackupState.GetProgress()

	var title string
	if running && progress != nil {
		title = FormatProgress(progress)
	} else {
		title = FormatStatus(m.cfg.AppState(), running)
	}

	m.mStatus.SetTitle(title)
}

// SetBackupRunning updates menu visibility for backup start/stop.
func (m *Menu) SetBackupRunning(running bool) {
	if running {
		if m.mBackupNow != nil {
			m.mBackupNow.Hide()
		}
		if m.mStopBackup != nil {
			m.mStopBackup.Show()
		}
	} else {
		if m.mStopBackup != nil {
			m.mStopBackup.Hide()
		}
		if m.mBackupNow != nil {
			m.mBackupNow.Show()
			if m.cfg.IsConfigured() {
				m.mBackupNow.Enable()
			}
		}
	}
}

// SetUpdateStatus sets the update menu item text and enabled state.
func (m *Menu) SetUpdateStatus(text string, enabled bool) {
	if m.mUpdateStatus == nil {
		return
	}
	m.mUpdateStatus.SetTitle(text)
	if enabled {
		m.mUpdateStatus.Enable()
	} else {
		m.mUpdateStatus.Disable()
	}
}

// RefreshOnConfigChange updates menu after config reload.
func (m *Menu) RefreshOnConfigChange() {
	if m.cfg.IsConfigured() {
		if m.mBackupNow != nil {
			m.mBackupNow.Enable()
		}
	}
	m.UpdateStatus()
}
