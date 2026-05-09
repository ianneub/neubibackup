package tray

import (
	"errors"
	"strings"
	"testing"
	"time"

	"neubibackup/internal/restic"
	"neubibackup/internal/state"
)

// Mock implementations for testing

type mockBackupState struct {
	running  bool
	progress *restic.BackupProgress
}

func (m *mockBackupState) IsRunning() bool                     { return m.running }
func (m *mockBackupState) GetProgress() *restic.BackupProgress { return m.progress }

type mockUpdateState struct {
	hasUpdate        bool
	availableVersion string
}

func (m *mockUpdateState) HasUpdate() bool             { return m.hasUpdate }
func (m *mockUpdateState) GetAvailableVersion() string { return m.availableVersion }

type mockAutostart struct {
	enabled     bool
	toggleError error
}

func (m *mockAutostart) IsEnabled() bool { return m.enabled }
func (m *mockAutostart) Toggle() error {
	if m.toggleError != nil {
		return m.toggleError
	}
	m.enabled = !m.enabled
	return nil
}

// Tests for MenuConfig callback wiring

func TestMenuConfig_Callbacks(t *testing.T) {
	backupNowCalled := false
	stopBackupCalled := false
	openConfigCalled := false
	openLogsCalled := false
	openAppLogCalled := false
	updateClickCalled := false
	quitCalled := false

	cfg := MenuConfig{
		OnBackupNow:   func() { backupNowCalled = true },
		OnStopBackup:  func() { stopBackupCalled = true },
		OnOpenConfig:  func() { openConfigCalled = true },
		OnOpenLogs:    func() { openLogsCalled = true },
		OnOpenAppLog:  func() { openAppLogCalled = true },
		OnUpdateClick: func() { updateClickCalled = true },
		OnQuit:        func() { quitCalled = true },
	}

	cfg.OnBackupNow()
	if !backupNowCalled {
		t.Error("OnBackupNow callback not called")
	}

	cfg.OnStopBackup()
	if !stopBackupCalled {
		t.Error("OnStopBackup callback not called")
	}

	cfg.OnOpenConfig()
	if !openConfigCalled {
		t.Error("OnOpenConfig callback not called")
	}

	cfg.OnOpenLogs()
	if !openLogsCalled {
		t.Error("OnOpenLogs callback not called")
	}

	cfg.OnOpenAppLog()
	if !openAppLogCalled {
		t.Error("OnOpenAppLog callback not called")
	}

	cfg.OnUpdateClick()
	if !updateClickCalled {
		t.Error("OnUpdateClick callback not called")
	}

	cfg.OnQuit()
	if !quitCalled {
		t.Error("OnQuit callback not called")
	}
}

func TestMenuConfig_StateProviders(t *testing.T) {
	appState := &state.State{
		Backup: state.BackupState{
			ConsecutiveFailures: 3,
		},
	}
	backupState := &mockBackupState{running: true}
	updateState := &mockUpdateState{hasUpdate: true, availableVersion: "1.2.3"}

	cfg := MenuConfig{
		AppState:     func() *state.State { return appState },
		BackupState:  backupState,
		UpdateState:  updateState,
		IsConfigured: func() bool { return true },
	}

	if cfg.AppState().Backup.ConsecutiveFailures != 3 {
		t.Error("AppState provider not working")
	}

	if !cfg.BackupState.IsRunning() {
		t.Error("BackupState provider not working")
	}

	if !cfg.UpdateState.HasUpdate() {
		t.Error("UpdateState provider not working")
	}

	if cfg.UpdateState.GetAvailableVersion() != "1.2.3" {
		t.Error("UpdateState.GetAvailableVersion not working")
	}

	if !cfg.IsConfigured() {
		t.Error("IsConfigured provider not working")
	}
}

func TestMockBackupState(t *testing.T) {
	tests := []struct {
		name     string
		running  bool
		progress *restic.BackupProgress
	}{
		{
			name:    "not running no progress",
			running: false,
		},
		{
			name:    "running with progress",
			running: true,
			progress: &restic.BackupProgress{
				PercentDone:    0.5,
				BytesProcessed: 1024,
				TotalBytes:     2048,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockBackupState{running: tt.running, progress: tt.progress}

			if m.IsRunning() != tt.running {
				t.Errorf("IsRunning() = %v, want %v", m.IsRunning(), tt.running)
			}

			if m.GetProgress() != tt.progress {
				t.Errorf("GetProgress() = %v, want %v", m.GetProgress(), tt.progress)
			}
		})
	}
}

func TestMockUpdateState(t *testing.T) {
	tests := []struct {
		name             string
		hasUpdate        bool
		availableVersion string
	}{
		{
			name:      "no update",
			hasUpdate: false,
		},
		{
			name:             "update available",
			hasUpdate:        true,
			availableVersion: "2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockUpdateState{hasUpdate: tt.hasUpdate, availableVersion: tt.availableVersion}

			if m.HasUpdate() != tt.hasUpdate {
				t.Errorf("HasUpdate() = %v, want %v", m.HasUpdate(), tt.hasUpdate)
			}

			if m.GetAvailableVersion() != tt.availableVersion {
				t.Errorf("GetAvailableVersion() = %v, want %v", m.GetAvailableVersion(), tt.availableVersion)
			}
		})
	}
}

