package state

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRecordSuccess(t *testing.T) {
	s := &State{
		Backup: BackupState{
			ConsecutiveFailures: 5,
			LastError:           "previous error",
		},
	}

	before := time.Now()
	s.RecordSuccess()
	after := time.Now()

	if s.Backup.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", s.Backup.ConsecutiveFailures)
	}

	if s.Backup.LastError != "" {
		t.Errorf("LastError = %q, want empty", s.Backup.LastError)
	}

	if s.Backup.LastSuccess.Before(before) || s.Backup.LastSuccess.After(after) {
		t.Errorf("LastSuccess = %v, should be between %v and %v", s.Backup.LastSuccess, before, after)
	}

	if s.Backup.LastAttempt.Before(before) || s.Backup.LastAttempt.After(after) {
		t.Errorf("LastAttempt = %v, should be between %v and %v", s.Backup.LastAttempt, before, after)
	}

	// LastSuccess and LastAttempt should be the same after success
	if s.Backup.LastSuccess != s.Backup.LastAttempt {
		t.Errorf("LastSuccess (%v) != LastAttempt (%v)", s.Backup.LastSuccess, s.Backup.LastAttempt)
	}
}

func TestRecordFailure(t *testing.T) {
	s := &State{
		Backup: BackupState{
			ConsecutiveFailures: 2,
		},
	}

	testErr := errors.New("backup failed: network error")
	before := time.Now()
	s.RecordFailure(testErr)
	after := time.Now()

	if s.Backup.ConsecutiveFailures != 3 {
		t.Errorf("ConsecutiveFailures = %d, want 3", s.Backup.ConsecutiveFailures)
	}

	if s.Backup.LastError != testErr.Error() {
		t.Errorf("LastError = %q, want %q", s.Backup.LastError, testErr.Error())
	}

	if s.Backup.LastAttempt.Before(before) || s.Backup.LastAttempt.After(after) {
		t.Errorf("LastAttempt = %v, should be between %v and %v", s.Backup.LastAttempt, before, after)
	}

	// LastSuccess should remain zero (not set on failure)
	if !s.Backup.LastSuccess.IsZero() {
		t.Errorf("LastSuccess = %v, want zero", s.Backup.LastSuccess)
	}
}

func TestRecordFailure_Increment(t *testing.T) {
	s := &State{}

	// Record multiple failures
	for i := 1; i <= 5; i++ {
		s.RecordFailure(errors.New("error"))
		if s.Backup.ConsecutiveFailures != i {
			t.Errorf("After %d failures, ConsecutiveFailures = %d, want %d", i, s.Backup.ConsecutiveFailures, i)
		}
	}
}

func TestHasBackedUpToday(t *testing.T) {
	loc := time.Local

	tests := []struct {
		name             string
		lastSuccess      time.Time
		hasBackedUpToday bool
	}{
		{
			name:             "zero time",
			lastSuccess:      time.Time{},
			hasBackedUpToday: false,
		},
		{
			name:             "today",
			lastSuccess:      time.Now(),
			hasBackedUpToday: true,
		},
		{
			name:             "yesterday",
			lastSuccess:      time.Now().Add(-24 * time.Hour),
			hasBackedUpToday: false,
		},
		{
			name:             "earlier today",
			lastSuccess:      time.Now().Add(-1 * time.Hour),
			hasBackedUpToday: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &State{
				Backup: BackupState{
					LastSuccess: tt.lastSuccess,
				},
			}
			result := s.HasBackedUpToday(loc)
			if result != tt.hasBackedUpToday {
				t.Errorf("HasBackedUpToday() = %v, want %v", result, tt.hasBackedUpToday)
			}
		})
	}
}

func TestHasBackedUpToday_DifferentTimezone(t *testing.T) {
	// Test that timezone is properly considered
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatalf("Failed to load UTC timezone: %v", err)
	}

	// Create a time that's "today" in UTC
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	s := &State{
		Backup: BackupState{
			LastSuccess: todayStart.Add(1 * time.Hour), // 1am today in UTC
		},
	}

	if !s.HasBackedUpToday(loc) {
		t.Error("HasBackedUpToday() should be true for backup earlier today")
	}
}

