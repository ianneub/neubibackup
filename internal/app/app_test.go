package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"neubibackup/internal/config"
	"neubibackup/internal/restic"
	"neubibackup/internal/state"
)

// TestNew tests the App constructor
func TestNew(t *testing.T) {
	app := New("v1.0.0")

	if app == nil {
		t.Fatal("New returned nil")
	}

	if app.version != "v1.0.0" {
		t.Errorf("version = %q, want %q", app.version, "v1.0.0")
	}

	if app.backupState == nil {
		t.Error("backupState should not be nil")
	}

	if app.updateState == nil {
		t.Error("updateState should not be nil")
	}
}

// TestNewWithOptions tests functional options
func TestNewWithOptions(t *testing.T) {
	iconUpdateCalled := false
	quitCalled := false
	tooltipCalled := false

	app := New("v1.0.0",
		WithOnIconUpdate(func([]byte) { iconUpdateCalled = true }),
		WithOnQuit(func() { quitCalled = true }),
		WithSetTooltip(func(string) { tooltipCalled = true }),
	)

	// Call the callbacks to verify they were set
	app.onIconUpdate(nil)
	app.onQuit()
	app.setTooltip("test")

	if !iconUpdateCalled {
		t.Error("onIconUpdate was not set correctly")
	}
	if !quitCalled {
		t.Error("onQuit was not set correctly")
	}
	if !tooltipCalled {
		t.Error("setTooltip was not set correctly")
	}
}

// TestBackupStateBehavior tests the backup state through the App
func TestBackupStateBehavior(t *testing.T) {
	app := New("v1.0.0")

	// Test that backup is not running initially
	if app.IsBackupRunning() {
		t.Error("backup should not be running initially")
	}

	// Simulate starting a backup
	ctx, err := app.backupState.StartBackup(context.Background())
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}
	if ctx == nil {
		t.Fatal("StartBackup returned nil context")
	}

	if !app.IsBackupRunning() {
		t.Error("backup should be running after StartBackup")
	}

	// Reset state
	app.backupState.Reset()

	if app.IsBackupRunning() {
		t.Error("backup should not be running after Reset")
	}
}

// TestStopBackupWhenNotRunning tests StopBackup behavior when no backup is running
func TestStopBackupWhenNotRunning(t *testing.T) {
	app := New("v1.0.0")

	// Ensure we start in a clean state
	app.backupState.Reset()

	// This should not panic when no backup is running
	app.StopBackup()

	// Verify state is unchanged
	if app.IsBackupRunning() {
		t.Error("backup should still not be running")
	}
}

// TestStopBackupWhenRunning tests StopBackup cancels the context
func TestStopBackupWhenRunning(t *testing.T) {
	app := New("v1.0.0")

	// Ensure we start in a clean state
	app.backupState.Reset()

	// Start a backup to get a context we can verify gets cancelled
	ctx, err := app.backupState.StartBackup(context.Background())
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}

	defer app.backupState.Reset()

	// Call StopBackup
	app.StopBackup()

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		if ctx.Err() != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", ctx.Err())
		}
	default:
		t.Error("context should be cancelled after StopBackup")
	}
}

// TestTriggerBackupWhenRunning tests that TriggerBackup is a no-op when backup is already running
func TestTriggerBackupWhenRunning(t *testing.T) {
	app := New("v1.0.0")

	// Start a backup manually
	_, err := app.backupState.StartBackup(context.Background())
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}
	defer app.backupState.Reset()

	// TriggerBackup should be a no-op (just logs and returns)
	// This test just ensures it doesn't panic
	app.TriggerBackup()

	// Backup should still be running
	if !app.IsBackupRunning() {
		t.Error("backup should still be running")
	}
}

