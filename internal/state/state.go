// Package state manages the application state (last backup time, errors, etc.).
package state

import (
	"fmt"
	"os"
	"time"

	"neubibackup/internal/config"

	"gopkg.in/yaml.v3"
)

// State represents the application state.
type State struct {
	LastBackupAttempt   time.Time `yaml:"last_backup_attempt"`
	LastBackupSuccess   time.Time `yaml:"last_backup_success"`
	LastBackupError     string    `yaml:"last_backup_error"`
	ConsecutiveFailures int       `yaml:"consecutive_failures"`
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

	return &s, nil
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
	s.LastBackupAttempt = now
	s.LastBackupSuccess = now
	s.LastBackupError = ""
	s.ConsecutiveFailures = 0
}

// RecordFailure updates the state after a failed backup.
func (s *State) RecordFailure(err error) {
	s.LastBackupAttempt = time.Now()
	s.LastBackupError = err.Error()
	s.ConsecutiveFailures++
}

// LastSuccessAge returns how long ago the last successful backup was.
// Returns a zero duration if there has never been a successful backup.
func (s *State) LastSuccessAge() time.Duration {
	if s.LastBackupSuccess.IsZero() {
		return 0
	}
	return time.Since(s.LastBackupSuccess)
}

// HasBackedUpToday returns true if there was a successful backup today.
func (s *State) HasBackedUpToday(loc *time.Location) bool {
	if s.LastBackupSuccess.IsZero() {
		return false
	}

	now := time.Now().In(loc)
	last := s.LastBackupSuccess.In(loc)

	return now.Year() == last.Year() &&
		now.Month() == last.Month() &&
		now.Day() == last.Day()
}
