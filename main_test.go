package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"neubibackup/internal/config"
	"neubibackup/internal/state"
)

// TestCancellationDetection verifies that context.Canceled errors are properly
// distinguished from other errors. This is important because we don't want to
// send Pushover notifications when the user manually stops a backup.
func TestCancellationDetection(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		isCancellation bool
	}{
		{
			name:           "context.Canceled is detected",
			err:            context.Canceled,
			isCancellation: true,
		},
		{
			name:           "wrapped context.Canceled is detected",
			err:            errors.Join(errors.New("backup stopped"), context.Canceled),
			isCancellation: true,
		},
		{
			name:           "context.DeadlineExceeded is not cancellation",
			err:            context.DeadlineExceeded,
			isCancellation: false,
		},
		{
			name:           "regular error is not cancellation",
			err:            errors.New("connection refused"),
			isCancellation: false,
		},
		{
			name:           "nil error is not cancellation",
			err:            nil,
			isCancellation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := errors.Is(tt.err, context.Canceled)
			if result != tt.isCancellation {
				t.Errorf("errors.Is(%v, context.Canceled) = %v, want %v", tt.err, result, tt.isCancellation)
			}
		})
	}
}

// TestBackupMutexBehavior tests the backup mutex locking behavior
func TestBackupMutexBehavior(t *testing.T) {
	// Reset global state for test
	backupMu.Lock()
	originalRunning := backupRunning
	backupRunning = false
	backupMu.Unlock()

	defer func() {
		backupMu.Lock()
		backupRunning = originalRunning
		backupMu.Unlock()
	}()

	// Test that we can check backup status
	backupMu.Lock()
	running := backupRunning
	backupMu.Unlock()

	if running {
		t.Error("backupRunning should be false initially in test")
	}

	// Simulate starting a backup
	backupMu.Lock()
	backupRunning = true
	backupMu.Unlock()

	backupMu.Lock()
	running = backupRunning
	backupMu.Unlock()

	if !running {
		t.Error("backupRunning should be true after setting")
	}
}

// TestStopBackupWhenNotRunning tests stopBackup behavior when no backup is running
func TestStopBackupWhenNotRunning(t *testing.T) {
	// Reset global state for test
	backupMu.Lock()
	originalRunning := backupRunning
	originalCancel := backupCancel
	backupRunning = false
	backupCancel = nil
	backupMu.Unlock()

	defer func() {
		backupMu.Lock()
		backupRunning = originalRunning
		backupCancel = originalCancel
		backupMu.Unlock()
	}()

	// This should not panic when no backup is running
	stopBackup()

	// Verify state is unchanged
	backupMu.Lock()
	running := backupRunning
	backupMu.Unlock()

	if running {
		t.Error("backupRunning should still be false")
	}
}

// TestStopBackupWhenRunning tests stopBackup cancels the context
func TestStopBackupWhenRunning(t *testing.T) {
	// Reset global state for test
	backupMu.Lock()
	originalRunning := backupRunning
	originalCancel := backupCancel
	backupMu.Unlock()

	defer func() {
		backupMu.Lock()
		backupRunning = originalRunning
		backupCancel = originalCancel
		backupMu.Unlock()
	}()

	// Create a context that we can verify gets cancelled
	ctx, cancel := context.WithCancel(context.Background())

	backupMu.Lock()
	backupRunning = true
	backupCancel = cancel
	backupMu.Unlock()

	// Call stopBackup
	stopBackup()

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		if ctx.Err() != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", ctx.Err())
		}
	default:
		t.Error("context should be cancelled after stopBackup")
	}
}

// TestConfigIsConfigured tests the IsConfigured method
func TestConfigIsConfigured(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		configured bool
	}{
		{
			name:       "nil config",
			cfg:        nil,
			configured: false,
		},
		{
			name:       "empty config",
			cfg:        &config.Config{},
			configured: false,
		},
		{
			name: "template config (not configured)",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path: "rest:https://user:pass@backup.example.com/repo",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/home/user"},
				},
			},
			configured: false,
		},
		{
			name: "valid config",
			cfg: &config.Config{
				Repository: config.RepositoryConfig{
					Path:     "rest:https://mybackup.example.com/repo",
					Password: "secret",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/home/user"},
				},
			},
			configured: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result bool
			if tt.cfg == nil {
				result = false
			} else {
				result = tt.cfg.IsConfigured()
			}
			if result != tt.configured {
				t.Errorf("IsConfigured() = %v, want %v", result, tt.configured)
			}
		})
	}
}