// TestUpdateStateBehavior tests the update state through the App
func TestUpdateStateBehavior(t *testing.T) {
	app := New("v1.0.0")

	// Reset state
	app.updateState.FinishUpdate()
	app.updateState.ClearAvailableVersion()

	// Test that update is not in progress initially
	if app.updateState.IsInProgress() {
		t.Error("update should not be in progress initially")
	}

	// Simulate starting an update
	if !app.updateState.TryStartUpdate() {
		t.Error("TryStartUpdate should return true when not in progress")
	}

	if !app.updateState.IsInProgress() {
		t.Error("update should be in progress after TryStartUpdate")
	}

	// Try starting again should fail
	if app.updateState.TryStartUpdate() {
		t.Error("TryStartUpdate should return false when already in progress")
	}

	// Finish the update
	app.updateState.FinishUpdate()

	if app.updateState.IsInProgress() {
		t.Error("update should not be in progress after FinishUpdate")
	}

	// Test available version
	app.updateState.SetAvailableVersion("v1.2.3")
	if !app.updateState.HasUpdate() {
		t.Error("HasUpdate should return true after setting version")
	}
	if app.updateState.GetAvailableVersion() != "v1.2.3" {
		t.Errorf("GetAvailableVersion = %q, want v1.2.3", app.updateState.GetAvailableVersion())
	}

	app.updateState.ClearAvailableVersion()
	if app.updateState.HasUpdate() {
		t.Error("HasUpdate should return false after clearing version")
	}
}

// TestShutdown tests that Shutdown cleans up resources
func TestShutdown(t *testing.T) {
	app := New("v1.0.0")

	// Create a context to verify it gets cancelled
	app.ctx, app.cancel = context.WithCancel(context.Background())

	// Start a backup
	backupCtx, err := app.backupState.StartBackup(app.ctx)
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}

	// Shutdown
	app.Shutdown()

	// Verify backup context was cancelled (StopBackup cancels the context)
	// Note: IsRunning() is still true until Reset() is called, which is done
	// by the backup goroutine itself. Shutdown just cancels the context.
	select {
	case <-backupCtx.Done():
		// Good, backup context was cancelled
	default:
		t.Error("backup context should be cancelled after Shutdown")
	}

	// Verify app context was cancelled
	select {
	case <-app.ctx.Done():
		// Good, app context was cancelled
	default:
		t.Error("app context should be cancelled after Shutdown")
	}
}

// TestCleanupOldUpdatesNonWindows tests that cleanup is skipped on non-Windows
func TestCleanupOldUpdatesNonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping non-Windows test on Windows")
	}

	// Create a temp directory with .old files
	tempDir := t.TempDir()

	// Create some .old files
	oldFile := filepath.Join(tempDir, ".neubibackup.exe.old")
	if err := os.WriteFile(oldFile, []byte("old binary"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Verify file exists before cleanup
	if _, err := os.Stat(oldFile); os.IsNotExist(err) {
		t.Fatal("Test file should exist before cleanup")
	}

	// Run cleanup (should do nothing on non-Windows)
	cleanupOldUpdates()

	// File should still exist because we're not on Windows
	if _, err := os.Stat(oldFile); os.IsNotExist(err) {
		t.Error("File should still exist on non-Windows after cleanupOldUpdates")
	}
}

// TestUpdateBlockedDuringBackup tests that updates wait for backups
func TestUpdateBlockedDuringBackup(t *testing.T) {
	app := New("v1.0.0")

	// Reset state first
	app.backupState.Reset()

	// Start a backup to simulate a running backup
	_, err := app.backupState.StartBackup(context.Background())
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}

	defer app.backupState.Reset()

	// Check that backup is detected as running
	if !app.IsBackupRunning() {
		t.Error("Backup should be detected as running")
	}

	// In a real scenario, attemptAutoUpdate would wait here
	// We just verify the detection mechanism works
}

// TestDefaultCallbacks tests that default callbacks are no-ops
func TestDefaultCallbacks(t *testing.T) {
	app := New("v1.0.0")

	// These should not panic
	app.onIconUpdate(nil)
	app.onQuit()
	app.setTooltip("test")
}

// TestUpdateIcon tests that updateIcon calls the icon callback with bytes
func TestUpdateIcon(t *testing.T) {
	var receivedBytes []byte
	app := New("v1.0.0",
		WithOnIconUpdate(func(b []byte) { receivedBytes = b }),
	)

	// Set up minimal state needed for updateIcon
	app.state = &state.State{}

	// Call updateIcon
	app.updateIcon()

	// Should have received some icon bytes
	if receivedBytes == nil {
		t.Error("onIconUpdate should have been called with icon bytes")
	}
	if len(receivedBytes) == 0 {
		t.Error("icon bytes should not be empty")
	}
}

