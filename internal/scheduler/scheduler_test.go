package scheduler

import (
	"sync"
	"testing"
	"time"

	"neubibackup/internal/config"
	"neubibackup/internal/state"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name: "valid config with local timezone",
			cfg: &config.Config{
				Schedule: config.ScheduleConfig{
					Time: "09:00",
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with explicit timezone",
			cfg: &config.Config{
				Schedule: config.ScheduleConfig{
					Time:     "09:00",
					Timezone: "America/New_York",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid timezone",
			cfg: &config.Config{
				Schedule: config.ScheduleConfig{
					Time:     "09:00",
					Timezone: "Invalid/Timezone",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &state.State{}
			onBackup := func() {}

			s, err := New(tt.cfg, st, onBackup)
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

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		timeStr string
		wantHr  int
		wantMin int
		wantErr bool
	}{
		{
			name:    "24hr format",
			timeStr: "15:04",
			wantHr:  15,
			wantMin: 4,
			wantErr: false,
		},
		{
			name:    "midnight",
			timeStr: "00:00",
			wantHr:  0,
			wantMin: 0,
			wantErr: false,
		},
		{
			name:    "with seconds",
			timeStr: "09:30:45",
			wantHr:  9,
			wantMin: 30,
			wantErr: false,
		},
		{
			name:    "invalid format",
			timeStr: "9:30 AM",
			wantErr: true,
		},
		{
			name:    "invalid hour",
			timeStr: "25:00",
			wantErr: true,
		},
		{
			name:    "empty string",
			timeStr: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTime(tt.timeStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTime(%q) error = %v, wantErr %v", tt.timeStr, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.Hour() != tt.wantHr {
					t.Errorf("parseTime(%q) hour = %d, want %d", tt.timeStr, got.Hour(), tt.wantHr)
				}
				if got.Minute() != tt.wantMin {
					t.Errorf("parseTime(%q) minute = %d, want %d", tt.timeStr, got.Minute(), tt.wantMin)
				}
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
		{
			name:     "empty uses local",
			timezone: "",
			wantName: time.Local.String(),
			wantErr:  false,
		},
		{
			name:     "valid timezone",
			timezone: "America/Los_Angeles",
			wantName: "America/Los_Angeles",
			wantErr:  false,
		},
		{
			name:     "UTC",
			timezone: "UTC",
			wantName: "UTC",
			wantErr:  false,
		},
		{
			name:     "invalid timezone",
			timezone: "Not/A/Timezone",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Schedule: config.ScheduleConfig{
					Timezone: tt.timezone,
				},
			}
			got, err := getLocation(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("getLocation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.String() != tt.wantName {
				t.Errorf("getLocation() = %q, want %q", got.String(), tt.wantName)
			}
		})
	}
}

func TestNextBackupTime(t *testing.T) {
	loc := time.Local
	now := time.Now().In(loc)

	// Calculate times that won't wrap around midnight
	// For "future today": use 23:59 which is always later today (unless it's already 23:59)
	// For "passed today": use 00:01 which is always earlier today (unless it's 00:00)
	futureTime := "23:59"
	pastTime := "00:01"

	// Handle edge cases: if we're too close to the boundary times, skip those tests
	currentHour := now.Hour()
	currentMinute := now.Minute()

	tests := []struct {
		name         string
		scheduleTime string
		wantToday    bool // true if should return today, false if tomorrow
		skip         bool
	}{
		{
			name:         "schedule in future today",
			scheduleTime: futureTime,
			wantToday:    true,
			// Skip if it's already 23:59
			skip: currentHour == 23 && currentMinute >= 59,
		},
		{
			name:         "schedule passed today",
			scheduleTime: pastTime,
			wantToday:    false,
			// Skip if it's 00:00 or 00:01 (schedule hasn't passed yet)
			skip: currentHour == 0 && currentMinute <= 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip {
				t.Skip("Skipping due to edge case near midnight")
			}

			cfg := &config.Config{
				Schedule: config.ScheduleConfig{
					Time: tt.scheduleTime,
				},
			}
			st := &state.State{}

			s, err := New(cfg, st, func() {})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			nextTime, err := s.NextBackupTime()
			if err != nil {
				t.Fatalf("NextBackupTime() error = %v", err)
			}

			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			tomorrow := today.AddDate(0, 0, 1)

			if tt.wantToday {
				if nextTime.Before(today) || nextTime.After(tomorrow) {
					t.Errorf("NextBackupTime() = %v, expected today", nextTime)
				}
			} else {
				if nextTime.Before(tomorrow) {
					t.Errorf("NextBackupTime() = %v, expected tomorrow or later", nextTime)
				}
			}
		})
	}
}

func TestTriggerNow(t *testing.T) {
	t.Run("triggers callback", func(t *testing.T) {
		cfg := &config.Config{
			Schedule: config.ScheduleConfig{
				Time: "09:00",
			},
		}
		st := &state.State{}

		var called bool
		var mu sync.Mutex
		onBackup := func() {
			mu.Lock()
			called = true
			mu.Unlock()
		}

		s, err := New(cfg, st, onBackup)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		s.TriggerNow()

		// Wait a bit for the goroutine to execute
		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		if !called {
			t.Error("TriggerNow() did not call onBackup")
		}
		mu.Unlock()
	})

	t.Run("skips if already running", func(t *testing.T) {
		cfg := &config.Config{
			Schedule: config.ScheduleConfig{
				Time: "09:00",
			},
		}
		st := &state.State{}

		callCount := 0
		var mu sync.Mutex
		onBackup := func() {
			mu.Lock()
			callCount++
			mu.Unlock()
			// Simulate a long-running backup
			time.Sleep(100 * time.Millisecond)
		}

		s, err := New(cfg, st, onBackup)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		// Trigger first backup
		s.TriggerNow()

		// Wait a tiny bit for the goroutine to start
		time.Sleep(10 * time.Millisecond)

		// Try to trigger again while running
		s.TriggerNow()

		// Wait for the backup to complete
		time.Sleep(150 * time.Millisecond)

		mu.Lock()
		if callCount != 1 {
			t.Errorf("Expected onBackup to be called once, got %d", callCount)
		}
		mu.Unlock()
	})
}

func TestIsRunning(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			Time: "09:00",
		},
	}
	st := &state.State{}

	started := make(chan struct{})
	done := make(chan struct{})
	onBackup := func() {
		close(started)
		<-done // Wait until test signals to finish
	}

	s, err := New(cfg, st, onBackup)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if s.IsRunning() {
		t.Error("IsRunning() should be false initially")
	}

	s.TriggerNow()
	<-started // Wait for backup to start

	if !s.IsRunning() {
		t.Error("IsRunning() should be true while backup is running")
	}

	close(done) // Signal backup to finish
	time.Sleep(50 * time.Millisecond)

	if s.IsRunning() {
		t.Error("IsRunning() should be false after backup completes")
	}
}

func TestUpdateConfig(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			Time: "09:00",
		},
	}
	st := &state.State{}

	s, err := New(cfg, st, func() {})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Update with valid timezone
	newCfg := &config.Config{
		Schedule: config.ScheduleConfig{
			Time:     "10:00",
			Timezone: "Europe/London",
		},
	}
	if err := s.UpdateConfig(newCfg); err != nil {
		t.Errorf("UpdateConfig() error = %v", err)
	}

	// Update with invalid timezone should fail
	badCfg := &config.Config{
		Schedule: config.ScheduleConfig{
			Time:     "10:00",
			Timezone: "Invalid/Zone",
		},
	}
	if err := s.UpdateConfig(badCfg); err == nil {
		t.Error("UpdateConfig() should error on invalid timezone")
	}
}

