// Package scheduler manages backup scheduling and timing.
package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"neubibackup/internal/config"
	"neubibackup/internal/state"
)

// BackupFunc is called when a backup should be executed.
type BackupFunc func()

// Scheduler manages backup timing and triggers.
type Scheduler struct {
	config     *config.Config
	state      *state.State
	onBackup   BackupFunc
	location   *time.Location
	mu         sync.Mutex
	running    bool
}

// New creates a new Scheduler.
func New(cfg *config.Config, st *state.State, onBackup BackupFunc) (*Scheduler, error) {
	loc, err := getLocation(cfg)
	if err != nil {
		return nil, err
	}

	return &Scheduler{
		config:   cfg,
		state:    st,
		onBackup: onBackup,
		location: loc,
	}, nil
}

// UpdateConfig updates the scheduler with a new configuration.
func (s *Scheduler) UpdateConfig(cfg *config.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	loc, err := getLocation(cfg)
	if err != nil {
		return err
	}

	s.config = cfg
	s.location = loc
	return nil
}

// UpdateState updates the scheduler with new state.
func (s *Scheduler) UpdateState(st *state.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = st
}

// Start begins the schedule loop. Call in a goroutine.
func (s *Scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Check immediately on start
	s.checkAndTrigger()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkAndTrigger()
		}
	}
}

// OnWake should be called when the system wakes from sleep.
// It checks if a backup was missed and triggers one if needed.
func (s *Scheduler) OnWake() {
	log.Println("System wake detected, checking for missed backup...")
	s.checkAndTrigger()
}

// TriggerNow manually triggers a backup regardless of schedule.
func (s *Scheduler) TriggerNow() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		log.Println("Backup already running, skipping manual trigger")
		return
	}
	s.running = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()
		s.onBackup()
	}()
}

// IsRunning returns true if a backup is currently in progress.
func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Scheduler) checkAndTrigger() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}

	if !s.shouldRunNow() {
		s.mu.Unlock()
		return
	}

	s.running = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()
		s.onBackup()
	}()
}

// shouldRunNow checks if a backup should be triggered.
// Must be called with mu held.
func (s *Scheduler) shouldRunNow() bool {
	scheduleTime, err := parseTime(s.config.Schedule.Time)
	if err != nil {
		log.Printf("Invalid schedule time %q: %v", s.config.Schedule.Time, err)
		return false
	}

	now := time.Now().In(s.location)

	// Get today's scheduled time
	todaySchedule := time.Date(
		now.Year(), now.Month(), now.Day(),
		scheduleTime.Hour(), scheduleTime.Minute(), 0, 0,
		s.location,
	)

	// If it's before today's scheduled time, no backup needed
	if now.Before(todaySchedule) {
		return false
	}

	// If we already backed up today (after the scheduled time), no backup needed
	if s.state.HasBackedUpToday(s.location) {
		lastBackup := s.state.Backup.LastSuccess.In(s.location)
		if !lastBackup.Before(todaySchedule) {
			return false
		}
	}

	// We're past the scheduled time and haven't backed up yet today
	return true
}

// NextBackupTime returns when the next backup is scheduled.
func (s *Scheduler) NextBackupTime() (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	scheduleTime, err := parseTime(s.config.Schedule.Time)
	if err != nil {
		return time.Time{}, err
	}

	now := time.Now().In(s.location)

	// Get today's scheduled time
	todaySchedule := time.Date(
		now.Year(), now.Month(), now.Day(),
		scheduleTime.Hour(), scheduleTime.Minute(), 0, 0,
		s.location,
	)

	// If we already passed today's schedule, return tomorrow's
	if now.After(todaySchedule) {
		return todaySchedule.AddDate(0, 0, 1), nil
	}

	return todaySchedule, nil
}

func parseTime(timeStr string) (time.Time, error) {
	// Try 24-hour format
	t, err := time.Parse("15:04", timeStr)
	if err == nil {
		return t, nil
	}

	// Try with seconds
	t, err = time.Parse("15:04:05", timeStr)
	if err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid time format: %s (expected HH:MM)", timeStr)
}

func getLocation(cfg *config.Config) (*time.Location, error) {
	if cfg.Schedule.Timezone == "" {
		return time.Local, nil
	}

	loc, err := time.LoadLocation(cfg.Schedule.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", cfg.Schedule.Timezone, err)
	}

	return loc, nil
}
