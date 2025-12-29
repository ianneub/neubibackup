package updater

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Mock implementations for testing

type mockUpdater struct {
	checkResult    string
	checkAvailable bool
	checkErr       error
	applyErr       error
	checkCalled    bool
	applyCalled    bool
}

func (m *mockUpdater) CheckForUpdate(ctx context.Context) (string, bool, error) {
	m.checkCalled = true
	return m.checkResult, m.checkAvailable, m.checkErr
}

func (m *mockUpdater) DownloadAndApply(ctx context.Context) error {
	m.applyCalled = true
	return m.applyErr
}

type mockState struct {
	lastUpdateCheck time.Time
	savedCount      int
	saveErr         error
	lastError       string
	lastErrorTime   time.Time
	lastVersion     string
	lastVersionTime time.Time
}

func (m *mockState) GetLastUpdateCheck() time.Time {
	return m.lastUpdateCheck
}

func (m *mockState) SetLastUpdateCheck(t time.Time) {
	m.lastUpdateCheck = t
}

func (m *mockState) SetLastUpdateError(err string, t time.Time) {
	m.lastError = err
	m.lastErrorTime = t
}

func (m *mockState) SetLastUpdateSuccess(version string, t time.Time) {
	m.lastVersion = version
	m.lastVersionTime = t
}

func (m *mockState) Save() error {
	m.savedCount++
	return m.saveErr
}

type mockBackupChecker struct {
	running bool
}

func (m *mockBackupChecker) IsRunning() bool {
	return m.running
}

type mockUpdateState struct {
	mu               sync.Mutex
	hasUpdate        bool
	availableVersion string
	inProgress       bool
}

func (m *mockUpdateState) HasUpdate() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hasUpdate
}

func (m *mockUpdateState) GetAvailableVersion() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.availableVersion
}

func (m *mockUpdateState) SetAvailableVersion(version string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.availableVersion = version
	m.hasUpdate = version != ""
}

func (m *mockUpdateState) TryStartUpdate() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inProgress {
		return false
	}
	m.inProgress = true
	return true
}

func (m *mockUpdateState) FinishUpdate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inProgress = false
}

type mockMenuUpdater struct {
	mu        sync.Mutex
	lastText  string
	enabled   bool
	callCount int
}

func (m *mockMenuUpdater) SetUpdateStatus(text string, enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastText = text
	m.enabled = enabled
	m.callCount++
}

func (m *mockMenuUpdater) getLastStatus() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastText, m.enabled
}

// updaterInterface wraps *Updater for mocking
type updaterInterface interface {
	CheckForUpdate(ctx context.Context) (string, bool, error)
	DownloadAndApply(ctx context.Context) error
}

// testOrchestrator is a version of UpdateOrchestrator that accepts an interface for the updater
type testOrchestrator struct {
	updater       updaterInterface
	state         StateProvider
	backupChecker BackupChecker
	updateState   UpdateStateProvider
	menuUpdater   MenuUpdater
}

func newTestOrchestrator(
	updater updaterInterface,
	state StateProvider,
	backupChecker BackupChecker,
	updateState UpdateStateProvider,
	menuUpdater MenuUpdater,
) *testOrchestrator {
	return &testOrchestrator{
		updater:       updater,
		state:         state,
		backupChecker: backupChecker,
		updateState:   updateState,
		menuUpdater:   menuUpdater,
	}
}

func (o *testOrchestrator) CheckIfNeeded(ctx context.Context) {
	if time.Since(o.state.GetLastUpdateCheck()) < 24*time.Hour {
		return
	}
	o.Check(ctx)
}

func (o *testOrchestrator) Check(ctx context.Context) {
	newVersion, available, err := o.updater.CheckForUpdate(ctx)
	if err != nil {
		return
	}

	o.state.SetLastUpdateCheck(time.Now())
	_ = o.state.Save()

	if available {
		o.updateState.SetAvailableVersion(newVersion)
		if o.menuUpdater != nil {
			o.menuUpdater.SetUpdateStatus("Update Available ("+newVersion+")", true)
		}
	}
}

func (o *testOrchestrator) ManualCheck(ctx context.Context) {
	if o.menuUpdater != nil {
		o.menuUpdater.SetUpdateStatus("Checking for updates...", false)
	}

	o.Check(ctx)

	if !o.updateState.HasUpdate() && o.menuUpdater != nil {
		o.menuUpdater.SetUpdateStatus("Check for Updates", true)
	}
}