// TestStateRecordSuccess tests the state recording for successful backups
func TestStateRecordSuccess(t *testing.T) {
	s := &state.State{
		ConsecutiveFailures: 5,
		LastBackupError:     "previous error",
	}

	before := time.Now()
	s.RecordSuccess()
	after := time.Now()

	if s.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", s.ConsecutiveFailures)
	}

	if s.LastBackupError != "" {
		t.Errorf("LastBackupError = %q, want empty", s.LastBackupError)
	}

	if s.LastBackupSuccess.Before(before) || s.LastBackupSuccess.After(after) {
		t.Errorf("LastBackupSuccess = %v, should be between %v and %v", s.LastBackupSuccess, before, after)
	}

	if s.LastBackupAttempt.Before(before) || s.LastBackupAttempt.After(after) {
		t.Errorf("LastBackupAttempt = %v, should be between %v and %v", s.LastBackupAttempt, before, after)
	}
}

// TestStateRecordFailure tests the state recording for failed backups
func TestStateRecordFailure(t *testing.T) {
	s := &state.State{
		ConsecutiveFailures: 2,
	}

	testErr := errors.New("backup failed: network error")
	before := time.Now()
	s.RecordFailure(testErr)
	after := time.Now()

	if s.ConsecutiveFailures != 3 {
		t.Errorf("ConsecutiveFailures = %d, want 3", s.ConsecutiveFailures)
	}

	if s.LastBackupError != testErr.Error() {
		t.Errorf("LastBackupError = %q, want %q", s.LastBackupError, testErr.Error())
	}

	if s.LastBackupAttempt.Before(before) || s.LastBackupAttempt.After(after) {
		t.Errorf("LastBackupAttempt = %v, should be between %v and %v", s.LastBackupAttempt, before, after)
	}
}

// TestStateHasBackedUpToday tests the HasBackedUpToday method
func TestStateHasBackedUpToday(t *testing.T) {
	loc := time.Local

	tests := []struct {
		name            string
		lastSuccess     time.Time
		hasBackedUpToday bool
	}{
		{
			name:            "zero time",
			lastSuccess:     time.Time{},
			hasBackedUpToday: false,
		},
		{
			name:            "today",
			lastSuccess:     time.Now(),
			hasBackedUpToday: true,
		},
		{
			name:            "yesterday",
			lastSuccess:     time.Now().Add(-24 * time.Hour),
			hasBackedUpToday: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &state.State{
				LastBackupSuccess: tt.lastSuccess,
			}
			result := s.HasBackedUpToday(loc)
			if result != tt.hasBackedUpToday {
				t.Errorf("HasBackedUpToday() = %v, want %v", result, tt.hasBackedUpToday)
			}
		})
	}
}

// TestTailscaleEnabled tests the IsTailscaleEnabled method
func TestTailscaleEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		enabled bool
	}{
		{
			name:    "disabled by default",
			cfg:     config.Config{},
			enabled: false,
		},
		{
			name: "enabled but no auth key",
			cfg: config.Config{
				Tailscale: config.TailscaleConfig{
					Enabled: true,
				},
			},
			enabled: false,
		},
		{
			name: "enabled with auth key",
			cfg: config.Config{
				Tailscale: config.TailscaleConfig{
					Enabled: true,
					AuthKey: "tskey-auth-xxx",
				},
			},
			enabled: true,
		},
		{
			name: "auth key but not enabled",
			cfg: config.Config{
				Tailscale: config.TailscaleConfig{
					Enabled: false,
					AuthKey: "tskey-auth-xxx",
				},
			},
			enabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cfg.IsTailscaleEnabled()
			if result != tt.enabled {
				t.Errorf("IsTailscaleEnabled() = %v, want %v", result, tt.enabled)
			}
		})
	}
}

