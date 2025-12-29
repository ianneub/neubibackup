package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestMigration_LegacyToNested(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("migrates legacy backup fields", func(t *testing.T) {
		statePath := filepath.Join(tmpDir, "legacy_backup.yaml")
		// Old format with flat fields
		stateContent := `last_backup_attempt: 2024-01-15T10:00:00Z
last_backup_success: 2024-01-15T09:00:00Z
last_backup_error: "network error"
consecutive_failures: 3
`
		if err := os.WriteFile(statePath, []byte(stateContent), 0600); err != nil {
			t.Fatalf("Failed to write test state: %v", err)
		}

		s, err := LoadFromFile(statePath)
		if err != nil {
			t.Fatalf("LoadFromFile() error = %v", err)
		}

		// Verify migration to nested fields
		if s.Backup.LastAttempt.IsZero() {
			t.Error("Backup.LastAttempt should be migrated")
		}
		if s.Backup.LastSuccess.IsZero() {
			t.Error("Backup.LastSuccess should be migrated")
		}
		if s.Backup.LastError != "network error" {
			t.Errorf("Backup.LastError = %q, want %q", s.Backup.LastError, "network error")
		}
		if s.Backup.ConsecutiveFailures != 3 {
			t.Errorf("Backup.ConsecutiveFailures = %d, want 3", s.Backup.ConsecutiveFailures)
		}

		// Verify legacy fields are cleared
		if !s.LegacyLastBackupAttempt.IsZero() {
			t.Error("LegacyLastBackupAttempt should be cleared")
		}
		if !s.LegacyLastBackupSuccess.IsZero() {
			t.Error("LegacyLastBackupSuccess should be cleared")
		}
		if s.LegacyLastBackupError != "" {
			t.Error("LegacyLastBackupError should be cleared")
		}
		if s.LegacyConsecutiveFailures != 0 {
			t.Error("LegacyConsecutiveFailures should be cleared")
		}
	})

	t.Run("migrates legacy update fields", func(t *testing.T) {
		statePath := filepath.Join(tmpDir, "legacy_update.yaml")
		stateContent := `last_update_check: 2024-01-15T10:00:00Z
last_update_version: "1.2.3"
last_update_time: 2024-01-15T10:01:00Z
last_update_error: "update failed"
last_update_error_time: 2024-01-15T10:00:30Z
`
		if err := os.WriteFile(statePath, []byte(stateContent), 0600); err != nil {
			t.Fatalf("Failed to write test state: %v", err)
		}

		s, err := LoadFromFile(statePath)
		if err != nil {
			t.Fatalf("LoadFromFile() error = %v", err)
		}

		// Verify migration to nested fields
		if s.Update.LastCheck.IsZero() {
			t.Error("Update.LastCheck should be migrated")
		}
		if s.Update.LastVersion != "1.2.3" {
			t.Errorf("Update.LastVersion = %q, want %q", s.Update.LastVersion, "1.2.3")
		}
		if s.Update.LastTime.IsZero() {
			t.Error("Update.LastTime should be migrated")
		}
		if s.Update.LastError != "update failed" {
			t.Errorf("Update.LastError = %q, want %q", s.Update.LastError, "update failed")
		}
		if s.Update.LastErrorTime.IsZero() {
			t.Error("Update.LastErrorTime should be migrated")
		}

		// Verify legacy fields are cleared
		if !s.LegacyLastUpdateCheck.IsZero() {
			t.Error("LegacyLastUpdateCheck should be cleared")
		}
	})

	t.Run("saves in new format after migration", func(t *testing.T) {
		statePath := filepath.Join(tmpDir, "migration_save.yaml")
		// Old format
		stateContent := `last_backup_success: 2024-01-15T09:00:00Z
consecutive_failures: 2
`
		if err := os.WriteFile(statePath, []byte(stateContent), 0600); err != nil {
			t.Fatalf("Failed to write test state: %v", err)
		}

		// Load (triggers migration)
		s, err := LoadFromFile(statePath)
		if err != nil {
			t.Fatalf("LoadFromFile() error = %v", err)
		}

		// Save in new format
		newPath := filepath.Join(tmpDir, "migration_save_new.yaml")
		if err := s.SaveToFile(newPath); err != nil {
			t.Fatalf("SaveToFile() error = %v", err)
		}

		// Read raw file content to verify format
		content, err := os.ReadFile(newPath)
		if err != nil {
			t.Fatalf("Failed to read saved file: %v", err)
		}

		// Should contain nested structure, not flat
		contentStr := string(content)
		if !strings.Contains(contentStr, "backup:") {
			t.Error("Saved file should contain 'backup:' nested key")
		}
		if strings.Contains(contentStr, "last_backup_success:") {
			t.Error("Saved file should not contain legacy 'last_backup_success:' key")
		}
		if strings.Contains(contentStr, "consecutive_failures:") && !strings.Contains(contentStr, "  consecutive_failures:") {
			t.Error("consecutive_failures should be nested under backup:")
		}
	})
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