func TestLastSuccessAge(t *testing.T) {
	t.Run("zero time returns zero", func(t *testing.T) {
		s := &State{}
		age := s.LastSuccessAge()
		if age != 0 {
			t.Errorf("LastSuccessAge() = %v, want 0", age)
		}
	})

	t.Run("returns time since last success", func(t *testing.T) {
		backupTime := time.Now().Add(-2 * time.Hour)
		s := &State{
			Backup: BackupState{
				LastSuccess: backupTime,
			},
		}

		age := s.LastSuccessAge()

		// Should be approximately 2 hours (allow some tolerance for test execution)
		expected := 2 * time.Hour
		tolerance := 1 * time.Second

		if age < expected-tolerance || age > expected+tolerance {
			t.Errorf("LastSuccessAge() = %v, want ~%v", age, expected)
		}
	})
}

func TestLoadFromFile(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("non-existent file returns empty state", func(t *testing.T) {
		s, err := LoadFromFile(filepath.Join(tmpDir, "nonexistent.yaml"))
		if err != nil {
			t.Fatalf("LoadFromFile() error = %v", err)
		}
		if s == nil {
			t.Fatal("LoadFromFile() returned nil state")
		}
		if s.Backup.ConsecutiveFailures != 0 {
			t.Errorf("Expected empty state, got ConsecutiveFailures = %d", s.Backup.ConsecutiveFailures)
		}
	})

	t.Run("loads existing nested state", func(t *testing.T) {
		statePath := filepath.Join(tmpDir, "state_nested.yaml")
		stateContent := `backup:
  last_attempt: 2024-01-15T10:00:00Z
  last_success: 2024-01-15T10:00:00Z
  last_error: ""
  consecutive_failures: 0
`
		if err := os.WriteFile(statePath, []byte(stateContent), 0600); err != nil {
			t.Fatalf("Failed to write test state: %v", err)
		}

		s, err := LoadFromFile(statePath)
		if err != nil {
			t.Fatalf("LoadFromFile() error = %v", err)
		}

		if s.Backup.ConsecutiveFailures != 0 {
			t.Errorf("ConsecutiveFailures = %d, want 0", s.Backup.ConsecutiveFailures)
		}
		if s.Backup.LastSuccess.IsZero() {
			t.Error("LastSuccess should not be zero")
		}
	})

	t.Run("invalid YAML returns error", func(t *testing.T) {
		statePath := filepath.Join(tmpDir, "invalid.yaml")
		if err := os.WriteFile(statePath, []byte("invalid: yaml: content:"), 0600); err != nil {
			t.Fatalf("Failed to write test state: %v", err)
		}

		_, err := LoadFromFile(statePath)
		if err == nil {
			t.Error("LoadFromFile() should error for invalid YAML")
		}
	})
}