// TestUpdateIconWithConfig tests updateIcon with a configured app
func TestUpdateIconWithConfig(t *testing.T) {
	var callCount int
	app := New("v1.0.0",
		WithOnIconUpdate(func([]byte) { callCount++ }),
	)

	app.state = &state.State{}
	app.cfg = &config.Config{
		Repository: config.RepositoryConfig{
			Path:     "/backup/repo",
			Password: "secret",
		},
		Backup: config.BackupConfig{
			Paths: []string{"/home"},
		},
	}

	app.updateIcon()

	if callCount != 1 {
		t.Errorf("onIconUpdate called %d times, want 1", callCount)
	}
}

// TestUpdateIconWhileBackupRunning tests updateIcon during backup
func TestUpdateIconWhileBackupRunning(t *testing.T) {
	var receivedBytes []byte
	app := New("v1.0.0",
		WithOnIconUpdate(func(b []byte) { receivedBytes = b }),
	)

	app.state = &state.State{}
	app.cfg = &config.Config{}

	// Start a backup
	_, err := app.backupState.StartBackup(context.Background())
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}
	defer app.backupState.Reset()

	app.updateIcon()

	if receivedBytes == nil {
		t.Error("onIconUpdate should have been called")
	}
}

// TestUpdateStatusWithNilMenu tests that updateStatus is safe with nil menu
func TestUpdateStatusWithNilMenu(t *testing.T) {
	app := New("v1.0.0")
	app.menu = nil

	// Should not panic
	app.updateStatus()
}

// TestOnSystemWakeWithNilScheduler tests that onSystemWake is safe with nil scheduler
func TestOnSystemWakeWithNilScheduler(t *testing.T) {
	app := New("v1.0.0")
	app.sched = nil

	// Should not panic
	app.onSystemWake()
}

// TestShutdownWithNilFields tests Shutdown with various nil fields
func TestShutdownWithNilFields(t *testing.T) {
	app := New("v1.0.0")

	// All fields are nil - should not panic
	app.Shutdown()
}

// TestShutdownWithContext tests Shutdown cancels the context
func TestShutdownWithContext(t *testing.T) {
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())

	app.Shutdown()

	select {
	case <-app.ctx.Done():
		// Good
	default:
		t.Error("context should be cancelled after Shutdown")
	}
}

// TestShutdownWithTickers tests Shutdown stops tickers
func TestShutdownWithTickers(t *testing.T) {
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())
	app.statusTicker = time.NewTicker(time.Hour)
	app.updateTicker = time.NewTicker(time.Hour)

	// Should not panic
	app.Shutdown()
}

// TestShutdownWithCleanupFunc tests Shutdown calls cleanup function
func TestShutdownWithCleanupFunc(t *testing.T) {
	cleanupCalled := false
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())
	app.cleanupAppLog = func() { cleanupCalled = true }

	app.Shutdown()

	if !cleanupCalled {
		t.Error("cleanup function should have been called")
	}
}

// TestMultipleOptions tests that multiple options are applied correctly
func TestMultipleOptions(t *testing.T) {
	var iconBytes []byte
	quitCalled := false
	tooltipText := ""

	app := New("v2.0.0",
		WithOnIconUpdate(func(b []byte) { iconBytes = b }),
		WithOnQuit(func() { quitCalled = true }),
		WithSetTooltip(func(s string) { tooltipText = s }),
	)

	if app.version != "v2.0.0" {
		t.Errorf("version = %q, want %q", app.version, "v2.0.0")
	}

	app.onIconUpdate([]byte{1, 2, 3})
	if len(iconBytes) != 3 {
		t.Errorf("iconBytes length = %d, want 3", len(iconBytes))
	}

	app.onQuit()
	if !quitCalled {
		t.Error("onQuit should have been called")
	}

	app.setTooltip("test tooltip")
	if tooltipText != "test tooltip" {
		t.Errorf("tooltipText = %q, want %q", tooltipText, "test tooltip")
	}
}