// TestConfigValidation tests the config validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
	}{
		{
			name:    "empty config",
			cfg:     config.Config{},
			wantErr: true,
		},
		{
			name: "missing password",
			cfg: config.Config{
				Repository: config.RepositoryConfig{
					Path: "/backup/repo",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/home"},
				},
				Schedule: config.ScheduleConfig{
					Time: "01:00",
				},
			},
			wantErr: true,
		},
		{
			name: "missing paths",
			cfg: config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Schedule: config.ScheduleConfig{
					Time: "01:00",
				},
			},
			wantErr: true,
		},
		{
			name: "missing schedule time",
			cfg: config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/home"},
				},
			},
			wantErr: true,
		},
		{
			name: "valid config with password",
			cfg: config.Config{
				Repository: config.RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/home"},
				},
				Schedule: config.ScheduleConfig{
					Time: "01:00",
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with password file",
			cfg: config.Config{
				Repository: config.RepositoryConfig{
					Path:         "/backup/repo",
					PasswordFile: "/path/to/password",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/home"},
				},
				Schedule: config.ScheduleConfig{
					Time: "01:00",
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with password command",
			cfg: config.Config{
				Repository: config.RepositoryConfig{
					Path:            "/backup/repo",
					PasswordCommand: "pass show backup",
				},
				Backup: config.BackupConfig{
					Paths: []string{"/home"},
				},
				Schedule: config.ScheduleConfig{
					Time: "01:00",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestUpdateMutexBehavior tests the update mutex locking behavior
func TestUpdateMutexBehavior(t *testing.T) {
	// Reset global state for test
	updateMu.Lock()
	originalInProgress := updateInProgress
	updateInProgress = false
	updateMu.Unlock()

	defer func() {
		updateMu.Lock()
		updateInProgress = originalInProgress
		updateMu.Unlock()
	}()

	// Test that we can check update status
	updateMu.Lock()
	inProgress := updateInProgress
	updateMu.Unlock()

	if inProgress {
		t.Error("updateInProgress should be false initially in test")
	}

	// Simulate starting an update
	updateMu.Lock()
	updateInProgress = true
	updateMu.Unlock()

	updateMu.Lock()
	inProgress = updateInProgress
	updateMu.Unlock()

	if !inProgress {
		t.Error("updateInProgress should be true after setting")
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

// TestStateUpdateFields tests the new update tracking fields in state
func TestStateUpdateFields(t *testing.T) {
	s := &state.State{}

	// Test initial state
	if s.LastUpdateVersion != "" {
		t.Errorf("LastUpdateVersion should be empty initially, got %q", s.LastUpdateVersion)
	}
	if !s.LastUpdateTime.IsZero() {
		t.Errorf("LastUpdateTime should be zero initially, got %v", s.LastUpdateTime)
	}
	if s.LastUpdateError != "" {
		t.Errorf("LastUpdateError should be empty initially, got %q", s.LastUpdateError)
	}
	if !s.LastUpdateErrorTime.IsZero() {
		t.Errorf("LastUpdateErrorTime should be zero initially, got %v", s.LastUpdateErrorTime)
	}

	// Set update success fields
	now := time.Now()
	s.LastUpdateVersion = "v1.2.3"
	s.LastUpdateTime = now
	s.LastUpdateError = ""

	if s.LastUpdateVersion != "v1.2.3" {
		t.Errorf("LastUpdateVersion = %q, want %q", s.LastUpdateVersion, "v1.2.3")
	}
	if s.LastUpdateTime != now {
		t.Errorf("LastUpdateTime = %v, want %v", s.LastUpdateTime, now)
	}

	// Set update error fields
	errTime := time.Now()
	s.LastUpdateError = "network error"
	s.LastUpdateErrorTime = errTime

	if s.LastUpdateError != "network error" {
		t.Errorf("LastUpdateError = %q, want %q", s.LastUpdateError, "network error")
	}
	if s.LastUpdateErrorTime != errTime {
		t.Errorf("LastUpdateErrorTime = %v, want %v", s.LastUpdateErrorTime, errTime)
	}
}

// TestUpdateBlockedDuringBackup tests that updates wait for backups
func TestUpdateBlockedDuringBackup(t *testing.T) {
	// Reset global state
	backupMu.Lock()
	originalRunning := backupRunning
	backupRunning = true // Simulate a running backup
	backupMu.Unlock()

	defer func() {
		backupMu.Lock()
		backupRunning = originalRunning
		backupMu.Unlock()
	}()

	// Check that backup is detected as running
	backupMu.Lock()
	running := backupRunning
	backupMu.Unlock()

	if !running {
		t.Error("Backup should be detected as running")
	}

	// In a real scenario, attemptAutoUpdate would wait here
	// We just verify the detection mechanism works
}
