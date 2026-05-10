package scheduler

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"neubibackup/internal/config"
	"neubibackup/internal/state"
)

func TestMain(m *testing.M) {
	// Redirect state.Save() (used by checkAndTrigger) to a temp directory so
	// scheduler tests never touch real user data.
	tmpDir, err := os.MkdirTemp("", "neubibackup-scheduler-test-*")
	if err != nil {
		os.Exit(1)
	}
	os.Setenv("NEUBIBACKUP_APP_DIR", filepath.Join(tmpDir, "data"))
	code := m.Run()
	os.RemoveAll(tmpDir)
	os.Exit(code)
}

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
		name    string
		spec    string
		lastAgo time.Duration
		wantDue bool
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

func TestNextBackupTime_TickAligned_FireBeforeNextTick(t *testing.T) {
	// @every 1h, last_success 55 min ago → cron fire 5 min from now. Next
	// scheduler tick is 11 min from now. Menu should show the tick (11 min),
	// not the cron fire (5 min), because that's when the backup actually runs.
	now := time.Now()
	st := &state.State{}
	st.Backup.LastSuccess = now.Add(-55 * time.Minute)

	s, err := New(newTestConfig("@every 1h"), st, func() {})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate Start() having set the tick clock 11 min from now.
	s.mu.Lock()
	s.nextTickAt = now.Add(11 * time.Minute)
	s.mu.Unlock()

	got, err := s.NextBackupTime()
	if err != nil {
		t.Fatal(err)
	}

	want := now.Add(11 * time.Minute)
	if delta := got.Sub(want); delta > time.Second || delta < -time.Second {
		t.Errorf("NextBackupTime() = %s, want ~%s (delta %s)", got, want, delta)
	}
}

func TestNextBackupTime_TickAligned_FireAfterNextTick(t *testing.T) {
	// @every 1h, last_success 4m30s ago → cron fire 55m30s from now.
	// Set nextTickAt to a non-aligned offset so the ceiling-divide logic is
	// actually exercised: gap doesn't divide evenly into tickInterval, so the
	// result must round up to the first tick strictly after the cron fire.
	now := time.Now()
	st := &state.State{}
	st.Backup.LastSuccess = now.Add(-4*time.Minute - 30*time.Second)

	s, err := New(newTestConfig("@every 1h"), st, func() {})
	if err != nil {
		t.Fatal(err)
	}

	// nextTickAt at +1 min. Cron fire at +55m30s. gap = 54m30s.
	// With tickInterval=1m: ticks = ceil(54.5) = 55. Result = +1m + 55m = +56m.
	s.mu.Lock()
	s.nextTickAt = now.Add(1 * time.Minute)
	s.mu.Unlock()

	got, err := s.NextBackupTime()
	if err != nil {
		t.Fatal(err)
	}

	want := now.Add(56 * time.Minute)
	if delta := got.Sub(want); delta > time.Second || delta < -time.Second {
		t.Errorf("NextBackupTime() = %s, want ~%s (delta %s)", got, want, delta)
	}
}

func TestNextBackupTime_TickAligned_OverdueReturnsNextTick(t *testing.T) {
	// Schedule fired 30 min ago; the actual run happens on the next tick,
	// not "now".
	now := time.Now()
	st := &state.State{}
	st.Backup.LastSuccess = now.Add(-90 * time.Minute) // 90m ago, @every 1h fires every hour

	s, err := New(newTestConfig("@every 1h"), st, func() {})
	if err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.nextTickAt = now.Add(7 * time.Minute)
	s.mu.Unlock()

	got, err := s.NextBackupTime()
	if err != nil {
		t.Fatal(err)
	}

	want := now.Add(7 * time.Minute)
	if delta := got.Sub(want); delta > time.Second || delta < -time.Second {
		t.Errorf("NextBackupTime() overdue = %s, want ~%s (delta %s)", got, want, delta)
	}
}