func TestSaveToFile(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.yaml")

	s := &State{
		Backup: BackupState{
			LastAttempt:         time.Now(),
			LastSuccess:         time.Now(),
			LastError:           "",
			ConsecutiveFailures: 0,
		},
	}

	if err := s.SaveToFile(statePath); err != nil {
		t.Fatalf("SaveToFile() error = %v", err)
	}

	// Verify file was created with correct permissions
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("State file not created: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("State file permissions = %o, want 0600", info.Mode().Perm())
	}

	// Load it back and verify
	loaded, err := LoadFromFile(statePath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if loaded.Backup.ConsecutiveFailures != s.Backup.ConsecutiveFailures {
		t.Errorf("Loaded ConsecutiveFailures = %d, want %d", loaded.Backup.ConsecutiveFailures, s.Backup.ConsecutiveFailures)
	}
}

func TestSaveToFile_WithFailures(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.yaml")

	s := &State{
		Backup: BackupState{
			ConsecutiveFailures: 3,
			LastError:           "network error",
		},
	}
	s.RecordFailure(errors.New("connection refused"))

	if err := s.SaveToFile(statePath); err != nil {
		t.Fatalf("SaveToFile() error = %v", err)
	}

	// Load it back
	loaded, err := LoadFromFile(statePath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if loaded.Backup.ConsecutiveFailures != 4 {
		t.Errorf("Loaded ConsecutiveFailures = %d, want 4", loaded.Backup.ConsecutiveFailures)
	}
	if loaded.Backup.LastError != "connection refused" {
		t.Errorf("Loaded LastError = %q, want %q", loaded.Backup.LastError, "connection refused")
	}
}

func TestStatePreservation_UpdateDoesNotAffectBackup(t *testing.T) {
	// Regression test: modifying Update fields and saving should not affect Backup fields
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.yaml")

	// Create initial state with valid backup timestamps
	initialContent := `backup:
  last_attempt: 2025-12-28T10:00:00Z
  last_success: 2025-12-28T10:00:00Z
  last_error: ""
  consecutive_failures: 0
update:
  last_check: 2025-12-28T09:00:00Z
`
	if err := os.WriteFile(statePath, []byte(initialContent), 0600); err != nil {
		t.Fatalf("Failed to write initial state: %v", err)
	}

	// Load state
	s, err := LoadFromFile(statePath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	// Verify backup.last_success was loaded correctly
	if s.Backup.LastSuccess.IsZero() {
		t.Fatal("Backup.LastSuccess should not be zero after load")
	}
	originalLastSuccess := s.Backup.LastSuccess

	// Modify only Update fields (simulating what update orchestrator does)
	s.Update.LastCheck = time.Now()

	// Save state
	if err := s.SaveToFile(statePath); err != nil {
		t.Fatalf("SaveToFile() error = %v", err)
	}

	// Reload state
	s2, err := LoadFromFile(statePath)
	if err != nil {
		t.Fatalf("LoadFromFile() after save error = %v", err)
	}

	// Verify backup.last_success is preserved
	if s2.Backup.LastSuccess.IsZero() {
		t.Error("Backup.LastSuccess should not be zero after reload")
	}
	if !s2.Backup.LastSuccess.Equal(originalLastSuccess) {
		t.Errorf("Backup.LastSuccess changed: got %v, want %v", s2.Backup.LastSuccess, originalLastSuccess)
	}
}

func TestConcurrentAccess(t *testing.T) {
	// Test that concurrent access to State is safe.
	// This test verifies the mutex protection works correctly.
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.yaml")

	s := &State{
		Backup: BackupState{
			LastSuccess: time.Now(),
		},
	}

	// Use WaitGroup to coordinate goroutines
	var wg sync.WaitGroup
	const numGoroutines = 10
	const numOperations = 100

	// Goroutines that call RecordSuccess
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				s.RecordSuccess()
			}
		}()
	}

	// Goroutines that call RecordFailure
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				s.RecordFailure(errors.New("test error"))
			}
		}()
	}

	// Goroutines that read HasBackedUpToday
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = s.HasBackedUpToday(time.Local)
			}
		}()
	}

	// Goroutines that read LastSuccessAge
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = s.LastSuccessAge()
			}
		}()
	}

	// Goroutines that call SetLastUpdateCheck
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				s.SetLastUpdateCheck(time.Now())
			}
		}()
	}

	// Goroutines that call GetLastUpdateCheck
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = s.GetLastUpdateCheck()
			}
		}()
	}

	// Goroutines that save state
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations/10; j++ {
				_ = s.SaveToFile(statePath)
			}
		}()
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// If we get here without data races or panics, the mutex is working
	t.Log("Concurrent access test completed without race conditions")
}

