package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordSuccess(t *testing.T) {
	s := &State{
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

	// LastBackupSuccess and LastBackupAttempt should be the same after success
	if s.LastBackupSuccess != s.LastBackupAttempt {
		t.Errorf("LastBackupSuccess (%v) != LastBackupAttempt (%v)", s.LastBackupSuccess, s.LastBackupAttempt)
	}
}

func TestRecordFailure(t *testing.T) {
	s := &State{
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

	// LastBackupSuccess should remain zero (not set on failure)
	if !s.LastBackupSuccess.IsZero() {
		t.Errorf("LastBackupSuccess = %v, want zero", s.LastBackupSuccess)
	}
}

func TestRecordFailure_Increment(t *testing.T) {
	s := &State{}

	// Record multiple failures
	for i := 1; i <= 5; i++ {
		s.RecordFailure(errors.New("error"))
		if s.ConsecutiveFailures != i {
			t.Errorf("After %d failures, ConsecutiveFailures = %d, want %d", i, s.ConsecutiveFailures, i)
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
				LastBackupSuccess: tt.lastSuccess,
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
		LastBackupSuccess: todayStart.Add(1 * time.Hour), // 1am today in UTC
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
			LastBackupSuccess: backupTime,
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
		if s.ConsecutiveFailures != 0 {
			t.Errorf("Expected empty state, got ConsecutiveFailures = %d", s.ConsecutiveFailures)
		}
	})

	t.Run("loads existing state", func(t *testing.T) {
		statePath := filepath.Join(tmpDir, "state.yaml")
		stateContent := `last_backup_attempt: 2024-01-15T10:00:00Z
last_backup_success: 2024-01-15T10:00:00Z
last_backup_error: ""
consecutive_failures: 0
`
		if err := os.WriteFile(statePath, []byte(stateContent), 0600); err != nil {
			t.Fatalf("Failed to write test state: %v", err)
		}

		s, err := LoadFromFile(statePath)
		if err != nil {
			t.Fatalf("LoadFromFile() error = %v", err)
		}

		if s.ConsecutiveFailures != 0 {
			t.Errorf("ConsecutiveFailures = %d, want 0", s.ConsecutiveFailures)
		}
		if s.LastBackupSuccess.IsZero() {
			t.Error("LastBackupSuccess should not be zero")
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
		LastBackupAttempt:   time.Now(),
		LastBackupSuccess:   time.Now(),
		LastBackupError:     "",
		ConsecutiveFailures: 0,
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

	if loaded.ConsecutiveFailures != s.ConsecutiveFailures {
		t.Errorf("Loaded ConsecutiveFailures = %d, want %d", loaded.ConsecutiveFailures, s.ConsecutiveFailures)
	}
}

func TestSaveToFile_WithFailures(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.yaml")

	s := &State{
		ConsecutiveFailures: 3,
		LastBackupError:     "network error",
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

	if loaded.ConsecutiveFailures != 4 {
		t.Errorf("Loaded ConsecutiveFailures = %d, want 4", loaded.ConsecutiveFailures)
	}
	if loaded.LastBackupError != "connection refused" {
		t.Errorf("Loaded LastBackupError = %q, want %q", loaded.LastBackupError, "connection refused")
	}
}