func TestIsBackupDue_PrefersScheduledFireOverSuccess(t *testing.T) {
	// LastScheduledFire is the authoritative anchor. LastSuccess is older here
	// (e.g. backup ran for several minutes), but the schedule should be
	// computed from when the schedule fired, not when the backup completed.
	now := time.Now()
	st := &state.State{}
	st.Backup.LastScheduledFire = now.Add(-30 * time.Minute) // 30m ago
	st.Backup.LastSuccess = now.Add(-25 * time.Minute)       // 25m ago (5m backup duration)

	s, err := New(newTestConfig("@every 1h"), st, func() {})
	if err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	due := s.isBackupDue()
	s.mu.Unlock()

	// Anchored on LastScheduledFire (30m ago), next fire is in 30m → not due.
	// If we incorrectly anchored on LastSuccess (25m ago), next fire would be
	// in 35m and we'd still get false here — so also check the precise next
	// time below.
	if due {
		t.Error("isBackupDue() = true with LastScheduledFire 30m ago / @every 1h, want false")
	}

	got, err := s.NextBackupTime()
	if err != nil {
		t.Fatal(err)
	}
	until := time.Until(got)
	// Anchor = LastScheduledFire (30m ago) + 1h = 30m from now.
	// Anchor = LastSuccess (25m ago) + 1h = 35m from now (the bug).
	if until > 32*time.Minute {
		t.Errorf("NextBackupTime() = %s away, want ~30m (anchored on LastScheduledFire); >32m suggests fallback to LastSuccess",
			until)
	}
}

func TestIsBackupDue_FallsBackToLastSuccess(t *testing.T) {
	// Migration path: state files written before LastScheduledFire was added
	// have only LastSuccess. The scheduler must still compute the next fire.
	now := time.Now()
	st := &state.State{}
	st.Backup.LastSuccess = now.Add(-2 * time.Hour) // 2h ago, no LastScheduledFire

	s, err := New(newTestConfig("@every 1h"), st, func() {})
	if err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	due := s.isBackupDue()
	s.mu.Unlock()

	if !due {
		t.Error("isBackupDue() = false with LastSuccess 2h ago and zero LastScheduledFire, want true (migration fallback)")
	}
}

func TestCheckAndTrigger_RecordsScheduledFire(t *testing.T) {
	st := &state.State{}
	st.Backup.LastScheduledFire = time.Now().Add(-2 * time.Hour) // overdue

	fired := make(chan struct{})
	s, err := New(newTestConfig("@every 1h"), st, func() {
		close(fired)
	})
	if err != nil {
		t.Fatal(err)
	}

	before := time.Now()
	s.checkAndTrigger()

	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("onBackup was not invoked within 1s")
	}

	got := st.GetLastScheduledFire()
	if got.Before(before) {
		t.Errorf("LastScheduledFire = %v, expected to be updated to >= %v", got, before)
	}
}

func TestCheckAndTrigger_DoesNotDriftWithBackupDuration(t *testing.T) {
	// Reproduces the bug this change fixes: with the old anchor (LastSuccess),
	// each cycle's "next fire" was computed from when the previous backup
	// COMPLETED, so backup duration cumulatively pushed the schedule forward.
	// With LastScheduledFire as the anchor, the next fire is computed from
	// when the previous schedule FIRED, regardless of how long the backup ran.
	now := time.Now()
	st := &state.State{}
	st.Backup.LastScheduledFire = now.Add(-1 * time.Hour) // fired exactly 1h ago
	// Simulate a long-running backup: success was recorded 5 min after the fire.
	st.Backup.LastSuccess = now.Add(-1*time.Hour + 5*time.Minute)

	s, err := New(newTestConfig("@every 1h"), st, func() {})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.NextBackupTime()
	if err != nil {
		t.Fatal(err)
	}
	until := time.Until(got)
	// Anchored on LastScheduledFire: next fire is now-ish (1h after fire). The
	// 5-min backup duration must not push it to "in 5 minutes".
	if until > 2*time.Minute {
		t.Errorf("NextBackupTime() = %s away, want ~0 (overdue). Drift suggests anchor is LastSuccess, not LastScheduledFire", until)
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

func TestIsBackupDue_CronWallClock(t *testing.T) {
	// Use a cron schedule with two daily fires (08:00 and 18:00) so that
	// "since last success" math is unambiguous regardless of when the test
	// happens to run.
	const spec = "0 8,18 * * *"

	tests := []struct {
		name    string
		lastAgo time.Duration
		wantDue bool
	}{
		// Last success 1m ago — clearly before the next fire of any 10–14h
		// window schedule, so never due regardless of wall-clock time.
		{"1m ago, cron 0 8,18 * * * — not due", 1 * time.Minute, false},
		// Last success 25h ago is guaranteed past at least one fire of a
		// twice-daily schedule (fires at most 14h apart).
		{"25h ago, cron 0 8,18 * * * — due", 25 * time.Hour, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &state.State{}
			st.Backup.LastSuccess = time.Now().Add(-tt.lastAgo)

			s, err := New(newTestConfig(spec), st, func() {})
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