func (o *testOrchestrator) Install(ctx context.Context) {
	if o.backupChecker.IsRunning() {
		if o.menuUpdater != nil {
			o.menuUpdater.SetUpdateStatus("Update blocked - backup running", true)
		}
		return
	}

	if o.menuUpdater != nil {
		o.menuUpdater.SetUpdateStatus("Downloading update...", false)
	}

	if err := o.updater.DownloadAndApply(ctx); err != nil {
		if o.menuUpdater != nil {
			o.menuUpdater.SetUpdateStatus("Update failed - click to retry", true)
		}
		return
	}

	o.state.SetLastUpdateSuccess(o.updateState.GetAvailableVersion(), time.Now())
	_ = o.state.Save()

	if o.menuUpdater != nil {
		o.menuUpdater.SetUpdateStatus("Update installed - restarting...", false)
	}
}

// Tests

func TestCheckIfNeeded_SkipsRecentCheck(t *testing.T) {
	updater := &mockUpdater{}
	state := &mockState{lastUpdateCheck: time.Now().Add(-12 * time.Hour)} // 12 hours ago
	backupChecker := &mockBackupChecker{}
	updateState := &mockUpdateState{}
	menuUpdater := &mockMenuUpdater{}

	orch := newTestOrchestrator(updater, state, backupChecker, updateState, menuUpdater)
	orch.CheckIfNeeded(context.Background())

	if updater.checkCalled {
		t.Error("Expected update check to be skipped for recent check")
	}
}

func TestCheckIfNeeded_ChecksWhenOld(t *testing.T) {
	updater := &mockUpdater{checkResult: "1.2.0", checkAvailable: true}
	state := &mockState{lastUpdateCheck: time.Now().Add(-25 * time.Hour)} // 25 hours ago
	backupChecker := &mockBackupChecker{}
	updateState := &mockUpdateState{}
	menuUpdater := &mockMenuUpdater{}

	orch := newTestOrchestrator(updater, state, backupChecker, updateState, menuUpdater)
	orch.CheckIfNeeded(context.Background())

	if !updater.checkCalled {
		t.Error("Expected update check to be called for old check")
	}
}

func TestCheck_UpdateAvailable(t *testing.T) {
	updater := &mockUpdater{checkResult: "1.2.0", checkAvailable: true}
	state := &mockState{}
	backupChecker := &mockBackupChecker{}
	updateState := &mockUpdateState{}
	menuUpdater := &mockMenuUpdater{}

	orch := newTestOrchestrator(updater, state, backupChecker, updateState, menuUpdater)
	orch.Check(context.Background())

	if !updateState.HasUpdate() {
		t.Error("Expected update state to have update")
	}
	if updateState.GetAvailableVersion() != "1.2.0" {
		t.Errorf("Expected version 1.2.0, got %s", updateState.GetAvailableVersion())
	}

	text, enabled := menuUpdater.getLastStatus()
	if text != "Update Available (1.2.0)" {
		t.Errorf("Expected menu text 'Update Available (1.2.0)', got %s", text)
	}
	if !enabled {
		t.Error("Expected menu item to be enabled")
	}
}

func TestCheck_NoUpdate(t *testing.T) {
	updater := &mockUpdater{checkResult: "", checkAvailable: false}
	state := &mockState{}
	backupChecker := &mockBackupChecker{}
	updateState := &mockUpdateState{}
	menuUpdater := &mockMenuUpdater{}

	orch := newTestOrchestrator(updater, state, backupChecker, updateState, menuUpdater)
	orch.Check(context.Background())

	if updateState.HasUpdate() {
		t.Error("Expected no update to be available")
	}
}

func TestCheck_Error(t *testing.T) {
	updater := &mockUpdater{checkErr: errors.New("network error")}
	state := &mockState{}
	backupChecker := &mockBackupChecker{}
	updateState := &mockUpdateState{}
	menuUpdater := &mockMenuUpdater{}

	orch := newTestOrchestrator(updater, state, backupChecker, updateState, menuUpdater)
	orch.Check(context.Background())

	if state.savedCount > 0 {
		t.Error("Expected state not to be saved on error")
	}
}

func TestCheck_SavesState(t *testing.T) {
	updater := &mockUpdater{checkResult: "", checkAvailable: false}
	state := &mockState{}
	backupChecker := &mockBackupChecker{}
	updateState := &mockUpdateState{}
	menuUpdater := &mockMenuUpdater{}

	orch := newTestOrchestrator(updater, state, backupChecker, updateState, menuUpdater)
	orch.Check(context.Background())

	if state.savedCount != 1 {
		t.Errorf("Expected state to be saved once, was saved %d times", state.savedCount)
	}
	if time.Since(state.lastUpdateCheck) > time.Second {
		t.Error("Expected last update check to be recent")
	}
}

func TestManualCheck_ShowsCheckingStatus(t *testing.T) {
	updater := &mockUpdater{}
	state := &mockState{}
	backupChecker := &mockBackupChecker{}
	updateState := &mockUpdateState{}
	menuUpdater := &mockMenuUpdater{}

	orch := newTestOrchestrator(updater, state, backupChecker, updateState, menuUpdater)
	orch.ManualCheck(context.Background())

	// Since no update was found, menu should show "Check for Updates"
	text, enabled := menuUpdater.getLastStatus()
	if text != "Check for Updates" {
		t.Errorf("Expected menu text 'Check for Updates', got %s", text)
	}
	if !enabled {
		t.Error("Expected menu item to be enabled")
	}
}