func TestScheduler_SkipOnBatteryConfig(t *testing.T) {
	// Test that the SkipOnBattery config is respected
	tests := []struct {
		name          string
		skipOnBattery bool
	}{
		{
			name:          "skip_on_battery disabled",
			skipOnBattery: false,
		},
		{
			name:          "skip_on_battery enabled",
			skipOnBattery: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Schedule: config.ScheduleConfig{
					Time:          "09:00",
					SkipOnBattery: tt.skipOnBattery,
				},
			}
			st := &state.State{}

			s, err := New(cfg, st, func() {})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			// Verify the config is stored correctly
			if s.config.Schedule.SkipOnBattery != tt.skipOnBattery {
				t.Errorf("SkipOnBattery = %v, want %v", s.config.Schedule.SkipOnBattery, tt.skipOnBattery)
			}
		})
	}
}

func TestTriggerNow_IgnoresBatteryStatus(t *testing.T) {
	// Manual triggers (TriggerNow) should always run regardless of battery status
	// This test verifies that TriggerNow does not go through shouldRunNow
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			Time:          "09:00",
			SkipOnBattery: true, // Enabled, but should be ignored for manual triggers
		},
	}
	st := &state.State{}

	var called bool
	var mu sync.Mutex
	onBackup := func() {
		mu.Lock()
		called = true
		mu.Unlock()
	}

	s, err := New(cfg, st, onBackup)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// TriggerNow should work regardless of battery status
	s.TriggerNow()

	// Wait for the goroutine to execute
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !called {
		t.Error("TriggerNow() should call onBackup even with skip_on_battery enabled")
	}
	mu.Unlock()
}