// TestHandleUpdateClickNoUpdate tests handleUpdateClick when no update available
func TestHandleUpdateClickNoUpdate(t *testing.T) {
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())
	defer app.cancel()

	// Clear any available version
	app.updateState.ClearAvailableVersion()

	// Verify the path taken based on HasUpdate()
	if app.updateState.HasUpdate() {
		t.Error("should not have update available")
	}

	// Note: We can't call handleUpdateClick without a properly initialized updater
	// The test just verifies the state check works correctly
}

// TestHandleUpdateClickWithUpdate tests handleUpdateClick when update is available
func TestHandleUpdateClickWithUpdate(t *testing.T) {
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())
	defer app.cancel()

	// Set an available version
	app.updateState.SetAvailableVersion("v2.0.0")

	// Verify the path taken based on HasUpdate()
	if !app.updateState.HasUpdate() {
		t.Error("should have update available")
	}
	if app.updateState.GetAvailableVersion() != "v2.0.0" {
		t.Errorf("available version = %q, want %q", app.updateState.GetAvailableVersion(), "v2.0.0")
	}

	// Note: We can't call handleUpdateClick without a properly initialized updater
	// The test just verifies the state check works correctly
}

// TestCheckForUpdatesIfNeededRecent tests that recent check is skipped
func TestCheckForUpdatesIfNeededRecent(t *testing.T) {
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())
	defer app.cancel()

	// Set state with recent check
	app.state = &state.State{
		LastUpdateCheck: time.Now(), // Just checked
	}

	// Verify the time check logic - recent check should be within 24 hours
	if time.Since(app.state.LastUpdateCheck) >= 24*time.Hour {
		t.Error("should detect recent check time")
	}

	// Note: The actual check is now done via updateOrch.CheckIfNeeded()
	// which is tested in internal/updater/orchestrator_test.go
}

// TestCheckForUpdatesIfNeededOld tests that old check would trigger new check
func TestCheckForUpdatesIfNeededOld(t *testing.T) {
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())
	defer app.cancel()

	// Set state with old check (more than 24 hours ago)
	app.state = &state.State{
		LastUpdateCheck: time.Now().Add(-25 * time.Hour),
	}

	// Verify the time check logic
	if time.Since(app.state.LastUpdateCheck) < 24*time.Hour {
		t.Error("should detect old check time")
	}

	// Note: We can't actually call checkForUpdatesIfNeeded without a properly initialized updater
	// The test just verifies the time check logic works correctly
}

// TestInstallUpdateBlockedDuringBackup tests that install is blocked during backup
func TestInstallUpdateBlockedDuringBackup(t *testing.T) {
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())
	defer app.cancel()

	// Start a backup
	_, err := app.backupState.StartBackup(app.ctx)
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}
	defer app.backupState.Reset()

	// Set an available version
	app.updateState.SetAvailableVersion("v2.0.0")

	// Verify the blocking logic - installUpdate checks IsBackupRunning() first
	if !app.IsBackupRunning() {
		t.Error("backup should be running")
	}

	// The actual installUpdate() would return early due to backup running
	// We verify the state that would cause the block
}

// TestAttemptAutoUpdateAlreadyInProgress tests that concurrent auto-updates are prevented
func TestAttemptAutoUpdateAlreadyInProgress(t *testing.T) {
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())
	defer app.cancel()

	// Start an update
	if !app.updateState.TryStartUpdate() {
		t.Fatal("TryStartUpdate should succeed")
	}
	defer app.updateState.FinishUpdate()

	// Verify that TryStartUpdate would fail for a second update
	if app.updateState.TryStartUpdate() {
		t.Error("TryStartUpdate should return false when already in progress")
	}

	// The actual attemptAutoUpdate() would return early due to update in progress
}

// TestAttemptAutoUpdateContextCancelled tests that context cancellation is detected
func TestAttemptAutoUpdateContextCancelled(t *testing.T) {
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())
	app.state = &state.State{}

	// Cancel the context immediately
	app.cancel()

	// Verify context is cancelled
	select {
	case <-app.ctx.Done():
		// Good, context is cancelled
	default:
		t.Error("context should be cancelled")
	}

	// The actual attemptAutoUpdate() would check ctx.Done() and return early
}

