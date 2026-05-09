package tray

import (
	"log/slog"
	"strings"
	"time"

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

// ScheduleProvider provides the next scheduled backup time.
type ScheduleProvider interface {
	NextBackupTime() (time.Time, error)
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
	// ConfigError returns the most recent config load/validate error.
	// When non-nil, the menu surfaces it in the status line + tooltip
	// instead of the usual "Last backup" / "Configuration required" text.
	// May be nil if the caller does not provide one.
	ConfigError func() error
	Autostart   AutostartProvider
	// Schedule is a getter (not a value) so the menu picks up the scheduler
	// after a late initScheduler call (e.g. unconfigured -> configured via
	// config reload). Returning nil means "no scheduler available, hide the
	// next-backup line".
	Schedule func() ScheduleProvider

	// Action callbacks
	OnBackupNow    func()
	OnStopBackup   func()
	OnOpenConfig   func()
	OnOpenLogs     func()
	OnOpenAppLog   func()
	OnUpdateClick  func()
	OnVersionClick func()
	OnQuit         func()

	// UseKeychain reports whether the active config has use_keychain: true.
	// When false, the password menu items are disabled.
	UseKeychain func() bool

	// OnSetPassword is invoked when the user clicks "Set repository
	// password…". Implementations should pop the password dialog and write
	// the result to the keychain.
	OnSetPassword func()

	// OnClearPassword is invoked when the user clicks "Clear repository
	// password".
	OnClearPassword func()
}

// Menu manages the system tray menu.
type Menu struct {
	cfg MenuConfig

	// Menu items (internal references for dynamic updates)
	mStatus       *systray.MenuItem
	mNextBackup   *systray.MenuItem
	mBackupNow    *systray.MenuItem
	mStopBackup   *systray.MenuItem
	mAutostart    *systray.MenuItem
	mUpdateStatus *systray.MenuItem
	mSetPassword  *systray.MenuItem
	mClearPassword *systray.MenuItem
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

	// Status line: config error takes priority over other states.
	if cfgErr := m.currentConfigError(); cfgErr != nil {
		title, tooltip := statusLineForError(cfgErr)
		m.mStatus = systray.AddMenuItem(title, tooltip)
	} else if !isConfigured {
		m.mStatus = systray.AddMenuItem("Configuration required...", "Please edit config.yaml")
	} else {
		m.mStatus = systray.AddMenuItem(FormatStatus(m.cfg.AppState(), m.cfg.BackupState.IsRunning()), "Backup status")
	}
	m.mStatus.Disable()

	m.mNextBackup = systray.AddMenuItem("", "Next scheduled backup")
	m.mNextBackup.Disable()
	if !isConfigured {
		m.mNextBackup.Hide()
	}

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

	// Password management (only meaningful when use_keychain is enabled)
	m.mSetPassword = systray.AddMenuItem("Set repository password…", "Store the restic repository password in the OS keychain")
	m.mClearPassword = systray.AddMenuItem("Clear repository password", "Remove the stored repository password from the OS keychain")
	m.applyPasswordMenuState()

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

	systray.AddSeparator()

	// About
	mAbout := systray.AddMenuItem("About", "Open project website")

	// Quit
	mQuit := systray.AddMenuItem("Quit", "Quit NeubiBackup")

	// Start event loop
	go m.eventLoop(mOpenConfig, mOpenLogs, mOpenAppLog, mAbout, mQuit)
}

// eventLoop handles menu item clicks.
func (m *Menu) eventLoop(mOpenConfig, mOpenLogs, mOpenAppLog, mAbout, mQuit *systray.MenuItem) {
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

		case <-mAbout.ClickedCh:
			if m.cfg.OnVersionClick != nil {
				m.cfg.OnVersionClick()
			}

		case <-m.mSetPassword.ClickedCh:
			if m.cfg.OnSetPassword != nil {
				m.cfg.OnSetPassword()
			}

		case <-m.mClearPassword.ClickedCh:
			if m.cfg.OnClearPassword != nil {
				m.cfg.OnClearPassword()
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

// applyPasswordMenuState enables/disables the password menu items based
// on whether the config has use_keychain: true.
func (m *Menu) applyPasswordMenuState() {
	enabled := m.cfg.UseKeychain != nil && m.cfg.UseKeychain()
	if m.mSetPassword != nil {
		if enabled {
			m.mSetPassword.Enable()
		} else {
			m.mSetPassword.Disable()
		}
	}
	if m.mClearPassword != nil {
		if enabled {
			m.mClearPassword.Enable()
		} else {
			m.mClearPassword.Disable()
		}
	}
}

// statusLineMaxLen caps the menu item title length so a long error message
// doesn't push the menu off screen. The full text is always available in the
// tooltip.
const statusLineMaxLen = 110

// statusLineForError builds the title and tooltip for the status line when
// the config has an error. The title is truncated; the tooltip carries the
// full message verbatim.
func statusLineForError(err error) (title, tooltip string) {
	full := err.Error()
	tooltip = full
	// Collapse internal newlines so the title stays on one line.
	oneLine := strings.ReplaceAll(full, "\n", " ")
	if len(oneLine) > statusLineMaxLen {
		oneLine = oneLine[:statusLineMaxLen-1] + "…"
	}
	title = "⚠ " + oneLine
	return title, tooltip
}

// nextBackupMenuText decides what the next-backup menu line should display.
// Returns ("", false) when the line should be hidden.
func nextBackupMenuText(isConfigured, isRunning bool, scheduleFn func() ScheduleProvider) (string, bool) {
	if scheduleFn == nil || !isConfigured || isRunning {
		return "", false
	}
	sched := scheduleFn()
	if sched == nil {
		return "", false
	}
	next, err := sched.NextBackupTime()
	if err != nil {
		return "", false
	}
	return "Next backup: " + FormatNextBackup(next), true
}

// currentConfigError returns the current config error, or nil if the caller
// did not provide a ConfigError getter or the getter returned nil.
func (m *Menu) currentConfigError() error {
	if m.cfg.ConfigError == nil {
		return nil
	}
	return m.cfg.ConfigError()
}

// UpdateStatus refreshes the status menu item.
func (m *Menu) UpdateStatus() {
	if m.mStatus == nil {
		return
	}

	if cfgErr := m.currentConfigError(); cfgErr != nil {
		title, tooltip := statusLineForError(cfgErr)
		m.mStatus.SetTitle(title)
		m.mStatus.SetTooltip(tooltip)
		// A config error overrides scheduler info — no useful "next backup" to show.
		if m.mNextBackup != nil {
			m.mNextBackup.Hide()
		}
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
	m.mStatus.SetTooltip("Backup status")

	if m.mNextBackup == nil {
		return
	}
	text, show := nextBackupMenuText(m.cfg.IsConfigured(), m.cfg.BackupState.IsRunning(), m.cfg.Schedule)
	if show {
		m.mNextBackup.SetTitle(text)
		m.mNextBackup.Show()
	} else {
		m.mNextBackup.Hide()
	}
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
	m.applyPasswordMenuState()
	m.UpdateStatus()
}