func TestManualCheck_KeepsUpdateStatusWhenFound(t *testing.T) {
	updater := &mockUpdater{checkResult: "2.0.0", checkAvailable: true}
	state := &mockState{}
	backupChecker := &mockBackupChecker{}
	updateState := &mockUpdateState{}
	menuUpdater := &mockMenuUpdater{}

	orch := newTestOrchestrator(updater, state, backupChecker, updateState, menuUpdater)
	orch.ManualCheck(context.Background())

	text, enabled := menuUpdater.getLastStatus()
	if text != "Update Available (2.0.0)" {
		t.Errorf("Expected menu text 'Update Available (2.0.0)', got %s", text)
	}
	if !enabled {
		t.Error("Expected menu item to be enabled")
	}
}

func TestInstall_BlockedDuringBackup(t *testing.T) {
	updater := &mockUpdater{}
	state := &mockState{}
	backupChecker := &mockBackupChecker{running: true}
	updateState := &mockUpdateState{availableVersion: "1.2.0", hasUpdate: true}
	menuUpdater := &mockMenuUpdater{}

	orch := newTestOrchestrator(updater, state, backupChecker, updateState, menuUpdater)
	orch.Install(context.Background())

	if updater.applyCalled {
		t.Error("Expected update not to be applied during backup")
	}

	text, _ := menuUpdater.getLastStatus()
	if text != "Update blocked - backup running" {
		t.Errorf("Expected blocked message, got %s", text)
	}
}

func TestInstall_Success(t *testing.T) {
	updater := &mockUpdater{}
	state := &mockState{}
	backupChecker := &mockBackupChecker{running: false}
	updateState := &mockUpdateState{availableVersion: "1.2.0", hasUpdate: true}
	menuUpdater := &mockMenuUpdater{}

	orch := newTestOrchestrator(updater, state, backupChecker, updateState, menuUpdater)
	orch.Install(context.Background())

	if !updater.applyCalled {
		t.Error("Expected update to be applied")
	}

	if state.lastVersion != "1.2.0" {
		t.Errorf("Expected version 1.2.0 to be recorded, got %s", state.lastVersion)
	}

	text, _ := menuUpdater.getLastStatus()
	if text != "Update installed - restarting..." {
		t.Errorf("Expected success message, got %s", text)
	}
}

func TestInstall_Failure(t *testing.T) {
	updater := &mockUpdater{applyErr: errors.New("download failed")}
	state := &mockState{}
	backupChecker := &mockBackupChecker{running: false}
	updateState := &mockUpdateState{availableVersion: "1.2.0", hasUpdate: true}
	menuUpdater := &mockMenuUpdater{}

	orch := newTestOrchestrator(updater, state, backupChecker, updateState, menuUpdater)
	orch.Install(context.Background())

	text, enabled := menuUpdater.getLastStatus()
	if text != "Update failed - click to retry" {
		t.Errorf("Expected failure message, got %s", text)
	}
	if !enabled {
		t.Error("Expected menu item to be enabled for retry")
	}
}

func TestInstall_NilMenuUpdater(t *testing.T) {
	updater := &mockUpdater{}
	state := &mockState{}
	backupChecker := &mockBackupChecker{running: false}
	updateState := &mockUpdateState{availableVersion: "1.2.0", hasUpdate: true}

	// nil menu updater should not panic
	orch := newTestOrchestrator(updater, state, backupChecker, updateState, nil)
	orch.Install(context.Background())

	if !updater.applyCalled {
		t.Error("Expected update to be applied even with nil menu")
	}
}

func TestCheck_NilMenuUpdater(t *testing.T) {
	updater := &mockUpdater{checkResult: "1.2.0", checkAvailable: true}
	state := &mockState{}
	backupChecker := &mockBackupChecker{}
	updateState := &mockUpdateState{}

	// nil menu updater should not panic
	orch := newTestOrchestrator(updater, state, backupChecker, updateState, nil)
	orch.Check(context.Background())

	if !updateState.HasUpdate() {
		t.Error("Expected update state to have update")
	}
}

// Test concurrent access to update state
func TestUpdateState_ConcurrentAccess(t *testing.T) {
	updateState := &mockUpdateState{}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			updateState.SetAvailableVersion("1.0.0")
			_ = updateState.HasUpdate()
			_ = updateState.GetAvailableVersion()
			if updateState.TryStartUpdate() {
				updateState.FinishUpdate()
			}
		}(i)
	}
	wg.Wait()
}