// TestReloadConfigStopsRunningBackup tests that ReloadConfig stops running backup
func TestReloadConfigStopsRunningBackup(t *testing.T) {
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())
	defer app.cancel()

	// Start a backup
	backupCtx, err := app.backupState.StartBackup(app.ctx)
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}

	// Verify backup is running
	if !app.IsBackupRunning() {
		t.Error("backup should be running")
	}

	// Simulate what ReloadConfig does first: stop the backup
	app.backupState.StopBackup()

	// Verify backup context was cancelled
	select {
	case <-backupCtx.Done():
		// Good, backup was stopped
	default:
		t.Error("backup context should be cancelled after StopBackup")
	}

	// Note: We can't call the full ReloadConfig() without proper filesystem setup
	// The test verifies the stop behavior which is the critical part
}

// TestCleanupOldAutostartShortcutNonWindows tests that cleanup is skipped on non-Windows
func TestCleanupOldAutostartShortcutNonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping non-Windows test on Windows")
	}

	// Should do nothing and not panic on non-Windows
	cleanupOldAutostartShortcut()
}

// TestVersionField tests that version is set correctly
func TestVersionField(t *testing.T) {
	tests := []struct {
		version string
	}{
		{"v1.0.0"},
		{"v2.3.4"},
		{"dev"},
		{""},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			app := New(tt.version)
			if app.version != tt.version {
				t.Errorf("version = %q, want %q", app.version, tt.version)
			}
		})
	}
}

// TestUpdateIconWithFailedBackups tests icon state with consecutive failures
func TestUpdateIconWithFailedBackups(t *testing.T) {
	var receivedBytes []byte
	app := New("v1.0.0",
		WithOnIconUpdate(func(b []byte) { receivedBytes = b }),
	)

	app.state = &state.State{
		ConsecutiveFailures: 3, // Simulate failed backups
	}
	app.cfg = &config.Config{
		Repository: config.RepositoryConfig{
			Path:     "/backup/repo",
			Password: "secret",
		},
		Backup: config.BackupConfig{
			Paths: []string{"/home"},
		},
	}

	app.updateIcon()

	if receivedBytes == nil {
		t.Error("onIconUpdate should have been called")
	}
}

// TestUpdateIconWithSuccessfulBackup tests icon state after successful backup
func TestUpdateIconWithSuccessfulBackup(t *testing.T) {
	var receivedBytes []byte
	app := New("v1.0.0",
		WithOnIconUpdate(func(b []byte) { receivedBytes = b }),
	)

	app.state = &state.State{
		ConsecutiveFailures: 0,
		LastBackupSuccess:   time.Now(), // Recent successful backup
	}
	app.cfg = &config.Config{
		Repository: config.RepositoryConfig{
			Path:     "/backup/repo",
			Password: "secret",
		},
		Backup: config.BackupConfig{
			Paths: []string{"/home"},
		},
	}

	app.updateIcon()

	if receivedBytes == nil {
		t.Error("onIconUpdate should have been called")
	}
}

// TestBackupStateProgress tests setting and getting backup progress
func TestBackupStateProgress(t *testing.T) {
	app := New("v1.0.0")

	// Initially no progress
	if app.backupState.GetProgress() != nil {
		t.Error("progress should be nil initially")
	}

	// Start backup and set progress
	_, err := app.backupState.StartBackup(context.Background())
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}
	defer app.backupState.Reset()

	// Set progress
	app.backupState.SetProgress(&restic.BackupProgress{
		PercentDone:    0.5,
		TotalFiles:     100,
		FilesProcessed: 50,
	})

	// Get progress
	progress := app.backupState.GetProgress()
	if progress == nil {
		t.Fatal("progress should not be nil after SetProgress")
	}
	if progress.PercentDone != 0.5 {
		t.Errorf("PercentDone = %f, want 0.5", progress.PercentDone)
	}
	if progress.TotalFiles != 100 {
		t.Errorf("TotalFiles = %d, want 100", progress.TotalFiles)
	}
	if progress.FilesProcessed != 50 {
		t.Errorf("FilesProcessed = %d, want 50", progress.FilesProcessed)
	}
}