func TestScheduler_AllowedSSIDsConfig(t *testing.T) {
	tests := []struct {
		name         string
		allowedSSIDs []string
	}{
		{
			name:         "no allowed SSIDs (feature disabled)",
			allowedSSIDs: nil,
		},
		{
			name:         "empty allowed SSIDs (feature disabled)",
			allowedSSIDs: []string{},
		},
		{
			name:         "single allowed SSID",
			allowedSSIDs: []string{"HomeWiFi"},
		},
		{
			name:         "multiple allowed SSIDs",
			allowedSSIDs: []string{"HomeWiFi", "OfficeNetwork", "CoffeeShop"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Schedule: config.ScheduleConfig{
					Time:         "09:00",
					AllowedSSIDs: tt.allowedSSIDs,
				},
			}
			st := &state.State{}

			s, err := New(cfg, st, func() {})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			// Verify the config is stored correctly
			if len(s.config.Schedule.AllowedSSIDs) != len(tt.allowedSSIDs) {
				t.Errorf("AllowedSSIDs length = %d, want %d",
					len(s.config.Schedule.AllowedSSIDs), len(tt.allowedSSIDs))
			}
		})
	}
}

func TestIsSSIDAllowed(t *testing.T) {
	tests := []struct {
		name         string
		allowedSSIDs []string
		ssid         string
		want         bool
	}{
		{
			name:         "SSID in list",
			allowedSSIDs: []string{"HomeWiFi", "OfficeNetwork"},
			ssid:         "HomeWiFi",
			want:         true,
		},
		{
			name:         "SSID not in list",
			allowedSSIDs: []string{"HomeWiFi", "OfficeNetwork"},
			ssid:         "CoffeeShop",
			want:         false,
		},
		{
			name:         "empty list",
			allowedSSIDs: []string{},
			ssid:         "AnyNetwork",
			want:         false,
		},
		{
			name:         "case sensitive match",
			allowedSSIDs: []string{"HomeWiFi"},
			ssid:         "homewifi",
			want:         false, // Case-sensitive
		},
		{
			name:         "exact match required",
			allowedSSIDs: []string{"HomeWiFi"},
			ssid:         "HomeWiFi-5G",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Schedule: config.ScheduleConfig{
					Time:         "09:00",
					AllowedSSIDs: tt.allowedSSIDs,
				},
			}
			st := &state.State{}

			s, err := New(cfg, st, func() {})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			got := s.isSSIDAllowed(tt.ssid)
			if got != tt.want {
				t.Errorf("isSSIDAllowed(%q) = %v, want %v", tt.ssid, got, tt.want)
			}
		})
	}
}

func TestTriggerNow_IgnoresSSIDRestriction(t *testing.T) {
	// Manual triggers (TriggerNow) should always run regardless of SSID
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			Time:         "09:00",
			AllowedSSIDs: []string{"HomeWiFi"}, // Restricted, but should be ignored
		},
	}
	st := &state.State{}

	var called bool
	var mu sync.Mutex
	onBackup := func() {
		mu.Lock()
		called = true
		mu.Unlock()
	}

	s, err := New(cfg, st, onBackup)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// TriggerNow should work regardless of SSID
	s.TriggerNow()

	// Wait for the goroutine to execute
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !called {
		t.Error("TriggerNow() should call onBackup even with allowed_ssids configured")
	}
	mu.Unlock()
}