func TestIsPaused(t *testing.T) {
	tests := []struct {
		name        string
		pausedUntil time.Time
		want        bool
	}{
		{
			name:        "zero time is not paused",
			pausedUntil: time.Time{},
			want:        false,
		},
		{
			name:        "future time is paused",
			pausedUntil: time.Now().Add(1 * time.Hour),
			want:        true,
		},
		{
			name:        "past time is not paused",
			pausedUntil: time.Now().Add(-1 * time.Hour),
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &State{
				Backup: BackupState{
					PausedUntil: tt.pausedUntil,
				},
			}
			if got := s.IsPaused(); got != tt.want {
				t.Errorf("IsPaused() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClearPause(t *testing.T) {
	s := &State{
		Backup: BackupState{
			PausedUntil: time.Now().Add(1 * time.Hour),
		},
	}

	if !s.IsPaused() {
		t.Fatal("IsPaused() should be true before ClearPause")
	}

	s.ClearPause()

	if s.IsPaused() {
		t.Error("IsPaused() should be false after ClearPause")
	}
	if !s.Backup.PausedUntil.IsZero() {
		t.Errorf("PausedUntil should be zero, got %v", s.Backup.PausedUntil)
	}
}

func TestClearPause_WhenNotPaused(t *testing.T) {
	s := &State{}

	// Should not panic when clearing an already-cleared pause
	s.ClearPause()

	if s.IsPaused() {
		t.Error("IsPaused() should be false")
	}
}

func TestRecordSuccessClearsPause(t *testing.T) {
	s := &State{
		Backup: BackupState{
			PausedUntil:         time.Now().Add(1 * time.Hour),
			ConsecutiveFailures: 5,
		},
	}

	if !s.IsPaused() {
		t.Fatal("IsPaused() should be true before RecordSuccess")
	}

	s.RecordSuccess()

	if s.IsPaused() {
		t.Error("IsPaused() should be false after RecordSuccess")
	}
	if !s.Backup.PausedUntil.IsZero() {
		t.Errorf("PausedUntil should be zero, got %v", s.Backup.PausedUntil)
	}
}

func TestRecordNonRetryableFailure(t *testing.T) {
	loc := time.Local
	s := &State{
		Backup: BackupState{
			ConsecutiveFailures: 2,
		},
	}

	testErr := errors.New("password command failed")
	before := time.Now()
	s.RecordNonRetryableFailure(testErr, loc)
	after := time.Now()

	// Should increment consecutive failures
	if s.Backup.ConsecutiveFailures != 3 {
		t.Errorf("ConsecutiveFailures = %d, want 3", s.Backup.ConsecutiveFailures)
	}

	// Should record error
	if s.Backup.LastError != testErr.Error() {
		t.Errorf("LastError = %q, want %q", s.Backup.LastError, testErr.Error())
	}

	// Should record attempt time
	if s.Backup.LastAttempt.Before(before) || s.Backup.LastAttempt.After(after) {
		t.Errorf("LastAttempt = %v, should be between %v and %v", s.Backup.LastAttempt, before, after)
	}

	// Should be paused
	if !s.IsPaused() {
		t.Error("IsPaused() should be true after RecordNonRetryableFailure")
	}

	// Should be paused until midnight tomorrow
	now := time.Now().In(loc)
	expectedPause := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)
	if !s.Backup.PausedUntil.Equal(expectedPause) {
		t.Errorf("PausedUntil = %v, want %v", s.Backup.PausedUntil, expectedPause)
	}
}

func TestRecordNonRetryableFailure_DifferentTimezone(t *testing.T) {
	// Test that the pause is set correctly in a different timezone
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("Failed to load timezone: %v", err)
	}

	s := &State{}
	s.RecordNonRetryableFailure(errors.New("test error"), loc)

	// Pause should be until midnight in the specified timezone
	now := time.Now().In(loc)
	expectedPause := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)
	if !s.Backup.PausedUntil.Equal(expectedPause) {
		t.Errorf("PausedUntil = %v, want %v", s.Backup.PausedUntil, expectedPause)
	}
}

func TestPausePersistence(t *testing.T) {
	// Test that PausedUntil is persisted to and loaded from file
	tmpDir := t.TempDir()
	statePath := tmpDir + "/state.yaml"

	pauseTime := time.Now().Add(1 * time.Hour).Truncate(time.Second)
	s := &State{
		Backup: BackupState{
			PausedUntil: pauseTime,
		},
	}

	if err := s.SaveToFile(statePath); err != nil {
		t.Fatalf("SaveToFile() error = %v", err)
	}

	loaded, err := LoadFromFile(statePath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	// Compare with truncation since YAML may lose some precision
	if !loaded.Backup.PausedUntil.Truncate(time.Second).Equal(pauseTime) {
		t.Errorf("Loaded PausedUntil = %v, want %v", loaded.Backup.PausedUntil, pauseTime)
	}
}
