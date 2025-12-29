package main

import (
	"context"
	"errors"
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
		Backup: state.BackupState{
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
}

// TestStateRecordFailure tests the state recording for failed backups
func TestStateRecordFailure(t *testing.T) {
	s := &state.State{
		Backup: state.BackupState{
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
}

// TestStateHasBackedUpToday tests the HasBackedUpToday method
func TestStateHasBackedUpToday(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &state.State{
				Backup: state.BackupState{
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

// TestStateUpdateFields tests the new update tracking fields in state
func TestStateUpdateFields(t *testing.T) {
	s := &state.State{}

	// Test initial state
	if s.Update.LastVersion != "" {
		t.Errorf("LastVersion should be empty initially, got %q", s.Update.LastVersion)
	}
	if !s.Update.LastTime.IsZero() {
		t.Errorf("LastTime should be zero initially, got %v", s.Update.LastTime)
	}
	if s.Update.LastError != "" {
		t.Errorf("LastError should be empty initially, got %q", s.Update.LastError)
	}
	if !s.Update.LastErrorTime.IsZero() {
		t.Errorf("LastErrorTime should be zero initially, got %v", s.Update.LastErrorTime)
	}

	// Set update success fields
	now := time.Now()
	s.Update.LastVersion = "v1.2.3"
	s.Update.LastTime = now
	s.Update.LastError = ""

	if s.Update.LastVersion != "v1.2.3" {
		t.Errorf("LastVersion = %q, want %q", s.Update.LastVersion, "v1.2.3")
	}
	if s.Update.LastTime != now {
		t.Errorf("LastTime = %v, want %v", s.Update.LastTime, now)
	}

	// Set update error fields
	errTime := time.Now()
	s.Update.LastError = "network error"
	s.Update.LastErrorTime = errTime

	if s.Update.LastError != "network error" {
		t.Errorf("LastError = %q, want %q", s.Update.LastError, "network error")
	}
	if s.Update.LastErrorTime != errTime {
		t.Errorf("LastErrorTime = %v, want %v", s.Update.LastErrorTime, errTime)
	}
}
