// Package scheduler manages backup scheduling and timing.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	cron "github.com/robfig/cron/v3"

	"neubibackup/internal/config"
	"neubibackup/internal/idle"
	"neubibackup/internal/network"
	"neubibackup/internal/power"
	"neubibackup/internal/state"
)

// BackupFunc is called when a backup should be executed.
type BackupFunc func()

// tickInterval is how often the scheduler wakes to evaluate whether a backup
// is due. Kept short (1 min) so the actual fire time tracks the cron schedule
// closely — independent of the user-facing minScheduleGap (15m), which limits
// how often a backup can be configured to run, not how often we wake to check.
const tickInterval = 1 * time.Minute

// Scheduler manages backup timing and triggers.
type Scheduler struct {
	config     *config.Config
	state      *state.State
	onBackup   BackupFunc
	location   *time.Location
	schedule   cron.Schedule
	minGap     time.Duration
	nextTickAt time.Time // when the next checkAndTrigger will run; zero before Start
	mu         sync.Mutex
	running    bool
}

// New creates a new Scheduler.
func New(cfg *config.Config, st *state.State, onBackup BackupFunc) (*Scheduler, error) {
	loc, err := getLocation(cfg)
	if err != nil {
		return nil, err
	}

	sched, minGap, err := cfg.Schedule.ParseSchedule()
	if err != nil {
		return nil, fmt.Errorf("scheduler: %w", err)
	}

	return &Scheduler{
		config:   cfg,
		state:    st,
		onBackup: onBackup,
		location: loc,
		schedule: sched,
		minGap:   minGap,
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

	sched, minGap, err := cfg.Schedule.ParseSchedule()
	if err != nil {
		return fmt.Errorf("scheduler: %w", err)
	}

	s.config = cfg
	s.location = loc
	s.schedule = sched
	s.minGap = minGap
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
	s.mu.Lock()
	s.nextTickAt = time.Now().Add(tickInterval)
	s.mu.Unlock()

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	// Check immediately on start
	s.checkAndTrigger()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			s.mu.Lock()
			s.nextTickAt = t.Add(tickInterval)
			s.mu.Unlock()
			s.checkAndTrigger()
		}
	}
}

// TriggerNow manually triggers a backup regardless of schedule.
func (s *Scheduler) TriggerNow() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		slog.Info("Backup already running, skipping manual trigger")
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

// Location returns the timezone location used by the scheduler.
func (s *Scheduler) Location() *time.Location {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.location
}

// MinGap returns the minimum gap between consecutive fires for the current schedule.
func (s *Scheduler) MinGap() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.minGap
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
	s.state.RecordScheduledFire()
	s.mu.Unlock()

	if err := s.state.Save(); err != nil {
		slog.Error("Failed to persist scheduled fire time", "error", err)
	}

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
	if !s.isBackupDue() {
		return false
	}
	if !s.checkBatteryOK() {
		return false
	}
	if !s.checkSSIDOK() {
		return false
	}
	if !s.checkUserActive() {
		return false
	}
	return true
}

// isBackupDue returns true if the schedule has fired at least once since the
// scheduler last fired a backup (or ever, if it has never fired).
// Must be called with mu held.
func (s *Scheduler) isBackupDue() bool {
	last := s.scheduleAnchor()
	if last.IsZero() {
		return true
	}
	next := s.schedule.Next(last)
	return !time.Now().Before(next)
}

// scheduleAnchor returns the timestamp used as the "previous fire" reference
// for cron computations. Prefers LastScheduledFire (set whenever the scheduler
// fires a backup, independent of duration or outcome). Falls back to
// LastSuccess for state files written before LastScheduledFire was introduced.
func (s *Scheduler) scheduleAnchor() time.Time {
	if t := s.state.GetLastScheduledFire(); !t.IsZero() {
		return t
	}
	return s.state.GetLastSuccess()
}

