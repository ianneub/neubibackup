// Package state manages the application state (last backup time, errors, etc.).
package state

import (
	"fmt"
	"os"
	"time"

	"neubibackup/internal/config"

	"gopkg.in/yaml.v3"
)

// BackupState holds backup-related state.
type BackupState struct {
	LastAttempt         time.Time `yaml:"last_attempt"`
	LastSuccess         time.Time `yaml:"last_success"`
	LastError           string    `yaml:"last_error"`
	ConsecutiveFailures int       `yaml:"consecutive_failures"`
}

// UpdateState holds update-related state.
type UpdateState struct {
	LastCheck     time.Time `yaml:"last_check,omitempty"`
	LastVersion   string    `yaml:"last_version,omitempty"`
	LastTime      time.Time `yaml:"last_time,omitempty"`
	LastError     string    `yaml:"last_error,omitempty"`
	LastErrorTime time.Time `yaml:"last_error_time,omitempty"`
}

// State represents the application state.
type State struct {
	Backup BackupState `yaml:"backup,omitempty"`
	Update UpdateState `yaml:"update,omitempty"`

	// Legacy fields for backward compatibility (omitempty so they won't save)
	LegacyLastBackupAttempt   time.Time `yaml:"last_backup_attempt,omitempty"`
	LegacyLastBackupSuccess   time.Time `yaml:"last_backup_success,omitempty"`
	LegacyLastBackupError     string    `yaml:"last_backup_error,omitempty"`
	LegacyConsecutiveFailures int       `yaml:"consecutive_failures,omitempty"`
	LegacyLastUpdateCheck     time.Time `yaml:"last_update_check,omitempty"`
	LegacyLastUpdateVersion   string    `yaml:"last_update_version,omitempty"`
	LegacyLastUpdateTime      time.Time `yaml:"last_update_time,omitempty"`
	LegacyLastUpdateError     string    `yaml:"last_update_error,omitempty"`
	LegacyLastUpdateErrorTime time.Time `yaml:"last_update_error_time,omitempty"`
}

// Load reads the state from the default state file.
// Returns an empty state if the file doesn't exist.
func Load() (*State, error) {
	statePath, err := config.GetStatePath()
	if err != nil {
		return nil, fmt.Errorf("getting state path: %w", err)
	}
	return LoadFromFile(statePath)
}

// LoadFromFile reads the state from a specific file.
// Returns an empty state if the file doesn't exist.
func LoadFromFile(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &State{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var s State
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	// Migrate legacy fields to nested structure
	s.migrate()

	return &s, nil
}

// migrate converts legacy flat fields to the new nested structure.
// This is called after loading to support old state.yaml files.
func (s *State) migrate() {
	// Migrate backup fields if legacy fields have values but nested fields don't
	if !s.LegacyLastBackupAttempt.IsZero() && s.Backup.LastAttempt.IsZero() {
		s.Backup.LastAttempt = s.LegacyLastBackupAttempt
	}
	if !s.LegacyLastBackupSuccess.IsZero() && s.Backup.LastSuccess.IsZero() {
		s.Backup.LastSuccess = s.LegacyLastBackupSuccess
	}
	if s.LegacyLastBackupError != "" && s.Backup.LastError == "" {
		s.Backup.LastError = s.LegacyLastBackupError
	}
	if s.LegacyConsecutiveFailures != 0 && s.Backup.ConsecutiveFailures == 0 {
		s.Backup.ConsecutiveFailures = s.LegacyConsecutiveFailures
	}

	// Migrate update fields
	if !s.LegacyLastUpdateCheck.IsZero() && s.Update.LastCheck.IsZero() {
		s.Update.LastCheck = s.LegacyLastUpdateCheck
	}
	if s.LegacyLastUpdateVersion != "" && s.Update.LastVersion == "" {
		s.Update.LastVersion = s.LegacyLastUpdateVersion
	}
	if !s.LegacyLastUpdateTime.IsZero() && s.Update.LastTime.IsZero() {
		s.Update.LastTime = s.LegacyLastUpdateTime
	}
	if s.LegacyLastUpdateError != "" && s.Update.LastError == "" {
		s.Update.LastError = s.LegacyLastUpdateError
	}
	if !s.LegacyLastUpdateErrorTime.IsZero() && s.Update.LastErrorTime.IsZero() {
		s.Update.LastErrorTime = s.LegacyLastUpdateErrorTime
	}

	// Clear legacy fields so they won't be saved
	s.LegacyLastBackupAttempt = time.Time{}
	s.LegacyLastBackupSuccess = time.Time{}
	s.LegacyLastBackupError = ""
	s.LegacyConsecutiveFailures = 0
	s.LegacyLastUpdateCheck = time.Time{}
	s.LegacyLastUpdateVersion = ""
	s.LegacyLastUpdateTime = time.Time{}
	s.LegacyLastUpdateError = ""
	s.LegacyLastUpdateErrorTime = time.Time{}
}

// Save writes the state to the default state file.
func (s *State) Save() error {
	statePath, err := config.GetStatePath()
	if err != nil {
		return fmt.Errorf("getting state path: %w", err)
	}
	return s.SaveToFile(statePath)
}

// SaveToFile writes the state to a specific file.
func (s *State) SaveToFile(path string) error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}

	return nil
}

// RecordSuccess updates the state after a successful backup.
func (s *State) RecordSuccess() {
	now := time.Now()
	s.Backup.LastAttempt = now
	s.Backup.LastSuccess = now
	s.Backup.LastError = ""
	s.Backup.ConsecutiveFailures = 0
}

// RecordFailure updates the state after a failed backup.
func (s *State) RecordFailure(err error) {
	s.Backup.LastAttempt = time.Now()
	s.Backup.LastError = err.Error()
	s.Backup.ConsecutiveFailures++
}

// LastSuccessAge returns how long ago the last successful backup was.
// Returns a zero duration if there has never been a successful backup.
func (s *State) LastSuccessAge() time.Duration {
	if s.Backup.LastSuccess.IsZero() {
		return 0
	}
	return time.Since(s.Backup.LastSuccess)
}

// HasBackedUpToday returns true if there was a successful backup today.
func (s *State) HasBackedUpToday(loc *time.Location) bool {
	if s.Backup.LastSuccess.IsZero() {
		return false
	}

	now := time.Now().In(loc)
	last := s.Backup.LastSuccess.In(loc)

	return now.Year() == last.Year() &&
		now.Month() == last.Month() &&
		now.Day() == last.Day()
}

// GetLastUpdateCheck returns the time of the last update check.
func (s *State) GetLastUpdateCheck() time.Time {
	return s.Update.LastCheck
}

// SetLastUpdateCheck sets the time of the last update check.
func (s *State) SetLastUpdateCheck(t time.Time) {
	s.Update.LastCheck = t
}

// SetLastUpdateError records an update error.
func (s *State) SetLastUpdateError(err string, t time.Time) {
	s.Update.LastError = err
	s.Update.LastErrorTime = t
}

// SetLastUpdateSuccess records a successful update.
func (s *State) SetLastUpdateSuccess(version string, t time.Time) {
	s.Update.LastVersion = version
	s.Update.LastTime = t
	s.Update.LastError = ""
}
