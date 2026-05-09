# Cron Schedule Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the daily-at-time scheduler with a configurable cron expression / `@every` descriptor in `schedule.cron`, bump the config schema to v2 (rejecting v1 configs), enforce a 15-minute minimum gap, and surface the next backup time in the tray menu.

**Architecture:** `github.com/robfig/cron/v3` parses the cron string into a `cron.Schedule`. A new `ParseSchedule` helper on `ScheduleConfig` returns the schedule plus the minimum gap between consecutive fires (used both as a validation gate and as the input for log-retention sizing). The scheduler caches the parsed `cron.Schedule` and asks `Schedule.Next(lastSuccess)` whether a backup is due. The tray menu adds a second disabled item under "Last backup" driven by the existing 1-minute status ticker.

**Tech Stack:** Go 1.26, `github.com/robfig/cron/v3` (new), `gopkg.in/yaml.v3` (already present, switching to strict decoding), `github.com/getlantern/systray` (existing).

**Spec:** `docs/superpowers/specs/2026-05-09-hourly-backups-design.md`

---

## File Structure

**New files:** none (all changes go in existing files).

**Modified files:**

| File | Responsibility |
|---|---|
| `go.mod` / `go.sum` | Pin `github.com/robfig/cron/v3` |
| `internal/config/config.go` | `Cron` field, drop `Time`, `currentConfigVersion`, `ParseSchedule`, `minGapOf`, strict YAML decoding, version-aware `Validate` |
| `internal/config/config_test.go` | Tests for `ParseSchedule`, `minGapOf`, version validation, strict YAML, cron validation |
| `internal/config/template.go` | New `version: 2` template with `schedule.cron` block |
| `internal/scheduler/scheduler.go` | Cache `cron.Schedule`, rewrite `isBackupDue`/`NextBackupTime`, add `MinGap()`, drop `parseTime` |
| `internal/scheduler/scheduler_test.go` | Replace daily-anchor tests with cron tests |
| `internal/state/state.go` | Drop `HasBackedUpToday` / `HasBackedUpTodayAfter` |
| `internal/state/state_test.go` | Drop matching tests |
| `internal/logging/logging.go` | `CleanupOldLogs(maxFiles int)` signature, `RetentionFor(time.Duration) int` helper |
| `internal/logging/logging_test.go` | Update for new signature, add `RetentionFor` table |
| `internal/backup/orchestrator.go` | `WithLogRetention(int)` option, pass through to `CleanupOldLogs` |
| `internal/backup/orchestrator_test.go` | Cover the new option (light tests) |
| `internal/tray/status.go` | Extract `formatNextBackupAt(next, now)`; add tomorrow/weekday branches |
| `internal/tray/status_test.go` | Deterministic tests for new branches |
| `internal/tray/menu.go` | `mNextBackup` item, `ScheduleProvider` interface, `nextBackupMenuText` helper, wire into `setup()` / `UpdateStatus()` |
| `internal/tray/menu_test.go` | Tests for `nextBackupMenuText` and the `ScheduleProvider` mock |
| `internal/app/app.go` | Pass `WithLogRetention` to orchestrator, pass `a.sched` as `Schedule` provider to `MenuConfig` |
| `internal/config/template.go` | (see above; bump version + cron) |
| `README.md` | Replace daily-schedule docs with cron docs, add v1→v2 migration |
| `CHANGELOG.md` (new) | Document the v2 breaking change and migration |

---

## Task 1: Add cron dependency + ParseSchedule + minGapOf helpers

Add the dependency and the pure helpers without changing any existing behavior. The new `Cron` field is added alongside the existing `Time` field; `Validate` is untouched. Everything compiles and existing tests continue to pass.

**Files:**
- Modify: `go.mod`, `go.sum` (via `go get`)
- Modify: `internal/config/config.go` (add `Cron` field, helpers, constants — keep `Time` field)
- Modify: `internal/config/config_test.go` (add helper tests)

- [ ] **Step 1: Add the dependency**

Run:

```bash
go get github.com/robfig/cron/v3@latest
go mod tidy
```

Expected: `go.mod` lists `github.com/robfig/cron/v3 vX.Y.Z` under `require`. `go.sum` updates.

- [ ] **Step 2: Add Cron field, constants, and helper signatures (compile only)**

Edit `internal/config/config.go`. At the top of the file, add `time` and `cron/v3` imports if missing. Replace the `ScheduleConfig` struct (around line 32) so it includes `Cron`:

```go
// ScheduleConfig defines when backups should run.
type ScheduleConfig struct {
	Cron          string   `yaml:"cron"`            // Cron expression or "@every <duration>". Default: "@every 24h".
	Time          string   `yaml:"time"`            // DEPRECATED. Removed in schema v2; kept here only during migration window — see Task 4.
	Timezone      string   `yaml:"timezone"`        // Optional, defaults to system timezone
	SkipOnBattery bool     `yaml:"skip_on_battery"` // Skip scheduled backups when on battery power
	AllowedSSIDs  []string `yaml:"allowed_ssids"`   // Only run scheduled backups on these WiFi SSIDs (empty = no restriction)
}
```

Below the `IsTailscaleEnabled` method at the bottom of the file, append:

```go
// currentConfigVersion is the schema version this binary understands.
const currentConfigVersion = 2

// minScheduleGap is the smallest allowed delta between consecutive backup fires.
// Matches the scheduler's 15-minute ticker.
const minScheduleGap = 15 * time.Minute

// ParseSchedule validates and parses cfg.Schedule.Cron, returning the parsed
// schedule and the minimum gap between consecutive fires. Empty Cron defaults
// to "@every 24h". Returns an error if the spec parses but fires more often
// than minScheduleGap, or if the spec has no future fires.
func (s ScheduleConfig) ParseSchedule() (cron.Schedule, time.Duration, error) {
	spec := s.Cron
	if spec == "" {
		spec = "@every 24h"
	}
	sched, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, 0, fmt.Errorf("schedule.cron is not a valid cron expression or @every descriptor: %w", err)
	}
	minGap, err := minGapOf(sched)
	if err != nil {
		return nil, 0, err
	}
	if minGap < minScheduleGap {
		return nil, 0, fmt.Errorf("schedule.cron fires too frequently: minimum gap is %s, must be at least %s", minGap, minScheduleGap)
	}
	return sched, minGap, nil
}

// minGapOf walks the schedule from a fixed deterministic anchor and returns
// the smallest delta between consecutive fires. Bounded to 1000 iterations or
// one year of wall time, short-circuits as soon as a gap < minScheduleGap is
// found.
func minGapOf(sched cron.Schedule) (time.Duration, error) {
	const maxIters = 1000
	const maxSpan = 366 * 24 * time.Hour

	anchor := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prev := sched.Next(anchor)
	if prev.IsZero() {
		return 0, fmt.Errorf("schedule.cron has no future fires")
	}
	minGap := time.Duration(1<<63 - 1) // math.MaxInt64

	for i := 0; i < maxIters; i++ {
		next := sched.Next(prev)
		if next.IsZero() {
			break
		}
		gap := next.Sub(prev)
		if gap < minGap {
			minGap = gap
		}
		if minGap < minScheduleGap {
			return minGap, nil
		}
		if next.Sub(anchor) >= maxSpan {
			break
		}
		prev = next
	}
	return minGap, nil
}
```

Add the imports at the top (the `import` block currently has only `fmt`, `os`, `gopkg.in/yaml.v3`). The block should become:

```go
import (
	"fmt"
	"os"
	"time"

	cron "github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)
```

- [ ] **Step 3: Run the build to confirm it compiles**

Run:

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 4: Run the existing test suite (regression guard)**

Run:

```bash
go test ./internal/config/... ./internal/scheduler/... ./internal/state/...
```

Expected: all PASS. We have not changed any existing behavior yet.

- [ ] **Step 5: Write tests for ParseSchedule and minGapOf**

Append to `internal/config/config_test.go`:

