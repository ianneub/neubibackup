// Package state manages the application state (last backup time, errors, etc.).
package state

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
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
// All methods are thread-safe and can be called from multiple goroutines.
type State struct {
	mu     sync.RWMutex `yaml:"-"` // Protects all fields below
	Backup BackupState  `yaml:"backup,omitempty"`
	Update UpdateState  `yaml:"update,omitempty"`
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

	slog.Info("State loaded",
		"backup.last_success", s.Backup.LastSuccess,
		"backup.consecutive_failures", s.Backup.ConsecutiveFailures)

	return &s, nil
}

// Save writes the state to the default state file.
func (s *State) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Warn if we're about to save state with zero LastSuccess but non-zero LastAttempt
	// This could indicate a bug where state was not properly loaded
	if s.Backup.LastSuccess.IsZero() && !s.Backup.LastAttempt.IsZero() {
		slog.Warn("Saving state with zero LastSuccess - this may indicate a state loading issue",
			"last_attempt", s.Backup.LastAttempt)
	}

	statePath, err := config.GetStatePath()
	if err != nil {
		return fmt.Errorf("getting state path: %w", err)
	}
	return s.saveToFileLocked(statePath)
}

// SaveToFile writes the state to a specific file.
func (s *State) SaveToFile(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveToFileLocked(path)
}

// saveToFileLocked writes the state to a specific file atomically. Caller must hold s.mu.
// Uses write-to-temp-then-rename pattern to prevent corruption if interrupted.
func (s *State) saveToFileLocked(path string) error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	// Write to temp file first to ensure atomic write
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("writing temp state file: %w", err)
	}

	// Atomic rename - on POSIX systems this is atomic within the same filesystem
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Clean up temp file on failure
		return fmt.Errorf("renaming state file: %w", err)
	}

	return nil
}

// RecordSuccess updates the state after a successful backup.
func (s *State) RecordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.Backup.LastAttempt = now
	s.Backup.LastSuccess = now
	s.Backup.LastError = ""
	s.Backup.ConsecutiveFailures = 0
}

// RecordFailure updates the state after a failed backup.
func (s *State) RecordFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Backup.LastAttempt = time.Now()
	s.Backup.LastError = err.Error()
	s.Backup.ConsecutiveFailures++
}

// LastSuccessAge returns how long ago the last successful backup was.
// Returns a zero duration if there has never been a successful backup.
func (s *State) LastSuccessAge() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.Backup.LastSuccess.IsZero() {
		return 0
	}
	return time.Since(s.Backup.LastSuccess)
}

// GetLastUpdateCheck returns the time of the last update check.
func (s *State) GetLastUpdateCheck() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Update.LastCheck
}

// SetLastUpdateCheck sets the time of the last update check.
func (s *State) SetLastUpdateCheck(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Update.LastCheck = t
}

// SetLastUpdateError records an update error.
func (s *State) SetLastUpdateError(err string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Update.LastError = err
	s.Update.LastErrorTime = t
}

// SetLastUpdateSuccess records a successful update.
func (s *State) SetLastUpdateSuccess(version string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Update.LastVersion = version
	s.Update.LastTime = t
	s.Update.LastError = ""
}

// GetBackupState returns a copy of the current backup state.
// This provides a consistent snapshot of all backup fields for UI display.
func (s *State) GetBackupState() BackupState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Backup
}

// GetLastSuccess returns the time of the last successful backup.
func (s *State) GetLastSuccess() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Backup.LastSuccess
}

// GetConsecutiveFailures returns the number of consecutive backup failures.
func (s *State) GetConsecutiveFailures() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Backup.ConsecutiveFailures
}

// HasSuccessfulBackup returns true if there has ever been a successful backup.
func (s *State) HasSuccessfulBackup() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.Backup.LastSuccess.IsZero()
}