func TestIsBackupDue(t *testing.T) {
	tests := []struct {
		name        string
		schedTime   string
		lastSuccess time.Time
		want        bool
	}{
		{
			name:        "before schedule time - not due",
			schedTime:   "23:59", // Very late, so we're before it
			lastSuccess: time.Time{},
			want:        false,
		},
		{
			name:        "after schedule time with no backup today - due",
			schedTime:   "00:01", // Very early, so we're past it
			lastSuccess: time.Time{},
			want:        true,
		},
		{
			name:        "after schedule time with backup today - not due",
			schedTime:   "00:01",
			lastSuccess: time.Now(), // Backed up today
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip edge case near midnight
			now := time.Now()
			if now.Hour() == 0 && now.Minute() <= 1 {
				t.Skip("Skipping due to edge case near midnight")
			}
			if now.Hour() == 23 && now.Minute() >= 58 {
				t.Skip("Skipping due to edge case near midnight")
			}

			cfg := &config.Config{
				Schedule: config.ScheduleConfig{
					Time: tt.schedTime,
				},
			}
			st := &state.State{}
			if !tt.lastSuccess.IsZero() {
				st.RecordSuccess()
			}

			s, err := New(cfg, st, func() {})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			s.mu.Lock()
			got := s.isBackupDue()
			s.mu.Unlock()

			if got != tt.want {
				t.Errorf("isBackupDue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBackupDue_InvalidTime(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			Time: "invalid",
		},
	}
	st := &state.State{}

	s, err := New(cfg, st, func() {})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	s.mu.Lock()
	got := s.isBackupDue()
	s.mu.Unlock()

	if got {
		t.Error("isBackupDue() should return false for invalid schedule time")
	}
}

func TestCheckBatteryOK(t *testing.T) {
	tests := []struct {
		name          string
		skipOnBattery bool
		wantOK        bool
	}{
		{
			name:          "skip_on_battery disabled - always OK",
			skipOnBattery: false,
			wantOK:        true,
		},
		// Note: We can't easily test the "on battery" case without mocking power.GetBatteryStatus
		// The actual battery check depends on the system state
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Schedule: config.ScheduleConfig{
					Time:          "09:00",
					SkipOnBattery: tt.skipOnBattery,
				},
			}
			st := &state.State{}

			s, err := New(cfg, st, func() {})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			s.mu.Lock()
			got := s.checkBatteryOK()
			s.mu.Unlock()

			if got != tt.wantOK {
				t.Errorf("checkBatteryOK() = %v, want %v", got, tt.wantOK)
			}
		})
	}
}

func TestCheckSSIDOK(t *testing.T) {
	tests := []struct {
		name         string
		allowedSSIDs []string
		wantOK       bool
	}{
		{
			name:         "no allowed SSIDs configured - always OK",
			allowedSSIDs: nil,
			wantOK:       true,
		},
		{
			name:         "empty allowed SSIDs - always OK",
			allowedSSIDs: []string{},
			wantOK:       true,
		},
		// Note: We can't easily test SSID matching without mocking network.GetCurrentNetwork
		// The actual SSID check depends on the system state
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Schedule: config.ScheduleConfig{
					Time:         "09:00",
					AllowedSSIDs: tt.allowedSSIDs,
				},
			}
			st := &state.State{}

			s, err := New(cfg, st, func() {})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			s.mu.Lock()
			got := s.checkSSIDOK()
			s.mu.Unlock()

			if got != tt.wantOK {
				t.Errorf("checkSSIDOK() = %v, want %v", got, tt.wantOK)
			}
		})
	}
}

func TestCheckUserActive(t *testing.T) {
	// This test verifies that checkUserActive returns a boolean
	// The actual idle time depends on the system state, so we just verify it doesn't panic
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			Time: "09:00",
		},
	}
	st := &state.State{}

	s, err := New(cfg, st, func() {})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Just verify it runs without panicking and returns a bool
	got := s.checkUserActive()
	// On a machine being actively used (like during tests), this should be true
	// but we can't guarantee it, so we just check it doesn't panic
	_ = got
}

func TestShouldRunNow_Integration(t *testing.T) {
	// Integration test that verifies shouldRunNow calls all helper functions correctly
	// Skip if we're near midnight to avoid edge cases
	now := time.Now()
	if now.Hour() == 0 && now.Minute() <= 1 {
		t.Skip("Skipping due to edge case near midnight")
	}

	tests := []struct {
		name      string
		schedTime string
		wantRun   bool
	}{
		{
			name:      "before schedule time - should not run",
			schedTime: "23:59",
			wantRun:   false,
		},
		// Note: Testing "should run" case is difficult because it depends on
		// battery, SSID, and idle state which we can't easily control
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Schedule: config.ScheduleConfig{
					Time: tt.schedTime,
				},
			}
			st := &state.State{}

			s, err := New(cfg, st, func() {})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			s.mu.Lock()
			got := s.shouldRunNow()
			s.mu.Unlock()

			if got != tt.wantRun {
				t.Errorf("shouldRunNow() = %v, want %v", got, tt.wantRun)
			}
		})
	}
}
