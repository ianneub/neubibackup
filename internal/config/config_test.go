package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "empty config",
			cfg:     Config{},
			wantErr: "schema version",
		},
		{
			name: "missing password",
			cfg: Config{
				Version: 2,
				Repository: RepositoryConfig{
					Path: "/backup/repo",
				},
				Backup: BackupConfig{
					Paths: []string{"/home"},
				},
				Schedule: ScheduleConfig{
					Cron: "@every 24h",
				},
			},
			wantErr: "password",
		},
		{
			name: "missing paths",
			cfg: Config{
				Version: 2,
				Repository: RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Schedule: ScheduleConfig{
					Cron: "@every 24h",
				},
			},
			wantErr: "backup.paths is required",
		},
		{
			name: "valid config with password",
			cfg: Config{
				Version: 2,
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
			},
			wantErr: "",
		},
		{
			name: "valid config with password file",
			cfg: Config{
				Version: 2,
				Repository: RepositoryConfig{
					Path:         "/backup/repo",
					PasswordFile: "/path/to/password",
				},
				Backup: BackupConfig{
					Paths: []string{"/home"},
				},
				Schedule: ScheduleConfig{
					Cron: "@every 24h",
				},
			},
			wantErr: "",
		},
		{
			name: "valid config with password command",
			cfg: Config{
				Version: 2,
				Repository: RepositoryConfig{
					Path:            "/backup/repo",
					PasswordCommand: "pass show backup",
				},
				Backup: BackupConfig{
					Paths: []string{"/home"},
				},
				Schedule: ScheduleConfig{
					Cron: "@every 24h",
				},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() = nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestIsConfigured(t *testing.T) {
	tests := []struct {
		name       string
		cfg        Config
		configured bool
	}{
		{
			name:       "empty config",
			cfg:        Config{},
			configured: false,
		},
		{
			name: "template config (not configured)",
			cfg: Config{
				Repository: RepositoryConfig{
					Path: "rest:https://user:pass@backup.example.com/repo",
				},
				Backup: BackupConfig{
					Paths: []string{"/home/user"},
				},
			},
			configured: false,
		},
		{
			name: "no paths",
			cfg: Config{
				Repository: RepositoryConfig{
					Path: "/my/actual/repo",
				},
			},
			configured: false,
		},
		{
			name: "valid config",
			cfg: Config{
				Repository: RepositoryConfig{
					Path: "/my/actual/repo",
				},
				Backup: BackupConfig{
					Paths: []string{"/home/user"},
				},
			},
			configured: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cfg.IsConfigured()
			if result != tt.configured {
				t.Errorf("IsConfigured() = %v, want %v", result, tt.configured)
			}
		})
	}
}

func TestIsTailscaleEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		enabled bool
	}{
		{
			name:    "disabled by default",
			cfg:     Config{},
			enabled: false,
		},
		{
			name: "enabled but no auth key",
			cfg: Config{
				Tailscale: TailscaleConfig{
					Enabled: true,
				},
			},
			enabled: false,
		},
		{
			name: "enabled with auth key",
			cfg: Config{
				Tailscale: TailscaleConfig{
					Enabled: true,
					AuthKey: "tskey-auth-xxx",
				},
			},
			enabled: true,
		},
		{
			name: "auth key but not enabled",
			cfg: Config{
				Tailscale: TailscaleConfig{
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

func TestGetAppDir(t *testing.T) {
	appDir, err := GetAppDir()
	if err != nil {
		t.Fatalf("GetAppDir() error = %v", err)
	}

	// In dev builds, path ends with ".dev-data"
	// In production builds, path ends with "neubibackup"
	if !strings.HasSuffix(appDir, ".dev-data") && !strings.HasSuffix(appDir, "neubibackup") {
		t.Errorf("GetAppDir() = %q, want path ending with '.dev-data' or 'neubibackup'", appDir)
	}

	// Should be an absolute path
	if !filepath.IsAbs(appDir) {
		t.Errorf("GetAppDir() = %q, want absolute path", appDir)
	}

	// Call twice to ensure consistency
	appDir2, err := GetAppDir()
	if err != nil {
		t.Fatalf("GetAppDir() second call error = %v", err)
	}
	if appDir != appDir2 {
		t.Errorf("GetAppDir() inconsistent: first %q, second %q", appDir, appDir2)
	}
}

func TestGetAppDir_EnvOverride(t *testing.T) {
	// Save original env and restore after test
	original := os.Getenv("NEUBIBACKUP_APP_DIR")
	defer os.Setenv("NEUBIBACKUP_APP_DIR", original)

	testDir := "/custom/test/dir"
	os.Setenv("NEUBIBACKUP_APP_DIR", testDir)

	appDir, err := GetAppDir()
	if err != nil {
		t.Fatalf("GetAppDir() error = %v", err)
	}

	if appDir != testDir {
		t.Errorf("GetAppDir() = %q, want %q", appDir, testDir)
	}
}

func TestGetConfigPath(t *testing.T) {
	configPath, err := GetConfigPath()
	if err != nil {
		t.Fatalf("GetConfigPath() error = %v", err)
	}

	// Should end with config.yaml
	if !strings.HasSuffix(configPath, "config.yaml") {
		t.Errorf("GetConfigPath() = %q, want path ending with 'config.yaml'", configPath)
	}

	// Should be under app dir
	appDir, _ := GetAppDir()
	if !strings.HasPrefix(configPath, appDir) {
		t.Errorf("GetConfigPath() = %q, should be under %q", configPath, appDir)
	}
}

func TestGetStatePath(t *testing.T) {
	statePath, err := GetStatePath()
	if err != nil {
		t.Fatalf("GetStatePath() error = %v", err)
	}

	// Should end with state.yaml
	if !strings.HasSuffix(statePath, "state.yaml") {
		t.Errorf("GetStatePath() = %q, want path ending with 'state.yaml'", statePath)
	}

	// Should be under app dir
	appDir, _ := GetAppDir()
	if !strings.HasPrefix(statePath, appDir) {
		t.Errorf("GetStatePath() = %q, should be under %q", statePath, appDir)
	}
}

func TestGetLogsDir(t *testing.T) {
	logsDir, err := GetLogsDir()
	if err != nil {
		t.Fatalf("GetLogsDir() error = %v", err)
	}

	// Should end with "logs"
	if !strings.HasSuffix(logsDir, "logs") {
		t.Errorf("GetLogsDir() = %q, want path ending with 'logs'", logsDir)
	}

	// Should be under app dir
	appDir, _ := GetAppDir()
	if !strings.HasPrefix(logsDir, appDir) {
		t.Errorf("GetLogsDir() = %q, should be under %q", logsDir, appDir)
	}
}

func TestGetTailscaleDir(t *testing.T) {
	tsDir, err := GetTailscaleDir()
	if err != nil {
		t.Fatalf("GetTailscaleDir() error = %v", err)
	}

	// Should end with "tailscale"
	if !strings.HasSuffix(tsDir, "tailscale") {
		t.Errorf("GetTailscaleDir() = %q, want path ending with 'tailscale'", tsDir)
	}

	// Should be under app dir
	appDir, _ := GetAppDir()
	if !strings.HasPrefix(tsDir, appDir) {
		t.Errorf("GetTailscaleDir() = %q, should be under %q", tsDir, appDir)
	}
}

func TestLoadFromFile(t *testing.T) {
	// Create a temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `version: 2
schedule:
  cron: "@every 24h"
  timezone: "UTC"
repository:
  path: "/backup/repo"
  password: "secret123"
backup:
  paths:
    - /home/user
    - /etc
  excludes:
    - "*.tmp"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	// Verify loaded values
	if cfg.Version != 2 {
		t.Errorf("Version = %d, want 2", cfg.Version)
	}
	if cfg.Schedule.Cron != "@every 24h" {
		t.Errorf("Schedule.Cron = %q, want %q", cfg.Schedule.Cron, "@every 24h")
	}
	if cfg.Schedule.Timezone != "UTC" {
		t.Errorf("Schedule.Timezone = %q, want %q", cfg.Schedule.Timezone, "UTC")
	}
	if cfg.Repository.Path != "/backup/repo" {
		t.Errorf("Repository.Path = %q, want %q", cfg.Repository.Path, "/backup/repo")
	}
	if cfg.Repository.Password != "secret123" {
		t.Errorf("Repository.Password = %q, want %q", cfg.Repository.Password, "secret123")
	}
	if len(cfg.Backup.Paths) != 2 {
		t.Errorf("len(Backup.Paths) = %d, want 2", len(cfg.Backup.Paths))
	}
	if len(cfg.Backup.Excludes) != 1 || cfg.Backup.Excludes[0] != "*.tmp" {
		t.Errorf("Backup.Excludes = %v, want [*.tmp]", cfg.Backup.Excludes)
	}
}

func TestLoadFromFile_TailscaleConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `version: 2
schedule:
  cron: "@every 24h"
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
tailscale:
  enabled: true
  auth_key: "tskey-auth-test123"
  hostname: "mybackup"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if !cfg.Tailscale.Enabled {
		t.Error("Tailscale.Enabled should be true")
	}
	if cfg.Tailscale.AuthKey != "tskey-auth-test123" {
		t.Errorf("Tailscale.AuthKey = %q, want %q", cfg.Tailscale.AuthKey, "tskey-auth-test123")
	}
	if cfg.Tailscale.Hostname != "mybackup" {
		t.Errorf("Tailscale.Hostname = %q, want %q", cfg.Tailscale.Hostname, "mybackup")
	}
}

func TestLoadFromFile_TailscaleBackwardCompatibility(t *testing.T) {
	// With strict YAML decoding (v2 schema), unknown fields like 'ephemeral' are rejected.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `version: 2
schedule:
  cron: "@every 24h"
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
tailscale:
  enabled: true
  auth_key: "tskey-auth-test123"
  hostname: "mybackup"
  ephemeral: true
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := LoadFromFile(configPath)
	if err == nil {
		t.Fatal("LoadFromFile() should error for unknown field 'ephemeral' with strict decoding")
	}
}

func TestLoadFromFile_NotFound(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/config.yaml")
	if err == nil {
		t.Error("LoadFromFile() should error for non-existent file")
	}
}

func TestLoadFromFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write invalid YAML
	if err := os.WriteFile(configPath, []byte("invalid: yaml: content:"), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := LoadFromFile(configPath)
	if err == nil {
		t.Error("LoadFromFile() should error for invalid YAML")
	}
}

func TestSaveToFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := &Config{
		Version: 2,
		Schedule: ScheduleConfig{
			Cron:     "@every 24h",
			Timezone: "America/New_York",
		},
		Repository: RepositoryConfig{
			Path:     "s3:bucket/path",
			Password: "test",
		},
		Backup: BackupConfig{
			Paths: []string{"/data"},
		},
	}

	if err := cfg.SaveToFile(configPath); err != nil {
		t.Fatalf("SaveToFile() error = %v", err)
	}

	// Verify file was created
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("Config file not created: %v", err)
	}

	// Verify permissions (0600)
	if info.Mode().Perm() != 0600 {
		t.Errorf("Config file permissions = %o, want 0600", info.Mode().Perm())
	}

	// Load it back and verify
	loaded, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if loaded.Schedule.Cron != cfg.Schedule.Cron {
		t.Errorf("Loaded Schedule.Cron = %q, want %q", loaded.Schedule.Cron, cfg.Schedule.Cron)
	}
	if loaded.Repository.Path != cfg.Repository.Path {
		t.Errorf("Loaded Repository.Path = %q, want %q", loaded.Repository.Path, cfg.Repository.Path)
	}
}

func TestConfigExists(t *testing.T) {
	// This test depends on the actual filesystem state
	// Just verify it doesn't error
	_, err := ConfigExists()
	if err != nil {
		t.Errorf("ConfigExists() error = %v", err)
	}
}

func TestLoadFromFile_SkipOnBattery(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected bool
	}{
		{
			name: "not specified defaults to false",
			yaml: `version: 2
schedule:
  cron: "@every 24h"
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
`,
			expected: false,
		},
		{
			name: "explicitly false",
			yaml: `version: 2
schedule:
  cron: "@every 24h"
  skip_on_battery: false
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
`,
			expected: false,
		},
		{
			name: "explicitly true",
			yaml: `version: 2
schedule:
  cron: "@every 24h"
  skip_on_battery: true
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")

			if err := os.WriteFile(configPath, []byte(tt.yaml), 0600); err != nil {
				t.Fatalf("Failed to write test config: %v", err)
			}

			cfg, err := LoadFromFile(configPath)
			if err != nil {
				t.Fatalf("LoadFromFile() error = %v", err)
			}

			if cfg.Schedule.SkipOnBattery != tt.expected {
				t.Errorf("Schedule.SkipOnBattery = %v, want %v", cfg.Schedule.SkipOnBattery, tt.expected)
			}
		})
	}
}

func TestLoadFromFile_AllowedSSIDs(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []string
	}{
		{
			name: "not specified defaults to empty",
			yaml: `version: 2
schedule:
  cron: "@every 24h"
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
`,
			expected: nil,
		},
		{
			name: "empty list",
			yaml: `version: 2
schedule:
  cron: "@every 24h"
  allowed_ssids: []
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
`,
			expected: []string{},
		},
		{
			name: "single SSID",
			yaml: `version: 2
schedule:
  cron: "@every 24h"
  allowed_ssids:
    - "HomeWiFi"
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
`,
			expected: []string{"HomeWiFi"},
		},
		{
			name: "multiple SSIDs",
			yaml: `version: 2
schedule:
  cron: "@every 24h"
  allowed_ssids:
    - "HomeWiFi"
    - "OfficeNetwork"
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
`,
			expected: []string{"HomeWiFi", "OfficeNetwork"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")

			if err := os.WriteFile(configPath, []byte(tt.yaml), 0600); err != nil {
				t.Fatalf("Failed to write test config: %v", err)
			}

			cfg, err := LoadFromFile(configPath)
			if err != nil {
				t.Fatalf("LoadFromFile() error = %v", err)
			}

			if len(cfg.Schedule.AllowedSSIDs) != len(tt.expected) {
				t.Errorf("Schedule.AllowedSSIDs = %v, want %v",
					cfg.Schedule.AllowedSSIDs, tt.expected)
				return
			}
			for i, ssid := range cfg.Schedule.AllowedSSIDs {
				if i < len(tt.expected) && ssid != tt.expected[i] {
					t.Errorf("Schedule.AllowedSSIDs[%d] = %q, want %q",
						i, ssid, tt.expected[i])
				}
			}
		})
	}
}

func TestLoadFromFile_LogLevel(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected string
	}{
		{
			name: "log_level debug",
			yaml: `version: 2
log_level: "debug"
schedule:
  cron: "@every 24h"
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
`,
			expected: "debug",
		},
		{
			name: "log_level error",
			yaml: `version: 2
log_level: "error"
schedule:
  cron: "@every 24h"
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
`,
			expected: "error",
		},
		{
			name: "log_level not set (empty string)",
			yaml: `version: 2
schedule:
  cron: "@every 24h"
repository:
  path: "/backup/repo"
  password: "secret"
backup:
  paths:
    - /home/user
`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")

			if err := os.WriteFile(configPath, []byte(tt.yaml), 0600); err != nil {
				t.Fatalf("Failed to write test config: %v", err)
			}

			cfg, err := LoadFromFile(configPath)
			if err != nil {
				t.Fatalf("LoadFromFile() error = %v", err)
			}

			if cfg.LogLevel != tt.expected {
				t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, tt.expected)
			}
		})
	}
}

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
		{"0 8,18 * * *", 10 * time.Hour},
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