```go
func TestParseSchedule_DefaultEmpty(t *testing.T) {
	s := ScheduleConfig{Cron: ""}
	sched, gap, err := s.ParseSchedule()
	if err != nil {
		t.Fatalf("ParseSchedule(\"\") err = %v", err)
	}
	if sched == nil {
		t.Fatal("ParseSchedule(\"\") returned nil schedule")
	}
	if gap != 24*time.Hour {
		t.Errorf("default min gap = %s, want 24h", gap)
	}
}

func TestParseSchedule_Valid(t *testing.T) {
	cases := []struct {
		spec string
		gap  time.Duration
	}{
		{"@every 1h", time.Hour},
		{"@every 30m", 30 * time.Minute},
		{"@every 15m", 15 * time.Minute},
		{"@every 24h", 24 * time.Hour},
		{"@daily", 24 * time.Hour},
		{"@hourly", time.Hour},
		{"0 1 * * *", 24 * time.Hour},
		{"*/15 * * * *", 15 * time.Minute},
		{"0 1,2 * * *", time.Hour},
		{"0 8,18 * * *", 10 * time.Hour}, // gaps are 10h and 14h; min is 10h
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			s := ScheduleConfig{Cron: tc.spec}
			sched, gap, err := s.ParseSchedule()
			if err != nil {
				t.Fatalf("ParseSchedule(%q) err = %v", tc.spec, err)
			}
			if sched == nil {
				t.Fatal("nil schedule")
			}
			if gap != tc.gap {
				t.Errorf("min gap = %s, want %s", gap, tc.gap)
			}
		})
	}
}

func TestParseSchedule_Rejected(t *testing.T) {
	cases := []struct {
		spec      string
		wantInErr string
	}{
		{"@every 10m", "fires too frequently"},
		{"@every 0s", "fires too frequently"},
		{"*/10 * * * *", "fires too frequently"},
		{"0,5 * * * *", "fires too frequently"},
		{"* * * * *", "fires too frequently"},
		{"garbage", "not a valid cron expression"},
		{"60 * * * *", "not a valid cron expression"},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			s := ScheduleConfig{Cron: tc.spec}
			_, _, err := s.ParseSchedule()
			if err == nil {
				t.Fatalf("ParseSchedule(%q) expected error, got nil", tc.spec)
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("ParseSchedule(%q) err = %q, want to contain %q", tc.spec, err.Error(), tc.wantInErr)
			}
		})
	}
}

func TestParseSchedule_PathologicalShortCircuits(t *testing.T) {
	// Regression guard: "* * * * *" must not iterate the full 1000-fire / 1-year
	// budget — it should detect a sub-15m gap on the first iteration.
	s := ScheduleConfig{Cron: "* * * * *"}
	start := time.Now()
	_, _, err := s.ParseSchedule()
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error for '* * * * *'")
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("ParseSchedule short-circuit took %s, want < 100ms", elapsed)
	}
}
```

The `strings` import already exists in `config_test.go` (used for the existing `Validate` tests). The `time` import is new — add it to the existing import block.

- [ ] **Step 6: Run the new tests**

Run:

```bash
go test ./internal/config/... -run 'TestParseSchedule' -v
```

Expected: all four tests PASS.

- [ ] **Step 7: Run the full test suite**

Run:

```bash
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add cron parsing helpers and dependency

Adds github.com/robfig/cron/v3 plus ParseSchedule / minGapOf helpers on
ScheduleConfig. The Cron field is added to the struct; existing
behavior is unchanged because Validate and the scheduler still use the
old Time field. Helpers are unit-tested in isolation."
```

---

## Task 2: Migrate scheduler to cron-based scheduling

Switch `internal/scheduler/scheduler.go` to use the parsed `cron.Schedule` from `ParseSchedule`. Drop `parseTime`, the today-anchor logic, and the `HasBackedUpTodayAfter` call. Add a `MinGap()` accessor. Rewrite the scheduler tests to reflect the new semantics.

After this task, the scheduler ignores `cfg.Schedule.Time` entirely and runs on `@every 24h` by default. Existing user configs with only `Time:` set will silently shift to the rolling 24h cadence — but this is OK because Task 4 will reject those configs at load time. We accept this temporary state because doing the schema bump first would break the scheduler's compilation.

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/scheduler/scheduler_test.go`

- [ ] **Step 1: Rewrite scheduler.go**

Replace the entire contents of `internal/scheduler/scheduler.go` with:

```go
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