// checkBatteryOK returns true if battery status allows backup to proceed.
// Must be called with mu held.
func (s *Scheduler) checkBatteryOK() bool {
	if !s.config.Schedule.SkipOnBattery {
		return true
	}

	if power.GetBatteryStatus() == power.BatteryStatusOnBattery {
		slog.Info("Skipping scheduled backup - running on battery power")
		return false
	}

	return true
}

// checkSSIDOK returns true if WiFi SSID allows backup to proceed.
// Must be called with mu held.
func (s *Scheduler) checkSSIDOK() bool {
	if len(s.config.Schedule.AllowedSSIDs) == 0 {
		return true
	}

	netInfo := network.GetCurrentNetwork()
	slog.Debug("Checking WiFi SSID restriction",
		"status", netInfo.Status,
		"ssid", netInfo.SSID,
		"allowed_ssids", s.config.Schedule.AllowedSSIDs)

	if netInfo.Status == network.NetworkStatusConnected {
		if !s.isSSIDAllowed(netInfo.SSID) {
			slog.Info("Skipping scheduled backup - not on allowed SSID",
				"current_ssid", netInfo.SSID,
				"allowed_ssids", s.config.Schedule.AllowedSSIDs)
			return false
		}
		slog.Info("WiFi SSID allowed, proceeding with backup",
			"current_ssid", netInfo.SSID)
	} else {
		slog.Info("WiFi SSID not available - proceeding with backup (fail-open)",
			"hint", "On macOS, grant Location permission in System Settings for SSID detection")
	}

	return true
}

// checkUserActive returns true if user activity allows backup to proceed.
func (s *Scheduler) checkUserActive() bool {
	const maxIdleTime = 2 * time.Hour
	idleTime := idle.GetIdleTime()

	if idleTime > maxIdleTime {
		slog.Info("Skipping scheduled backup - user has been idle too long",
			"idle_time", idleTime.Round(time.Minute),
			"max_idle", maxIdleTime)
		return false
	}

	return true
}

// NextBackupTime returns when the next scheduled backup will actually fire.
// The result accounts for the scheduler's tick granularity: backups can only
// fire at a tick boundary, so the returned time is rounded up to the first
// tick at or after the cron schedule's next fire.
//
// If no successful backup has happened yet, returns time.Now() (the immediate
// startup check should fire one momentarily).
func (s *Scheduler) NextBackupTime() (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	last := s.scheduleAnchor()
	if last.IsZero() {
		return time.Now(), nil
	}

	scheduledFire := s.schedule.Next(last)
	now := time.Now()

	// Overdue (schedule already fired): the actual run happens at the next
	// scheduler tick. Without a tick clock (no Start() yet — unit-test path),
	// fall back to "now" so callers see "Backup due".
	if !scheduledFire.After(now) {
		if s.nextTickAt.IsZero() {
			return now, nil
		}
		return s.nextTickAt, nil
	}

	// Future fire. Without a tick clock, return the raw schedule fire.
	if s.nextTickAt.IsZero() {
		return scheduledFire, nil
	}

	// Scheduled fire is at or before the next tick: the actual fire is the
	// next tick.
	if !scheduledFire.After(s.nextTickAt) {
		return s.nextTickAt, nil
	}

	// Scheduled fire is after the next tick: round up to the first tick at or
	// after the scheduled fire.
	gap := scheduledFire.Sub(s.nextTickAt)
	ticks := int(gap / tickInterval)
	if gap%tickInterval != 0 {
		ticks++
	}
	return s.nextTickAt.Add(time.Duration(ticks) * tickInterval), nil
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

// isSSIDAllowed checks if the given SSID is in the allowed SSIDs list.
func (s *Scheduler) isSSIDAllowed(ssid string) bool {
	for _, allowed := range s.config.Schedule.AllowedSSIDs {
		if allowed == ssid {
			return true
		}
	}
	return false
}