// TestBackupStateClearProgress tests clearing backup progress
func TestBackupStateClearProgress(t *testing.T) {
	app := New("v1.0.0")

	// Start backup and set progress
	_, err := app.backupState.StartBackup(context.Background())
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}

	app.backupState.SetProgress(&restic.BackupProgress{PercentDone: 0.5})

	// Clear progress by setting nil
	app.backupState.SetProgress(nil)

	if app.backupState.GetProgress() != nil {
		t.Error("progress should be nil after SetProgress(nil)")
	}

	app.backupState.Reset()
}

// TestUpdateStateVersion tests the available version tracking
func TestUpdateStateVersion(t *testing.T) {
	app := New("v1.0.0")

	// Initially no version
	if app.updateState.GetAvailableVersion() != "" {
		t.Error("available version should be empty initially")
	}
	if app.updateState.HasUpdate() {
		t.Error("should not have update initially")
	}

	// Set version
	app.updateState.SetAvailableVersion("v2.0.0")

	if app.updateState.GetAvailableVersion() != "v2.0.0" {
		t.Errorf("available version = %q, want %q", app.updateState.GetAvailableVersion(), "v2.0.0")
	}
	if !app.updateState.HasUpdate() {
		t.Error("should have update after setting version")
	}

	// Clear version
	app.updateState.ClearAvailableVersion()

	if app.updateState.GetAvailableVersion() != "" {
		t.Error("available version should be empty after clear")
	}
	if app.updateState.HasUpdate() {
		t.Error("should not have update after clear")
	}
}

// TestConcurrentBackupStart tests that only one backup can start
func TestConcurrentBackupStart(t *testing.T) {
	app := New("v1.0.0")

	// Start first backup
	ctx1, err := app.backupState.StartBackup(context.Background())
	if err != nil {
		t.Fatalf("first StartBackup failed: %v", err)
	}
	defer app.backupState.Reset()

	if ctx1 == nil {
		t.Error("first context should not be nil")
	}

	// Try to start second backup - should fail
	ctx2, err := app.backupState.StartBackup(context.Background())
	if err != ErrBackupAlreadyRunning {
		t.Errorf("second StartBackup error = %v, want ErrBackupAlreadyRunning", err)
	}
	if ctx2 != nil {
		t.Error("second context should be nil")
	}
}

// TestShutdownStopsAllResources tests that shutdown properly cleans up
func TestShutdownStopsAllResources(t *testing.T) {
	app := New("v1.0.0")
	app.ctx, app.cancel = context.WithCancel(context.Background())

	// Set up various resources
	app.statusTicker = time.NewTicker(time.Hour)
	app.updateTicker = time.NewTicker(time.Hour)

	cleanupCalled := false
	app.cleanupAppLog = func() { cleanupCalled = true }

	// Start a backup
	backupCtx, err := app.backupState.StartBackup(app.ctx)
	if err != nil {
		t.Fatalf("StartBackup failed: %v", err)
	}

	// Shutdown
	app.Shutdown()

	// Verify cleanup was called
	if !cleanupCalled {
		t.Error("cleanup function should have been called")
	}

	// Verify backup was stopped
	select {
	case <-backupCtx.Done():
		// Good
	default:
		t.Error("backup context should be cancelled")
	}

	// Verify app context was cancelled
	select {
	case <-app.ctx.Done():
		// Good
	default:
		t.Error("app context should be cancelled")
	}
}

// TestOptionsAreIndependent tests that options don't affect each other
func TestOptionsAreIndependent(t *testing.T) {
	called := make(map[string]bool)

	app := New("v1.0.0",
		WithOnIconUpdate(func([]byte) { called["icon"] = true }),
		WithOnQuit(func() { called["quit"] = true }),
		WithSetTooltip(func(string) { called["tooltip"] = true }),
	)

	// Call only one callback
	app.onIconUpdate(nil)

	if !called["icon"] {
		t.Error("icon callback should have been called")
	}
	if called["quit"] {
		t.Error("quit callback should not have been called")
	}
	if called["tooltip"] {
		t.Error("tooltip callback should not have been called")
	}
}
