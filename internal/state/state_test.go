package state

import (
	"errors"
	"fmt"
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

func TestRecordScheduledFire(t *testing.T) {
	s := &State{}

	if got := s.GetLastScheduledFire(); !got.IsZero() {
		t.Errorf("GetLastScheduledFire() before any fire = %v, want zero", got)
	}

	before := time.Now()
	s.RecordScheduledFire()
	after := time.Now()

	got := s.GetLastScheduledFire()
	if got.Before(before) || got.After(after) {
		t.Errorf("GetLastScheduledFire() = %v, should be between %v and %v", got, before, after)
	}

	// RecordScheduledFire must not touch LastSuccess / LastAttempt — those track
	// the outcome of a backup, not the schedule firing.
	if !s.Backup.LastSuccess.IsZero() {
		t.Errorf("LastSuccess = %v, want zero (RecordScheduledFire should not set it)", s.Backup.LastSuccess)
	}
	if !s.Backup.LastAttempt.IsZero() {
		t.Errorf("LastAttempt = %v, want zero (RecordScheduledFire should not set it)", s.Backup.LastAttempt)
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

	// Goroutines that read HasSuccessfulBackup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = s.HasSuccessfulBackup()
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

func TestAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.yaml")

	// Create initial state
	s := &State{}
	s.RecordSuccess()

	if err := s.SaveToFile(statePath); err != nil {
		t.Fatalf("SaveToFile() error = %v", err)
	}

	// Verify the temp file doesn't exist (should be cleaned up)
	tmpPath := statePath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("Temp file should not exist after successful save")
	}

	// Verify state was saved correctly
	loaded, err := LoadFromFile(statePath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if loaded.Backup.LastSuccess.IsZero() {
		t.Error("LastSuccess should not be zero after RecordSuccess")
	}
}

func TestGetBackupState(t *testing.T) {
	s := &State{}

	// Initially all zero
	backup := s.GetBackupState()
	if !backup.LastSuccess.IsZero() {
		t.Error("Expected LastSuccess to be zero initially")
	}
	if backup.ConsecutiveFailures != 0 {
		t.Error("Expected ConsecutiveFailures to be 0 initially")
	}

	// After success
	s.RecordSuccess()
	backup = s.GetBackupState()
	if backup.LastSuccess.IsZero() {
		t.Error("Expected LastSuccess to be set after RecordSuccess")
	}
	if backup.ConsecutiveFailures != 0 {
		t.Errorf("Expected ConsecutiveFailures = 0, got %d", backup.ConsecutiveFailures)
	}

	// After failure
	s.RecordFailure(fmt.Errorf("test error"))
	backup = s.GetBackupState()
	if backup.ConsecutiveFailures != 1 {
		t.Errorf("Expected ConsecutiveFailures = 1, got %d", backup.ConsecutiveFailures)
	}
	if backup.LastError != "test error" {
		t.Errorf("Expected LastError = 'test error', got %q", backup.LastError)
	}
}

func TestGetters(t *testing.T) {
	s := &State{}

	// Test GetLastSuccess
	if !s.GetLastSuccess().IsZero() {
		t.Error("GetLastSuccess should return zero initially")
	}

	// Test GetConsecutiveFailures
	if s.GetConsecutiveFailures() != 0 {
		t.Error("GetConsecutiveFailures should return 0 initially")
	}

	// Test HasSuccessfulBackup
	if s.HasSuccessfulBackup() {
		t.Error("HasSuccessfulBackup should return false initially")
	}

	// After success
	s.RecordSuccess()
	if s.GetLastSuccess().IsZero() {
		t.Error("GetLastSuccess should not be zero after RecordSuccess")
	}
	if !s.HasSuccessfulBackup() {
		t.Error("HasSuccessfulBackup should return true after RecordSuccess")
	}

	// After failures
	for i := 0; i < 3; i++ {
		s.RecordFailure(fmt.Errorf("error %d", i))
	}
	if s.GetConsecutiveFailures() != 3 {
		t.Errorf("GetConsecutiveFailures = %d, want 3", s.GetConsecutiveFailures())
	}
}

func TestConcurrentGetterAccess(t *testing.T) {
	s := &State{}

	var wg sync.WaitGroup
	const numGoroutines = 10
	const numOperations = 100

	// Writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				s.RecordSuccess()
			}
		}()
	}

	// Readers using new getters
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = s.GetBackupState()
				_ = s.GetLastSuccess()
				_ = s.GetConsecutiveFailures()
				_ = s.HasSuccessfulBackup()
			}
		}()
	}

	wg.Wait()
}