func TestMockAutostart_Toggle(t *testing.T) {
	auto := &mockAutostart{enabled: false}

	if auto.IsEnabled() {
		t.Error("expected not enabled initially")
	}

	if err := auto.Toggle(); err != nil {
		t.Errorf("Toggle() error = %v", err)
	}

	if !auto.IsEnabled() {
		t.Error("expected enabled after toggle")
	}

	if err := auto.Toggle(); err != nil {
		t.Errorf("Toggle() error = %v", err)
	}

	if auto.IsEnabled() {
		t.Error("expected not enabled after second toggle")
	}
}

func TestMockAutostart_ToggleError(t *testing.T) {
	expectedErr := errors.New("toggle failed")
	auto := &mockAutostart{enabled: false, toggleError: expectedErr}

	err := auto.Toggle()
	if err != expectedErr {
		t.Errorf("Toggle() error = %v, want %v", err, expectedErr)
	}

	// State should not change on error
	if auto.IsEnabled() {
		t.Error("state should not change on toggle error")
	}
}

// Note: Full Menu tests require systray which needs a display.
// The tests above verify the configuration and mock objects work correctly.
// Integration tests would be done manually or with a CI environment that has a display.

type mockScheduleProvider struct {
	next time.Time
	err  error
}

func (m *mockScheduleProvider) NextBackupTime() (time.Time, error) {
	return m.next, m.err
}

func fnReturning(p ScheduleProvider) func() ScheduleProvider {
	return func() ScheduleProvider { return p }
}

func TestStatusLineForError(t *testing.T) {
	t.Run("short error gets warning prefix and full tooltip", func(t *testing.T) {
		title, tooltip := statusLineForError(errors.New("boom"))
		if !strings.HasPrefix(title, "⚠ ") {
			t.Errorf("title = %q, want warning-prefixed", title)
		}
		if !strings.Contains(title, "boom") {
			t.Errorf("title = %q, want to contain error text", title)
		}
		if tooltip != "boom" {
			t.Errorf("tooltip = %q, want full message", tooltip)
		}
	})

	t.Run("long error truncates title but not tooltip", func(t *testing.T) {
		long := strings.Repeat("x", statusLineMaxLen+50)
		title, tooltip := statusLineForError(errors.New(long))
		if !strings.HasSuffix(title, "…") {
			t.Errorf("title = %q, want trailing ellipsis on overflow", title)
		}
		if tooltip != long {
			t.Errorf("tooltip should preserve full message; got %d chars, want %d", len(tooltip), len(long))
		}
	})

	t.Run("multi-line error collapses to one line in title", func(t *testing.T) {
		err := errors.New("first line\nsecond line")
		title, tooltip := statusLineForError(err)
		if strings.Contains(title, "\n") {
			t.Errorf("title = %q, must not contain newlines", title)
		}
		if !strings.Contains(title, "first line") || !strings.Contains(title, "second line") {
			t.Errorf("title = %q, want to contain both lines joined", title)
		}
		if tooltip != "first line\nsecond line" {
			t.Errorf("tooltip = %q, want preserved newlines", tooltip)
		}
	})
}

func TestApplyPasswordMenuStateEnabled(t *testing.T) {
	var enabled bool
	m := &Menu{
		cfg: MenuConfig{
			UseKeychain: func() bool { return enabled },
		},
		// mSetPassword / mClearPassword left nil — applyPasswordMenuState
		// must not panic on nil items.
	}

	enabled = false
	m.applyPasswordMenuState() // must not panic on nil items

	enabled = true
	m.applyPasswordMenuState() // must not panic on nil items
}

func TestMenuConfigUseKeychainNilSafe(t *testing.T) {
	m := &Menu{cfg: MenuConfig{UseKeychain: nil}}
	m.applyPasswordMenuState() // must not panic when getter is nil
}

func TestNextBackupMenuText(t *testing.T) {
	future := time.Now().Add(2 * time.Hour)
	provider := &mockScheduleProvider{next: future}

	tests := []struct {
		name         string
		isConfigured bool
		isRunning    bool
		scheduleFn   func() ScheduleProvider
		wantShow     bool
		wantPrefix   string
	}{
		{"shown when idle and configured", true, false, fnReturning(provider), true, "Next backup: "},
		{"hidden while running", true, true, fnReturning(provider), false, ""},
		{"hidden when not configured", false, false, fnReturning(provider), false, ""},
		{"hidden when getter is nil", true, false, nil, false, ""},
		{"hidden when getter returns nil", true, false, fnReturning(nil), false, ""},
		{"hidden when provider errors", true, false, fnReturning(&mockScheduleProvider{err: errors.New("boom")}), false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, show := nextBackupMenuText(tt.isConfigured, tt.isRunning, tt.scheduleFn)
			if show != tt.wantShow {
				t.Errorf("show = %v, want %v", show, tt.wantShow)
			}
			if tt.wantShow && !strings.HasPrefix(text, tt.wantPrefix) {
				t.Errorf("text = %q, want prefix %q", text, tt.wantPrefix)
			}
			if !tt.wantShow && text != "" {
				t.Errorf("text = %q, want empty", text)
			}
		})
	}
}