// Scheduler manages backup timing and triggers.
type Scheduler struct {
	config   *config.Config
	state    *state.State
	onBackup BackupFunc
	location *time.Location
	schedule cron.Schedule
	minGap   time.Duration
	mu       sync.Mutex
	running  bool
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
	ticker := time.NewTicker(15 * time.Minute)
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
// last successful backup (or ever, if no successful backup has happened).
// Must be called with mu held.
func (s *Scheduler) isBackupDue() bool {
	last := s.state.GetLastSuccess()
	if last.IsZero() {
		return true
	}
	next := s.schedule.Next(last)
	return !time.Now().Before(next)
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

// NextBackupTime returns when the next scheduled backup will fire.
// If the schedule is overdue, returns time.Now().
func (s *Scheduler) NextBackupTime() (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	last := s.state.GetLastSuccess()
	if last.IsZero() {
		return time.Now(), nil
	}
	next := s.schedule.Next(last)
	if !time.Now().Before(next) {
		return time.Now(), nil
	}
	return next, nil
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
```

- [ ] **Step 2: Rewrite scheduler_test.go**

Replace the entire contents of `internal/scheduler/scheduler_test.go` with:

```go
package scheduler

import (
	"sync"
	"testing"
	"time"

	"neubibackup/internal/config"
	"neubibackup/internal/state"
)

func newTestConfig(crontab string) *config.Config {
	return &config.Config{
		Version: 2,
		Schedule: config.ScheduleConfig{
			Cron: crontab,
		},
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{name: "default cron (empty)", cfg: newTestConfig(""), wantErr: false},
		{name: "interval cron", cfg: newTestConfig("@every 1h"), wantErr: false},
		{name: "wall-clock cron", cfg: newTestConfig("0 1 * * *"), wantErr: false},
		{
			name: "valid timezone",
			cfg: &config.Config{
				Version: 2,
				Schedule: config.ScheduleConfig{
					Cron:     "@every 1h",
					Timezone: "America/New_York",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid timezone",
			cfg: &config.Config{
				Version: 2,
				Schedule: config.ScheduleConfig{
					Cron:     "@every 1h",
					Timezone: "Invalid/Timezone",
				},
			},
			wantErr: true,
		},
		{name: "invalid cron", cfg: newTestConfig("garbage"), wantErr: true},
		{name: "sub-15m cron", cfg: newTestConfig("@every 5m"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(tt.cfg, &state.State{}, func() {})
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && s == nil {
				t.Error("New() returned nil scheduler without error")
			}
		})
	}
}

func TestIsBackupDue_NeverRun(t *testing.T) {
	s, err := New(newTestConfig("@every 1h"), &state.State{}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	got := s.isBackupDue()
	s.mu.Unlock()
	if !got {
		t.Error("isBackupDue() = false on never-run scheduler, want true")
	}
}

func TestIsBackupDue_Interval(t *testing.T) {
	tests := []struct {
		name     string
		spec     string
		lastAgo  time.Duration
		wantDue  bool
	}{
		{"30m ago / @every 1h", "@every 1h", 30 * time.Minute, false},
		{"2h ago / @every 1h", "@every 1h", 2 * time.Hour, true},
		{"1h ago / @every 24h", "@every 24h", 1 * time.Hour, false},
		{"25h ago / @every 24h", "@every 24h", 25 * time.Hour, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &state.State{}
			st.Backup.LastSuccess = time.Now().Add(-tt.lastAgo)

			s, err := New(newTestConfig(tt.spec), st, func() {})
			if err != nil {
				t.Fatal(err)
			}
			s.mu.Lock()
			got := s.isBackupDue()
			s.mu.Unlock()
			if got != tt.wantDue {
				t.Errorf("isBackupDue() = %v, want %v", got, tt.wantDue)
			}
		})
	}
}

func TestNextBackupTime_NeverRun(t *testing.T) {
	s, err := New(newTestConfig("@every 1h"), &state.State{}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.NextBackupTime()
	if err != nil {
		t.Fatal(err)
	}
	if delta := time.Since(got); delta > time.Second || delta < -time.Second {
		t.Errorf("NextBackupTime() never-run = %s away from now (want ~0)", delta)
	}
}

func TestNextBackupTime_FutureFire(t *testing.T) {
	st := &state.State{}
	st.Backup.LastSuccess = time.Now().Add(-30 * time.Minute) // 1h schedule → next at +30m
	s, err := New(newTestConfig("@every 1h"), st, func() {})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.NextBackupTime()
	if err != nil {
		t.Fatal(err)
	}
	until := time.Until(got)
	if until < 25*time.Minute || until > 35*time.Minute {
		t.Errorf("NextBackupTime() = %s away, want ~30m", until)
	}
}

func TestNextBackupTime_OverdueReturnsNow(t *testing.T) {
	st := &state.State{}
	st.Backup.LastSuccess = time.Now().Add(-2 * time.Hour) // 1h schedule → overdue
	s, err := New(newTestConfig("@every 1h"), st, func() {})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.NextBackupTime()
	if err != nil {
		t.Fatal(err)
	}
	if delta := time.Since(got); delta > time.Second || delta < -time.Second {
		t.Errorf("NextBackupTime() overdue = %s away from now (want ~0)", delta)
	}
}

func TestUpdateConfig_RecachesSchedule(t *testing.T) {
	st := &state.State{}
	st.Backup.LastSuccess = time.Now().Add(-2 * time.Hour)

	s, err := New(newTestConfig("@every 24h"), st, func() {})
	if err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	dueBefore := s.isBackupDue()
	s.mu.Unlock()
	if dueBefore {
		t.Fatal("isBackupDue() = true with @every 24h and 2h-old success, want false")
	}

	if err := s.UpdateConfig(newTestConfig("@every 1h")); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	s.mu.Lock()
	dueAfter := s.isBackupDue()
	s.mu.Unlock()
	if !dueAfter {
		t.Error("isBackupDue() = false after UpdateConfig to @every 1h with 2h-old success, want true")
	}
}

func TestMinGap(t *testing.T) {
	s, err := New(newTestConfig("@every 1h"), &state.State{}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.MinGap(); got != time.Hour {
		t.Errorf("MinGap() = %s, want 1h", got)
	}

	if err := s.UpdateConfig(newTestConfig("@every 30m")); err != nil {
		t.Fatal(err)
	}
	if got := s.MinGap(); got != 30*time.Minute {
		t.Errorf("MinGap() after update = %s, want 30m", got)
	}
}

func TestTriggerNow(t *testing.T) {
	t.Run("triggers callback", func(t *testing.T) {
		var called bool
		var mu sync.Mutex
		s, err := New(newTestConfig("@every 1h"), &state.State{}, func() {
			mu.Lock()
			called = true
			mu.Unlock()
		})
		if err != nil {
			t.Fatal(err)
		}
		s.TriggerNow()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if !called {
			t.Error("TriggerNow() did not call onBackup")
		}
	})

	t.Run("skips if already running", func(t *testing.T) {
		callCount := 0
		var mu sync.Mutex
		onBackup := func() {
			mu.Lock()
			callCount++
			mu.Unlock()
			time.Sleep(100 * time.Millisecond)
		}
		s, err := New(newTestConfig("@every 1h"), &state.State{}, onBackup)
		if err != nil {
			t.Fatal(err)
		}
		s.TriggerNow()
		time.Sleep(10 * time.Millisecond)
		s.TriggerNow()
		time.Sleep(150 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if callCount != 1 {
			t.Errorf("callCount = %d, want 1", callCount)
		}
	})
}

func TestIsRunning(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})
	s, err := New(newTestConfig("@every 1h"), &state.State{}, func() {
		close(started)
		<-done
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.IsRunning() {
		t.Error("IsRunning() should be false initially")
	}
	s.TriggerNow()
	<-started
	if !s.IsRunning() {
		t.Error("IsRunning() should be true while running")
	}
	close(done)
	time.Sleep(50 * time.Millisecond)
	if s.IsRunning() {
		t.Error("IsRunning() should be false after completion")
	}
}

func TestTriggerNow_IgnoresBatteryStatus(t *testing.T) {
	cfg := newTestConfig("@every 1h")
	cfg.Schedule.SkipOnBattery = true

	var called bool
	var mu sync.Mutex
	s, err := New(cfg, &state.State{}, func() {
		mu.Lock()
		called = true
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	s.TriggerNow()
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Error("TriggerNow() must run regardless of skip_on_battery")
	}
}

func TestTriggerNow_IgnoresSSIDRestriction(t *testing.T) {
	cfg := newTestConfig("@every 1h")
	cfg.Schedule.AllowedSSIDs = []string{"HomeWiFi"}

	var called bool
	var mu sync.Mutex
	s, err := New(cfg, &state.State{}, func() {
		mu.Lock()
		called = true
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	s.TriggerNow()
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Error("TriggerNow() must run regardless of allowed_ssids")
	}
}

func TestIsSSIDAllowed(t *testing.T) {
	tests := []struct {
		name         string
		allowedSSIDs []string
		ssid         string
		want         bool
	}{
		{"in list", []string{"HomeWiFi", "OfficeNetwork"}, "HomeWiFi", true},
		{"not in list", []string{"HomeWiFi", "OfficeNetwork"}, "CoffeeShop", false},
		{"empty list", []string{}, "AnyNetwork", false},
		{"case sensitive", []string{"HomeWiFi"}, "homewifi", false},
		{"exact match required", []string{"HomeWiFi"}, "HomeWiFi-5G", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newTestConfig("@every 1h")
			cfg.Schedule.AllowedSSIDs = tt.allowedSSIDs
			s, err := New(cfg, &state.State{}, func() {})
			if err != nil {
				t.Fatal(err)
			}
			if got := s.isSSIDAllowed(tt.ssid); got != tt.want {
				t.Errorf("isSSIDAllowed(%q) = %v, want %v", tt.ssid, got, tt.want)
			}
		})
	}
}

func TestCheckBatteryOK_Disabled(t *testing.T) {
	cfg := newTestConfig("@every 1h")
	cfg.Schedule.SkipOnBattery = false
	s, err := New(cfg, &state.State{}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.checkBatteryOK() {
		t.Error("checkBatteryOK() with skip_on_battery=false must return true")
	}
}

func TestCheckSSIDOK_NoRestriction(t *testing.T) {
	for _, allowed := range [][]string{nil, {}} {
		cfg := newTestConfig("@every 1h")
		cfg.Schedule.AllowedSSIDs = allowed
		s, err := New(cfg, &state.State{}, func() {})
		if err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		got := s.checkSSIDOK()
		s.mu.Unlock()
		if !got {
			t.Errorf("checkSSIDOK() with allowed_ssids=%v must return true", allowed)
		}
	}
}

func TestCheckUserActive_DoesNotPanic(t *testing.T) {
	s, err := New(newTestConfig("@every 1h"), &state.State{}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.checkUserActive()
}

func TestGetLocation(t *testing.T) {
	tests := []struct {
		name     string
		timezone string
		wantName string
		wantErr  bool
	}{
		{"empty uses local", "", time.Local.String(), false},
		{"valid timezone", "America/Los_Angeles", "America/Los_Angeles", false},
		{"UTC", "UTC", "UTC", false},
		{"invalid timezone", "Not/A/Timezone", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newTestConfig("@every 1h")
			cfg.Schedule.Timezone = tt.timezone
			got, err := getLocation(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("getLocation() err = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.String() != tt.wantName {
				t.Errorf("getLocation() = %q, want %q", got.String(), tt.wantName)
			}
		})
	}
}
```

- [ ] **Step 3: Run scheduler tests**

Run:

```bash
go test ./internal/scheduler/... -v
```

Expected: all PASS.

- [ ] **Step 4: Run the full test suite**

Run:

```bash
go test ./...
```

Expected: all PASS. State tests still pass because we have not changed `state.go` yet (the `HasBackedUpToday*` methods are now unused but still present).

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go
git commit -m "feat(scheduler): drive backups from cron.Schedule

Replaces the parseTime/today-anchor logic with a cached cron.Schedule.
isBackupDue compares Schedule.Next(lastSuccess) against time.Now().
NextBackupTime returns now if overdue, else the next future fire.
Adds MinGap() so the app can size log retention to the schedule.
Tests rewritten for the new semantics."
```

---

## Task 3: Drop unused state helpers

`HasBackedUpToday` and `HasBackedUpTodayAfter` had a single caller (the old daily-anchor scheduler) which is now gone. Remove them and their tests.

**Files:**
- Modify: `internal/state/state.go` (delete two methods)
- Modify: `internal/state/state_test.go` (delete matching tests)

- [ ] **Step 1: Confirm there are no remaining callers**

Run:

```bash
grep -rn "HasBackedUpToday" --include='*.go' .
```

Expected: matches only inside `internal/state/state.go` and `internal/state/state_test.go`.

- [ ] **Step 2: Delete `HasBackedUpToday` from state.go**

In `internal/state/state.go`, delete these lines (157–172):

```go
// HasBackedUpToday returns true if there was a successful backup today.
func (s *State) HasBackedUpToday(loc *time.Location) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.Backup.LastSuccess.IsZero() {
		return false
	}

	now := time.Now().In(loc)
	last := s.Backup.LastSuccess.In(loc)

	return now.Year() == last.Year() &&
		now.Month() == last.Month() &&
		now.Day() == last.Day()
}
```

- [ ] **Step 3: Delete `HasBackedUpTodayAfter` from state.go**

In `internal/state/state.go`, delete these lines (234–259):

```go
// HasBackedUpTodayAfter returns true if there was a successful backup today
// at or after the specified time. This combines the day check and time check
// in a single lock acquisition to avoid TOCTOU races.
func (s *State) HasBackedUpTodayAfter(loc *time.Location, afterTime time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.Backup.LastSuccess.IsZero() {
		return false
	}

	now := time.Now().In(loc)
	last := s.Backup.LastSuccess.In(loc)

	// Check same day
	sameDay := now.Year() == last.Year() &&
		now.Month() == last.Month() &&
		now.Day() == last.Day()

	if !sameDay {
		return false
	}

	// Check if backup was at or after the specified time
	return !last.Before(afterTime)
}
```

- [ ] **Step 4: Delete the matching tests from state_test.go**

Run:

```bash
grep -n "TestHasBackedUpToday\|TestHasBackedUpTodayAfter\|^func.*HasBackedUpToday" /Users/ianneub/Code/neubibackup/internal/state/state_test.go
```

Open `internal/state/state_test.go` and delete every test function whose name starts with `TestHasBackedUpToday` (typically two: `TestHasBackedUpToday` and `TestHasBackedUpTodayAfter`). Delete from `func TestX(t *testing.T) {` through the closing `}` of the function.

- [ ] **Step 5: Run state tests**

Run:

```bash
go test ./internal/state/... -v
```

Expected: all remaining tests PASS.

- [ ] **Step 6: Run the full test suite**

Run:

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/state/state.go internal/state/state_test.go
git commit -m "refactor(state): drop unused HasBackedUpToday helpers

Sole caller (the old daily-anchor scheduler) was removed in the
previous commit. The methods and their tests are no longer reachable."
```

---

## Task 4: Bump config schema to v2 (strict YAML, drop Time field, version-aware Validate, new template)

This is the breaking change. After this task, v1 configs are rejected at load time, the `Time` field no longer exists on the struct, and the on-disk template uses `version: 2` with `schedule.cron`.

**Files:**
- Modify: `internal/config/config.go` (drop `Time` from struct, switch to strict YAML decoding, rewrite `Validate`)
- Modify: `internal/config/config_test.go` (update fixtures to use `Cron` and `Version: 2`, add v2 migration tests)
- Modify: `internal/config/template.go` (new content)

- [ ] **Step 1: Drop the `Time` field**

In `internal/config/config.go`, replace the `ScheduleConfig` struct (added in Task 1, around the location where `Cron` and `Time` both currently appear):

```go
// ScheduleConfig defines when backups should run.
type ScheduleConfig struct {
	Cron          string   `yaml:"cron"`            // Cron expression or "@every <duration>". Default: "@every 24h".
	Timezone      string   `yaml:"timezone"`        // Optional, defaults to system timezone
	SkipOnBattery bool     `yaml:"skip_on_battery"` // Skip scheduled backups when on battery power
	AllowedSSIDs  []string `yaml:"allowed_ssids"`   // Only run scheduled backups on these WiFi SSIDs (empty = no restriction)
}
```

- [ ] **Step 2: Switch `LoadFromFile` to strict YAML decoding**

In `internal/config/config.go`, replace the existing `LoadFromFile` (around line 92):

```go
// LoadFromFile reads the config from a specific file.
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w (see README for v2 schema migration)", err)
	}

	return &cfg, nil
}
```

Add `bytes` to the import block in `config.go`. Final import block should be:

```go
import (
	"bytes"
	"fmt"
	"os"
	"time"

	cron "github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)
```

- [ ] **Step 3: Rewrite `Validate`**

In `internal/config/config.go`, replace the existing `Validate` function (around line 131) with:

```go
// Validate checks if the config has the minimum required fields and that the
// schema version matches the current binary.
func (c *Config) Validate() error {
	if c.Version != currentConfigVersion {
		return fmt.Errorf(
			"config.yaml is schema version %d, expected %d. "+
				"The 'schedule.time' field has been removed in favor of 'schedule.cron' "+
				"(see README for migration). Update your config and set 'version: %d'",
			c.Version, currentConfigVersion, currentConfigVersion)
	}
	if c.Repository.Path == "" {
		return fmt.Errorf("repository.path is required")
	}
	if c.Repository.Password == "" && c.Repository.PasswordFile == "" && c.Repository.PasswordCommand == "" {
		return fmt.Errorf("repository.password, repository.password_file, or repository.password_command is required")
	}
	if len(c.Backup.Paths) == 0 {
		return fmt.Errorf("backup.paths is required")
	}
	if _, _, err := c.Schedule.ParseSchedule(); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Update the config template**

Replace the entire contents of `internal/config/template.go` with:

```go
package config

import (
	"fmt"
	"os"
)

// DefaultConfigTemplate is the template for a new config file with helpful comments.
const DefaultConfigTemplate = `# NeubiBackup Configuration
# Documentation: https://github.com/ianneub/neubibackup
version: 2

# Log verbosity: debug, info, warn, error (default: info)
# log_level: "info"

# Schedule settings
schedule:
  cron: "@every 24h"         # Cron expression or "@every <duration>". Minimum gap: 15m.
                             # Examples:
                             #   "@every 1h"      — every hour, rolling from last success
                             #   "@every 6h"      — every 6 hours
                             #   "0 1 * * *"      — every day at 01:00 (cron)
                             #   "*/30 * * * *"   — every 30 minutes on the half-hour
                             #   "0 8,18 * * *"   — daily at 08:00 and 18:00
  # timezone: ""             # Optional, defaults to system timezone (affects cron expressions)
  # skip_on_battery: false   # Skip scheduled backups when on battery power (manual backups always run)
  # allowed_ssids: []        # Only run scheduled backups on these WiFi SSIDs (empty = no restriction)
  #   - "HomeWiFi"           # Example: backup on home network
  #   - "OfficeNetwork"      # Example: also backup on office network

# Restic repository settings
repository:
  # REST server example:
  path: "rest:https://user:pass@backup.example.com/repo"

  # Local repository example:
  # path: "/path/to/backup/repo"

  # Password (enter directly - note: less secure than other options):
  # password: "your-restic-password"

  # Or use a password file:
  # password_file: "/path/to/password-file"

  # Or use a command to get the password (most secure):
  # macOS Keychain example:
  # password_command: "security find-generic-password -s neubibackup -w"
  # Windows Credential Manager example:
  # password_command: "powershell -Command \"(Get-StoredCredential -Target neubibackup).GetNetworkCredential().Password\""

# What to backup
backup:
  paths:
    - ""  # Add paths to backup
    # macOS examples:
    #   - "/Users/username/Documents"
    #   - "/Users/username/Pictures"
    # Windows examples (use forward slashes OR single quotes with backslashes):
    #   - "C:/Users/username/Documents"
    #   - 'C:\Users\username\Pictures'
  excludes:
    - "*.tmp"
    - ".DS_Store"
    - "node_modules"
    - "__pycache__"
  exclude_file: ""           # Optional path to exclude patterns file

# Additional restic arguments
restic_args:
  global: []                 # Args for all commands
  backup:                    # Args for backup command
    - "--verbose"
  # Note: The following flags are always added automatically:
  #   --one-file-system    (don't cross filesystem boundaries)
  #   --exclude-caches     (skip directories with CACHEDIR.TAG)
  #   --use-fs-snapshot    (Windows only: use VSS for consistent snapshots)

# Healthchecks.io integration (optional)
healthchecks:
  enabled: false
  ping_url: "https://hc-ping.com/your-uuid-here"
  send_logs_on_failure: true

# Pushover notifications (optional)
pushover:
  enabled: false
  user_key: "your-pushover-user-key"
  api_token: "your-pushover-api-token"
  on_success: false          # Notify on successful backup
  on_failure: true           # Notify on failed backup

# Tailscale integration (optional)
# Enable this to access restic REST servers that are only reachable via Tailscale.
# The device stays registered in your tailnet - auth key is only needed for initial setup.
tailscale:
  enabled: false
  # Auth key for headless login (get from https://login.tailscale.com/admin/settings/keys)
  # Use a reusable key for long-term operation.
  auth_key: ""
  # Hostname for this device in your tailnet (defaults to "neubibackup")
  hostname: "neubibackup"

# Note: state.yaml and logs/ are stored in ~/neubibackup/ alongside this config
`

// WriteDefaultConfig writes the default config template to the config file.
func WriteDefaultConfig() error {
	if err := EnsureAppDir(); err != nil {
		return fmt.Errorf("creating app directory: %w", err)
	}

	configPath, err := GetConfigPath()
	if err != nil {
		return fmt.Errorf("getting config path: %w", err)
	}

	if err := os.WriteFile(configPath, []byte(DefaultConfigTemplate), 0600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}
```

- [ ] **Step 5: Update existing config_test.go fixtures to v2 + Cron**

Open `internal/config/config_test.go`. Existing fixtures use `Time: "01:00"` and `Version: 0`. Replace each fixture so it sets `Version: 2` and uses `Cron` instead of `Time`. Specifically, find every literal that looks like:

```go
cfg: Config{
    Repository: ...,
    Backup: ...,
    Schedule: ScheduleConfig{
        Time: "01:00",
    },
},
```

…and rewrite it to:

```go
cfg: Config{
    Version: 2,
    Repository: ...,
    Backup: ...,
    Schedule: ScheduleConfig{
        Cron: "@every 24h",
    },
},
```

There's also a test named `"missing schedule time"` that asserts the error message contains `"schedule.time is required"`. **Delete that test case entirely** — that error no longer exists.

If a test asserts on `"version"` mismatch error text, leave it; otherwise add the new test cases below.

- [ ] **Step 6: Add v2 migration tests**

Append to `internal/config/config_test.go`:

```go
func TestValidate_VersionV1Rejected(t *testing.T) {
	cfg := Config{
		Version: 1,
		Repository: RepositoryConfig{
			Path:     "/backup/repo",
			Password: "secret",
		},
		Backup: BackupConfig{
			Paths: []string{"/home"},
		},
		Schedule: ScheduleConfig{
			Cron: "@every 24h",
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected v1 rejection, got nil")
	}
	for _, want := range []string{"schema version", "schedule.cron", "version: 2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() err = %q, want substring %q", err.Error(), want)
		}
	}
}

func TestValidate_VersionZeroRejected(t *testing.T) {
	cfg := Config{
		Repository: RepositoryConfig{
			Path:     "/backup/repo",
			Password: "secret",
		},
		Backup: BackupConfig{
			Paths: []string{"/home"},
		},
		Schedule: ScheduleConfig{
			Cron: "@every 24h",
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() expected version-0 rejection, got nil")
	}
}

func TestValidate_AcceptsV2WithEmptyCron(t *testing.T) {
	cfg := Config{
		Version: 2,
		Repository: RepositoryConfig{
			Path:     "/backup/repo",
			Password: "secret",
		},
		Backup: BackupConfig{
			Paths: []string{"/home"},
		},
		Schedule: ScheduleConfig{Cron: ""},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() with empty Cron err = %v, want nil", err)
	}
}

func TestValidate_RejectsBadCron(t *testing.T) {
	cfg := Config{
		Version: 2,
		Repository: RepositoryConfig{
			Path:     "/backup/repo",
			Password: "secret",
		},
		Backup: BackupConfig{
			Paths: []string{"/home"},
		},
		Schedule: ScheduleConfig{Cron: "*/5 * * * *"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected sub-15m cron rejection, got nil")
	}
	if !strings.Contains(err.Error(), "fires too frequently") {
		t.Errorf("Validate() err = %q, want substring %q", err.Error(), "fires too frequently")
	}
}

func TestLoadFromFile_RejectsStrayTimeField(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	body := []byte(`version: 2
schedule:
  cron: "@every 24h"
  time: "01:00"
repository:
  path: /repo
  password: secret
backup:
  paths: ["/home"]
`)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFromFile(path); err == nil {
		t.Fatal("LoadFromFile() expected strict-decoding error for stray time:, got nil")
	} else if !strings.Contains(err.Error(), "time") {
		t.Errorf("LoadFromFile() err = %q, want to mention 'time'", err.Error())
	}
}

func TestLoadFromFile_RejectsV1ConfigOnValidate(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	body := []byte(`version: 1
schedule:
  cron: "@every 24h"
repository:
  path: /repo
  password: secret
backup:
  paths: ["/home"]
`)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile() err = %v, want clean parse", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() of v1 config expected error, got nil")
	} else if !strings.Contains(err.Error(), "schema version") {
		t.Errorf("Validate() err = %q, want substring 'schema version'", err.Error())
	}
}
```

The `filepath` import is likely already present from existing tests; if not, add it.

- [ ] **Step 7: Run the config tests**

Run:

```bash
go test ./internal/config/... -v
```

Expected: all PASS.

- [ ] **Step 8: Run the full test suite**

Run:

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/config/template.go
git commit -m "feat(config)!: bump schema to v2 and drop schedule.time

BREAKING: config schema is now version 2. The schedule.time field is
removed from the struct. LoadFromFile now uses strict YAML decoding,
so a leftover schedule.time: line errors out by name. Validate
requires version: 2 and validates the schedule.cron expression.

Migration: see README for the v1 -> v2 instructions."
```

---

## Task 5: Logging retention scales with schedule

Change `CleanupOldLogs` to take a `maxFiles int`, add a `RetentionFor(time.Duration) int` helper, and plumb the value through the orchestrator's options API. The app passes `RetentionFor(scheduler.MinGap())`.

**Files:**
- Modify: `internal/logging/logging.go`
- Modify: `internal/logging/logging_test.go`
- Modify: `internal/backup/orchestrator.go`
- Modify: `internal/app/app.go`

- [ ] **Step 1: Update `logging.go`**

Replace the entire contents of `internal/logging/logging.go` with:

```go
// Package logging manages backup log files with automatic cleanup.
package logging

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"neubibackup/internal/config"
)

const (
	// DefaultMaxLogFiles is used when the caller doesn't compute a per-schedule
	// retention. Roughly one daily backup for ~25 days.
	DefaultMaxLogFiles = 25

	// MaxLogFilesCap is the hard ceiling on retained logs.
	MaxLogFilesCap = 500

	logTimeFormat = "2006-01-02T15-04-05"
	logFileSuffix = ".log"
)

// CreateLogFile creates a new log file with the current timestamp.
// Returns the opened file which the caller must close when done.
func CreateLogFile() (*os.File, error) {
	logsDir, err := config.GetLogsDir()
	if err != nil {
		return nil, fmt.Errorf("get logs dir: %w", err)
	}

	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}

	filename := time.Now().Format(logTimeFormat) + logFileSuffix
	logPath := filepath.Join(logsDir, filename)

	file, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}

	return file, nil
}

// CleanupOldLogs removes all but the most recent maxFiles log files.
// If maxFiles <= 0, it falls back to DefaultMaxLogFiles.
func CleanupOldLogs(maxFiles int) error {
	if maxFiles <= 0 {
		maxFiles = DefaultMaxLogFiles
	}
	logsDir, err := config.GetLogsDir()
	if err != nil {
		return fmt.Errorf("get logs dir: %w", err)
	}

	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read logs dir: %w", err)
	}

	var logFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), logFileSuffix) {
			logFiles = append(logFiles, entry.Name())
		}
	}

	if len(logFiles) <= maxFiles {
		return nil
	}

	sort.Strings(logFiles)

	toDelete := len(logFiles) - maxFiles
	for i := 0; i < toDelete; i++ {
		logPath := filepath.Join(logsDir, logFiles[i])
		if err := os.Remove(logPath); err != nil {
			return fmt.Errorf("remove old log %s: %w", logFiles[i], err)
		}
	}

	return nil
}

// RetentionFor computes the number of log files to retain so that frequent
// backups (e.g. hourly) keep ~7 days of logs while daily backups keep the
// historical default of 25. Capped at MaxLogFilesCap.
func RetentionFor(minGap time.Duration) int {
	if minGap <= 0 {
		return DefaultMaxLogFiles
	}
	week := (7 * 24 * time.Hour).Seconds()
	want := int(math.Ceil(week / minGap.Seconds()))
	if want < DefaultMaxLogFiles {
		want = DefaultMaxLogFiles
	}
	if want > MaxLogFilesCap {
		want = MaxLogFilesCap
	}
	return want
}

// GetLogPath returns the full path for a log file given its filename.
func GetLogPath(filename string) (string, error) {
	logsDir, err := config.GetLogsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(logsDir, filename), nil
}
```

- [ ] **Step 2: Update `logging_test.go`**

The existing tests use a private helper `cleanupOldLogsInDir` that hardcodes `maxLogFiles`. Replace the entire contents of `internal/logging/logging_test.go` with:

```go
package logging

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestCleanupOldLogs(t *testing.T) {
	tmpDir := t.TempDir()
	logsDir := filepath.Join(tmpDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("Failed to create logs dir: %v", err)
	}

	createLogFile := func(name string) {
		path := filepath.Join(logsDir, name)
		if err := os.WriteFile(path, []byte("log content"), 0600); err != nil {
			t.Fatalf("Failed to create log file %s: %v", name, err)
		}
	}

	countLogFiles := func() int {
		entries, _ := os.ReadDir(logsDir)
		count := 0
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".log") {
				count++
			}
		}
		return count
	}

	t.Run("fewer than max - no cleanup", func(t *testing.T) {
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)
		for i := 0; i < 5; i++ {
			createLogFile(time.Now().Add(time.Duration(-i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}
		if err := cleanupOldLogsInDir(logsDir, 25); err != nil {
			t.Fatalf("err = %v", err)
		}
		if got := countLogFiles(); got != 5 {
			t.Errorf("count = %d, want 5", got)
		}
	})

	t.Run("more than max - removes oldest", func(t *testing.T) {
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 30; i++ {
			createLogFile(baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}
		if err := cleanupOldLogsInDir(logsDir, 25); err != nil {
			t.Fatalf("err = %v", err)
		}
		if got := countLogFiles(); got != 25 {
			t.Errorf("count = %d, want 25", got)
		}
		for i := 0; i < 5; i++ {
			oldFile := baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log"
			if _, err := os.Stat(filepath.Join(logsDir, oldFile)); !os.IsNotExist(err) {
				t.Errorf("old file %s should have been removed", oldFile)
			}
		}
	})

	t.Run("non-log files are ignored", func(t *testing.T) {
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 30; i++ {
			createLogFile(baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}
		os.WriteFile(filepath.Join(logsDir, "readme.txt"), []byte("readme"), 0600)

		if err := cleanupOldLogsInDir(logsDir, 25); err != nil {
			t.Fatalf("err = %v", err)
		}
		if _, err := os.Stat(filepath.Join(logsDir, "readme.txt")); err != nil {
			t.Error("readme.txt should not be removed")
		}
	})

	t.Run("non-existent dir returns nil", func(t *testing.T) {
		if err := cleanupOldLogsInDir(filepath.Join(tmpDir, "nonexistent"), 25); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("zero falls back to default", func(t *testing.T) {
		os.RemoveAll(logsDir)
		os.MkdirAll(logsDir, 0755)
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 30; i++ {
			createLogFile(baseTime.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15-04-05") + ".log")
		}
		if err := cleanupOldLogsInDir(logsDir, 0); err != nil {
			t.Fatalf("err = %v", err)
		}
		if got := countLogFiles(); got != DefaultMaxLogFiles {
			t.Errorf("zero-arg count = %d, want %d", got, DefaultMaxLogFiles)
		}
	})
}

func TestRetentionFor(t *testing.T) {
	cases := []struct {
		gap  time.Duration
		want int
	}{
		{0, DefaultMaxLogFiles},
		{24 * time.Hour, 25},                  // ceil(168/24) = 7, max with 25 = 25
		{12 * time.Hour, 25},                  // ceil(168/12) = 14, max with 25 = 25
		{1 * time.Hour, 168},                  // ceil(168/1) = 168
		{30 * time.Minute, 336},               // ceil(168/0.5) = 336
		{15 * time.Minute, MaxLogFilesCap},    // ceil(168*4) = 672, capped at 500
		{1 * time.Minute, MaxLogFilesCap},     // pathological, capped
	}
	for _, tc := range cases {
		t.Run(tc.gap.String(), func(t *testing.T) {
			if got := RetentionFor(tc.gap); got != tc.want {
				t.Errorf("RetentionFor(%s) = %d, want %d", tc.gap, got, tc.want)
			}
		})
	}
}

func TestGetLogPath(t *testing.T) {
	logPath, err := GetLogPath("2024-01-15T10-30-00.log")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasSuffix(logPath, "2024-01-15T10-30-00.log") {
		t.Errorf("path = %q, want suffix %q", logPath, "2024-01-15T10-30-00.log")
	}
	if !strings.Contains(logPath, "logs") {
		t.Errorf("path = %q, want to contain 'logs'", logPath)
	}
	if !filepath.IsAbs(logPath) {
		t.Errorf("path = %q, want absolute", logPath)
	}
}

// cleanupOldLogsInDir is a testable copy of CleanupOldLogs that operates on an
// arbitrary directory rather than the global logs dir.
func cleanupOldLogsInDir(logsDir string, maxFiles int) error {
	if maxFiles <= 0 {
		maxFiles = DefaultMaxLogFiles
	}
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var logFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), logFileSuffix) {
			logFiles = append(logFiles, entry.Name())
		}
	}

	if len(logFiles) <= maxFiles {
		return nil
	}
	sort.Strings(logFiles)

	toDelete := len(logFiles) - maxFiles
	for i := 0; i < toDelete; i++ {
		if err := os.Remove(filepath.Join(logsDir, logFiles[i])); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 3: Run logging tests**

Run:

```bash
go test ./internal/logging/... -v
```

Expected: all PASS, including the new `TestRetentionFor`.

- [ ] **Step 4: Add `WithLogRetention` option to the orchestrator**

In `internal/backup/orchestrator.go`, add a field and an option. After the existing `location *time.Location` field in the `Orchestrator` struct, add:

```go
	logRetention int
```

After the existing `WithLocation` option, add:

```go
// WithLogRetention sets the maximum number of log files to retain after a
// successful backup. Zero or negative falls back to logging.DefaultMaxLogFiles.
func WithLogRetention(maxFiles int) OrchestratorOption {
	return func(o *Orchestrator) {
		o.logRetention = maxFiles
	}
}
```

Then update the `CleanupOldLogs` call site (around line 192) from:

```go
if err := logging.CleanupOldLogs(); err != nil {
```

…to:

```go
if err := logging.CleanupOldLogs(o.logRetention); err != nil {
```

- [ ] **Step 5: Wire the app to pass `WithLogRetention`**

In `internal/app/app.go`, find the `backup.NewOrchestrator(...)` call (around line 445) and add a `WithLogRetention` option computed from the scheduler. Replace this block:

```go
	// Create and run orchestrator
	orchestrator := backup.NewOrchestrator(a.cfg, a.state,
		backup.WithNotifier(notifier),
		backup.WithTailscale(tailscaleProvider),
		backup.WithProgressCallback(onProgress),
		backup.WithLocation(loc),
	)
```

with:

```go
	// Create and run orchestrator
	logRetention := logging.DefaultMaxLogFiles
	if a.sched != nil {
		logRetention = logging.RetentionFor(a.sched.MinGap())
	}
	orchestrator := backup.NewOrchestrator(a.cfg, a.state,
		backup.WithNotifier(notifier),
		backup.WithTailscale(tailscaleProvider),
		backup.WithProgressCallback(onProgress),
		backup.WithLocation(loc),
		backup.WithLogRetention(logRetention),
	)
```

- [ ] **Step 6: Build and run all tests**

Run:

```bash
go build ./...
go test ./...
```

Expected: clean build, all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/logging/logging.go internal/logging/logging_test.go internal/backup/orchestrator.go internal/app/app.go
git commit -m "feat(logging): scale log retention with schedule cadence

CleanupOldLogs now takes a maxFiles int. Adds RetentionFor that maps
the scheduler's minimum gap to a retention count (25 for daily, 168
for hourly, capped at 500). The orchestrator gets WithLogRetention,
and the app passes RetentionFor(scheduler.MinGap()) on every backup."
```

---

## Task 6: Status formatter handles tomorrow / weekday

Refactor `FormatNextBackup` to take an explicit `now` (via an internal helper) so the new "tomorrow at 3:04 PM" and "on Mon Jan 2 at 3:04 PM" branches are testable deterministically.

**Files:**
- Modify: `internal/tray/status.go`
- Modify: `internal/tray/status_test.go`

- [ ] **Step 1: Refactor `FormatNextBackup`**

In `internal/tray/status.go`, replace the existing `FormatNextBackup` (around line 83):

```go
// FormatNextBackup returns a human-readable string for the next backup time.
func FormatNextBackup(nextTime time.Time) string {
	return formatNextBackupAt(nextTime, time.Now())
}

func formatNextBackupAt(nextTime, now time.Time) string {
	if !nextTime.After(now) {
		return "Backup due"
	}

	until := nextTime.Sub(now)

	if until < time.Hour {
		mins := int(until.Minutes())
		if mins <= 1 {
			return "in 1 minute"
		}
		return fmt.Sprintf("in %d minutes", mins)
	}

	if until < 24*time.Hour {
		hours := int(until.Hours())
		if hours == 1 {
			return "in 1 hour"
		}
		return fmt.Sprintf("in %d hours", hours)
	}

	// Far future: distinguish "tomorrow" from "later this week or beyond".
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	nextDay := time.Date(nextTime.Year(), nextTime.Month(), nextTime.Day(), 0, 0, 0, 0, nextTime.Location())
	dayDelta := int(nextDay.Sub(nowDay).Hours() / 24)

	if dayDelta == 1 {
		return fmt.Sprintf("tomorrow at %s", nextTime.Format("3:04 PM"))
	}

	return fmt.Sprintf("on %s at %s", nextTime.Format("Mon Jan 2"), nextTime.Format("3:04 PM"))
}
```

- [ ] **Step 2: Update existing tests + add deterministic ones**

In `internal/tray/status_test.go`, replace the existing `TestFormatNextBackup` body and add new sub-tests. Replace:

```go
func TestFormatNextBackup(t *testing.T) {
	tests := []struct {
		name     string
		offset   time.Duration
		want     string
	}{
		{
			name:   "past due",
			offset: -1 * time.Hour,
			want:   "Backup due",
		},
		{
			name:   "in 1 minute",
			offset: 30 * time.Second,
			want:   "in 1 minute",
		},
		{
			name:   "in 5 minutes",
			offset: 5*time.Minute + 30*time.Second, // add buffer for test execution time
			want:   "in 5 minutes",
		},
		{
			name:   "in 1 hour",
			offset: 1*time.Hour + 30*time.Second,
			want:   "in 1 hour",
		},
		{
			name:   "in 3 hours",
			offset: 3*time.Hour + 30*time.Second,
			want:   "in 3 hours",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate nextTime fresh for each test to avoid timing issues
			nextTime := time.Now().Add(tt.offset)
			got := FormatNextBackup(nextTime)
			if got != tt.want {
				t.Errorf("FormatNextBackup() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

…with:

```go
func TestFormatNextBackup(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) // Saturday noon

	tests := []struct {
		name string
		next time.Time
		want string
	}{
		{"past due", now.Add(-1 * time.Hour), "Backup due"},
		{"now (boundary)", now, "Backup due"},
		{"in 1 minute", now.Add(30 * time.Second), "in 1 minute"},
		{"in 5 minutes", now.Add(5 * time.Minute), "in 5 minutes"},
		{"in 1 hour", now.Add(time.Hour + time.Second), "in 1 hour"},
		{"in 3 hours", now.Add(3*time.Hour + time.Second), "in 3 hours"},
		{"tomorrow at 3 PM", time.Date(2026, 5, 10, 15, 4, 0, 0, time.UTC), "tomorrow at 3:04 PM"},
		{"3 days out", time.Date(2026, 5, 12, 15, 4, 0, 0, time.UTC), "on Tue May 12 at 3:04 PM"},
		{"a week out", time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC), "on Sat May 16 at 9:30 AM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatNextBackupAt(tt.next, now); got != tt.want {
				t.Errorf("formatNextBackupAt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatNextBackup_PublicWrapper(t *testing.T) {
	// Smoke check that the public wrapper works against time.Now() without panic.
	got := FormatNextBackup(time.Now().Add(-time.Hour))
	if got != "Backup due" {
		t.Errorf("FormatNextBackup(past) = %q, want %q", got, "Backup due")
	}
}
```

- [ ] **Step 3: Run status tests**

Run:

```bash
go test ./internal/tray/... -run 'TestFormatNextBackup' -v
```

Expected: all PASS.

- [ ] **Step 4: Run all tests**

Run:

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tray/status.go internal/tray/status_test.go
git commit -m "feat(tray): handle tomorrow/weekday in FormatNextBackup

Extracts an internal formatNextBackupAt(next, now) so deterministic
tests can pin 'now'. Adds two branches for next-fire times beyond
24h: 'tomorrow at 3:04 PM' and 'on Mon Jan 2 at 3:04 PM'."
```

---

## Task 7: Menu shows next backup line

Add a second disabled menu item directly under `mStatus` that displays the next scheduled backup. The decision logic is extracted into a pure helper `nextBackupMenuText` so it can be tested without instantiating systray.

**Files:**
- Modify: `internal/tray/menu.go`
- Modify: `internal/tray/menu_test.go`

- [ ] **Step 1: Add `ScheduleProvider`, the helper, and `mNextBackup`**

In `internal/tray/menu.go`, add a new interface near the top of the file (just after `AutostartProvider`):

```go
// ScheduleProvider provides the next scheduled backup time.
type ScheduleProvider interface {
	NextBackupTime() (time.Time, error)
}
```

Add the import for `time` in this file's import block (it likely already imports `time` through transitive deps; if not, add `"time"` to the import list).

Add a `Schedule` field to `MenuConfig`. After the existing `Autostart AutostartProvider` line:

```go
	// Schedule is a getter (not a value) so the menu picks up the scheduler
	// after a late initScheduler call (e.g. unconfigured -> configured via
	// config reload). Returning nil means "no scheduler available, hide the
	// next-backup line".
	Schedule func() ScheduleProvider
```

This matches the pattern used by `AppState func() *state.State` so the menu always sees the latest value.

Add `mNextBackup` to the `Menu` struct, immediately after `mStatus`:

```go
	mStatus       *systray.MenuItem
	mNextBackup   *systray.MenuItem
```

Add a pure helper near the other helpers (just before `UpdateStatus`):

```go
// nextBackupMenuText decides what the next-backup menu line should display.
// Returns ("", false) when the line should be hidden.
func nextBackupMenuText(isConfigured, isRunning bool, scheduleFn func() ScheduleProvider) (string, bool) {
	if scheduleFn == nil || !isConfigured || isRunning {
		return "", false
	}
	sched := scheduleFn()
	if sched == nil {
		return "", false
	}
	next, err := sched.NextBackupTime()
	if err != nil {
		return "", false
	}
	return "Next backup: " + FormatNextBackup(next), true
}
```

- [ ] **Step 2: Wire `mNextBackup` into `setup()`**

In `setup()`, between the existing `m.mStatus.Disable()` line and the `systray.AddSeparator()` that follows, insert:

```go
	m.mNextBackup = systray.AddMenuItem("", "Next scheduled backup")
	m.mNextBackup.Disable()
	if !isConfigured {
		m.mNextBackup.Hide()
	}
```

- [ ] **Step 3: Update `UpdateStatus()` to refresh `mNextBackup`**

In `UpdateStatus()`, append after the existing `m.mStatus.SetTitle(title)` line:

```go
	if m.mNextBackup == nil {
		return
	}
	text, show := nextBackupMenuText(m.cfg.IsConfigured(), m.cfg.BackupState.IsRunning(), m.cfg.Schedule)
	if show {
		m.mNextBackup.SetTitle(text)
		m.mNextBackup.Show()
	} else {
		m.mNextBackup.Hide()
	}
```

- [ ] **Step 4: Tests for `nextBackupMenuText`**

Append to `internal/tray/menu_test.go`:

```go
type mockScheduleProvider struct {
	next time.Time
	err  error
}

func (m *mockScheduleProvider) NextBackupTime() (time.Time, error) {
	return m.next, m.err
}

func fnReturning(p ScheduleProvider) func() ScheduleProvider {
	return func() ScheduleProvider { return p }
}

func TestNextBackupMenuText(t *testing.T) {
	future := time.Now().Add(2 * time.Hour)
	provider := &mockScheduleProvider{next: future}

	tests := []struct {
		name         string
		isConfigured bool
		isRunning    bool
		scheduleFn   func() ScheduleProvider
		wantShow     bool
		wantPrefix   string
	}{
		{"shown when idle and configured", true, false, fnReturning(provider), true, "Next backup: "},
		{"hidden while running", true, true, fnReturning(provider), false, ""},
		{"hidden when not configured", false, false, fnReturning(provider), false, ""},
		{"hidden when getter is nil", true, false, nil, false, ""},
		{"hidden when getter returns nil", true, false, fnReturning(nil), false, ""},
		{"hidden when provider errors", true, false, fnReturning(&mockScheduleProvider{err: errors.New("boom")}), false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, show := nextBackupMenuText(tt.isConfigured, tt.isRunning, tt.scheduleFn)
			if show != tt.wantShow {
				t.Errorf("show = %v, want %v", show, tt.wantShow)
			}
			if tt.wantShow && !strings.HasPrefix(text, tt.wantPrefix) {
				t.Errorf("text = %q, want prefix %q", text, tt.wantPrefix)
			}
			if !tt.wantShow && text != "" {
				t.Errorf("text = %q, want empty", text)
			}
		})
	}
}
```

The `errors` import already exists in the test file. Add `"strings"` and `"time"` to the import block if either is missing.

- [ ] **Step 5: Run menu tests**

Run:

```bash
go test ./internal/tray/... -v
```

Expected: all PASS, including `TestNextBackupMenuText`.

- [ ] **Step 6: Run all tests**

Run:

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tray/menu.go internal/tray/menu_test.go
git commit -m "feat(tray): add 'Next backup' menu item under status line

Adds a disabled menu item that surfaces NextBackupTime via the new
ScheduleProvider interface on MenuConfig. The decision logic is
extracted into a pure helper so it's unit-testable without systray."
```

---

## Task 8: App passes scheduler as MenuConfig.Schedule

Plumb the scheduler instance into the menu so the new menu item gets a real provider.

**Files:**
- Modify: `internal/app/app.go`

- [ ] **Step 1: Update `MenuConfig` construction (use a getter)**

In `internal/app/app.go`, find the `tray.MenuConfig{...}` literal (around line 172) and add a `Schedule` getter that converts a possibly-nil `*scheduler.Scheduler` into a true nil `tray.ScheduleProvider`:

```go
	a.menu = tray.NewMenu(tray.MenuConfig{
		Version:       a.version,
		ResticVersion: restic.Version,
		AppState:      func() *state.State { return a.state },
		BackupState:   a.backupState,
		UpdateState:   a.updateState,
		IsConfigured:  func() bool { return a.cfg != nil && a.cfg.IsConfigured() },
		Autostart:     a.autostartMgr,
		Schedule: func() tray.ScheduleProvider {
			if a.sched == nil {
				return nil
			}
			return a.sched
		},
		OnBackupNow:    a.TriggerBackup,
		OnStopBackup:   a.StopBackup,
		OnOpenConfig:   a.openConfig,
		OnOpenLogs:     a.openLogs,
		OnOpenAppLog:   a.openAppLog,
		OnUpdateClick:  a.handleUpdateClick,
		OnVersionClick: a.openProjectWebsite,
		OnQuit:         a.onQuit,
	})
```

Why a getter: the menu may be built before the scheduler exists (the unconfigured-startup case) and the scheduler may also be (re)created later when the user saves a config and the watcher reloads. A captured-by-value `Schedule: a.sched` would freeze whatever `a.sched` was at construction time and never update. Using a `func() ScheduleProvider` makes the menu re-read `a.sched` on every `UpdateStatus` tick.

The explicit `if a.sched == nil { return nil }` step is also load-bearing: returning `a.sched` directly when it's a typed-nil pointer would yield a non-nil-but-dispatchable interface that panics in `Scheduler.NextBackupTime` (which dereferences `s.mu`). The conditional collapses the typed-nil into a true nil interface so `nextBackupMenuText`'s nil-guard fires.

Even with the getter, calling `UpdateStatus` immediately after `initScheduler` is still useful so the line refreshes without waiting for the next 1-minute tick. Add that call at the end of `initScheduler` (around line 355):

```go
func (a *App) initScheduler() {
	var err error
	a.sched, err = scheduler.New(a.cfg, a.state, a.runBackup)
	if err != nil {
		slog.Error("Error creating scheduler", "error", err)
		return
	}

	go a.sched.Start(a.ctx)
	slog.Info("Scheduler started")

	if a.menu != nil {
		a.menu.UpdateStatus()
	}
}
```

This change is small and self-contained.

- [ ] **Step 2: Build and run all tests**

Run:

```bash
go build ./...
go test ./...
```

Expected: clean build, all PASS.

Walking through what happens at runtime to confirm the wiring is correct:

- **Configured app at startup:** `Initialize()` builds the menu (`mNextBackup` is created hidden). `Run()` calls `initScheduler()`, which sets `a.sched`, then calls `a.menu.UpdateStatus()`. `UpdateStatus` invokes the getter, sees the now non-nil `a.sched`, and shows the line.
- **Unconfigured app at startup:** `Initialize()` builds the menu. `Run()` skips `initScheduler` (no config). Line stays hidden. When the user saves a config, the watcher reloads at line 542, calls `initScheduler` (which now refreshes the menu), and the line appears.
- **Backup running:** `runBackup` calls `a.updateStatus()`, which calls `Menu.UpdateStatus`, which the helper hides while `BackupState.IsRunning()` is true. When the backup finishes, `a.updateStatus()` is called again (line 432), and the line reappears.

- [ ] **Step 3: Commit**

```bash
git add internal/app/app.go
git commit -m "feat(app): wire scheduler into MenuConfig.Schedule

Menu reads the scheduler through a getter so the line tracks the
scheduler across config reloads. The getter explicitly collapses a
nil *scheduler.Scheduler into a true nil interface to avoid the
typed-nil dispatch trap."
```

---

## Task 9: README and CHANGELOG

Document the breaking change so users can self-serve the migration.

**Files:**
- Modify: `README.md`
- Create: `CHANGELOG.md`

- [ ] **Step 1: Update `README.md`**

Replace the existing schedule example block (around lines 60–75) and the related Features bullet:

In the **Features** list (around line 9), replace:

```markdown
- **Daily scheduled backups** - Set a time and your files are backed up automatically
```

with:

```markdown
- **Configurable schedule** - Cron expression or `@every <duration>` (hourly, daily, custom cadence)
```

In the **Configuration** YAML block (around line 60), replace:

```yaml
version: 1

# Log verbosity: debug, info, warn, error (default: info)
# log_level: "info"

# When to run daily backups (24-hour format)
schedule:
  time: "02:00"
  timezone: "America/New_York"  # Optional, defaults to system timezone
  skip_on_battery: false        # Optional, skip scheduled backups when on battery power
  # allowed_ssids:              # Optional, only backup on these WiFi SSIDs
  #   - "HomeWiFi"
  #   - "OfficeNetwork"
```

with:

```yaml
version: 2

# Log verbosity: debug, info, warn, error (default: info)
# log_level: "info"

# Backup schedule (cron expression or "@every <duration>"). Minimum gap: 15m.
# Examples:
#   "@every 1h"      — every hour, rolling from last success
#   "@every 6h"      — every 6 hours
#   "0 1 * * *"      — every day at 01:00
#   "*/30 * * * *"   — every 30 minutes
#   "0 8,18 * * *"   — daily at 08:00 and 18:00
schedule:
  cron: "@every 24h"
  timezone: "America/New_York"  # Optional, defaults to system timezone (affects cron expressions)
  skip_on_battery: false        # Optional, skip scheduled backups when on battery power
  # allowed_ssids:              # Optional, only backup on these WiFi SSIDs
  #   - "HomeWiFi"
  #   - "OfficeNetwork"
```

Then add a new top-level subsection below the schedule example. After the configuration code block ends and before the next section, insert:

```markdown
### Migrating from v1 to v2

NeubiBackup v2 replaces the `schedule.time` field with the more flexible `schedule.cron`. If you upgrade from v1 you must update your `config.yaml` once:

1. Set `version: 2` at the top of the file.
2. Remove the `schedule.time: "HH:MM"` line.
3. Add a `schedule.cron: "<expression>"` line. To preserve your previous behavior:

   | Old                         | New                          |
   |-----------------------------|------------------------------|
   | `time: "01:00"`             | `cron: "0 1 * * *"`          |
   | `time: "02:00"`             | `cron: "0 2 * * *"`          |
   | (no preference on wall time) | `cron: "@every 24h"`         |

The app refuses to start until you complete this migration; the validation error in `app.log` will tell you exactly what's wrong.

### Backup frequency

Two syntaxes are supported by `schedule.cron`:

- **`@every <Go duration>`** — rolling cadence anchored to the last successful backup. Examples: `"@every 30m"`, `"@every 1h"`, `"@every 6h"`. Best when you don't care about the wall-clock time.
- **5-field cron expressions** — calendar-anchored fires. Examples: `"0 1 * * *"` (daily at 01:00), `"*/30 * * * *"` (every 30 minutes on the half-hour), `"0 8,18 * * *"` (twice a day).

The minimum gap between consecutive fires is **15 minutes** (the scheduler tick rate). Configurations that fire more often are rejected at startup.

When you back up frequently (hourly or sub-hourly), keep these trade-offs in mind:

- **Healthchecks.io** pings fire on every backup — set the schedule grace there to match.
- **Pushover** with `on_success: true` produces one notification per run.
- **Logs** are retained automatically — daily backups keep 25, hourly keeps ~168, capped at 500.
```

- [ ] **Step 2: Create `CHANGELOG.md`**

Create `CHANGELOG.md` at the repo root with this content:

```markdown
# Changelog

All notable changes to NeubiBackup are documented here.

## v2.0.0

### Breaking changes

- **Config schema bumps to version 2.** Existing v1 configs are rejected at startup with a clear migration error.
- **`schedule.time` is removed.** Replace with `schedule.cron`, which accepts either a 5-field cron expression (e.g. `"0 1 * * *"`) or a Go-duration descriptor (e.g. `"@every 1h"`).
- **`config.yaml` is parsed strictly.** Unknown fields produce a YAML error naming the offending key. This is intentional, to catch leftover `schedule.time:` entries during the v1 → v2 migration.

### Migration

In `~/neubibackup/config.yaml`:

1. Set `version: 2` at the top.
2. Remove the `schedule.time:` line.
3. Add `schedule.cron: "<expression>"`. Quick mappings:
   - `time: "01:00"` → `cron: "0 1 * * *"`
   - `time: "02:00"` → `cron: "0 2 * * *"`
   - No preference → `cron: "@every 24h"`

See the README "Migrating from v1 to v2" section for details.

### New

- **Configurable schedule** via cron expressions or `@every` descriptors (hourly, every-6-hours, twice-daily, etc.). Minimum gap between fires is 15 minutes.
- **"Next backup: …"** menu item directly under "Last backup: …" showing when the next scheduled run will fire.
- **Log retention scales with cadence.** Daily backups keep 25 logs (unchanged), hourly keep ~168, capped at 500.

### Internal

- Scheduler now uses `github.com/robfig/cron/v3`. The daily-anchor code path is removed.
- `state.HasBackedUpToday` and `state.HasBackedUpTodayAfter` are removed (they had no remaining callers).
```

- [ ] **Step 3: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: document v1 -> v2 schedule migration

Replaces the daily-schedule docs with cron / @every docs, adds a
migration table for users coming from v1, and seeds a CHANGELOG
with the breaking-change notice."
```

---

## Task 10: Final verification

Build, run the full test suite, and do a manual smoke check on the dev build.

**Files:** none (verification only).

- [ ] **Step 1: Native build**

Run:

```bash
go build ./...
```

Expected: no output, exit 0.

The cross-platform sanity-checks from CLAUDE.md skip CGO-dependent packages (`internal/idle`, `internal/network`, `internal/tray`) and the `internal/updater` package (no `restart_linux.go`). Use them to confirm the touched non-CGO packages still compile cleanly under a different `GOOS`:

```bash
docker run --rm -v "$PWD:/workspace" -w /workspace \
  -e GOOS=darwin -e GOARCH=arm64 -e CGO_ENABLED=0 \
  golang:1.26.2 go build ./internal/config/... ./internal/scheduler/... ./internal/state/... ./internal/logging/... ./internal/backup/...
```

Expected: clean build. Anything CGO-bound is the responsibility of CI, not this checkpoint.

- [ ] **Step 2: Run the full test suite**

Run:

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 3: Run the test suite with the race detector (catches scheduler concurrency bugs)**

Run:

```bash
go test -race ./internal/scheduler/... ./internal/state/... ./internal/config/...
```

Expected: all PASS, no race warnings.

- [ ] **Step 4: Manual smoke (optional but recommended on macOS)**

Smoke checklist:

```bash
# Build a dev binary
go build -o /tmp/neubibackup-dev .

# Make sure no real config interferes; dev builds use .dev-data/
rm -rf .dev-data
mkdir -p .dev-data
cat > .dev-data/config.yaml <<'YAML'
version: 2
schedule:
  cron: "@every 1h"
repository:
  path: rest:https://example/repo
  password: dummy
backup:
  paths:
    - /tmp
YAML

# Run; the scheduler should accept the config and the tray should show
# "Next backup: ..." within ~1 minute (or immediately if you click around)
/tmp/neubibackup-dev
```

Things to verify by clicking the tray icon:
- Status line: "Not yet backed up" or "Last backup: …"
- Next line: "Next backup: …" (e.g. "Backup due" since LastSuccess is zero)
- Quit cleanly via the Quit menu

Stop the app, edit `.dev-data/config.yaml` and change `cron:` to `*/5 * * * *`. Restart and confirm the app refuses to start (a validation error appears in `.dev-data/app.log`).

Then test the v1-rejection path: change the config to `version: 1` and add back `schedule.time: "01:00"`. Restart and confirm the validation error mentions the schema version and points at the migration docs.

- [ ] **Step 5: Commit nothing**

This task only verifies prior commits; no new commit is needed unless the smoke uncovered a bug. If it did, file the fix as an additional task in the same plan and re-run from Task 6 onward as needed.

---

## Self-review notes

- **Dependency:** added in Task 1 (`go get`). All later tasks reference it without re-adding.
- **Type names used downstream:** `cron.Schedule`, `cron.ParseStandard`, `ScheduleProvider`, `nextBackupMenuText`, `formatNextBackupAt`, `RetentionFor`, `WithLogRetention`, `MinGap`. All defined in earlier tasks before being referenced.
- **Order constraints honored:**
  - Task 1 introduces helpers without behavior changes (compiles green).
  - Task 2 swaps the scheduler to the helpers; `state.HasBackedUpTodayAfter` becomes unused (compiles green, all tests still pass).
  - Task 3 removes the unused state helpers (compiles green).
  - Task 4 drops `Time` and bumps the schema (breaking change is contained to a single commit; tests updated atomically).
  - Task 5 changes `CleanupOldLogs` signature and updates both callers in the same commit (compiles green).
  - Task 6 refactors the formatter without changing the public signature.
  - Task 7 adds the menu item; Task 8 wires the scheduler into the menu *before* the menu is built so the interface value is non-nil.
  - Task 9 documents the migration.
  - Task 10 verifies.
- **Spec coverage check:** every "Files touched" entry from the spec maps to a task. README and CHANGELOG are in Task 9.
- **No placeholders:** every code block is complete; every command has expected output.
