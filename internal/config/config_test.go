package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			wantErr: "repository.path is required",
		},
		{
			name: "missing password",
			cfg: Config{
				Repository: RepositoryConfig{
					Path: "/backup/repo",
				},
				Backup: BackupConfig{
					Paths: []string{"/home"},
				},
				Schedule: ScheduleConfig{
					Time: "01:00",
				},
			},
			wantErr: "password",
		},
		{
			name: "missing paths",
			cfg: Config{
				Repository: RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Schedule: ScheduleConfig{
					Time: "01:00",
				},
			},
			wantErr: "backup.paths is required",
		},
		{
			name: "missing schedule time",
			cfg: Config{
				Repository: RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Backup: BackupConfig{
					Paths: []string{"/home"},
				},
			},
			wantErr: "schedule.time is required",
		},
		{
			name: "valid config with password",
			cfg: Config{
				Repository: RepositoryConfig{
					Path:     "/backup/repo",
					Password: "secret",
				},
				Backup: BackupConfig{
					Paths: []string{"/home"},
				},
				Schedule: ScheduleConfig{
					Time: "01:00",
				},
			},
			wantErr: "",
		},
		{
			name: "valid config with password file",
			cfg: Config{
				Repository: RepositoryConfig{
					Path:         "/backup/repo",
					PasswordFile: "/path/to/password",
				},
				Backup: BackupConfig{
					Paths: []string{"/home"},
				},
				Schedule: ScheduleConfig{
					Time: "01:00",
				},
			},
			wantErr: "",
		},
		{
			name: "valid config with password command",
			cfg: Config{
				Repository: RepositoryConfig{
					Path:            "/backup/repo",
					PasswordCommand: "pass show backup",
				},
				Backup: BackupConfig{
					Paths: []string{"/home"},
				},
				Schedule: ScheduleConfig{
					Time: "01:00",
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

	configContent := `version: 1
schedule:
  time: "02:00"
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
	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
	if cfg.Schedule.Time != "02:00" {
		t.Errorf("Schedule.Time = %q, want %q", cfg.Schedule.Time, "02:00")
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

	configContent := `version: 1
schedule:
  time: "01:00"
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
	// Test that old config files with 'ephemeral' field are still parsed
	// (the field is ignored but shouldn't cause an error)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `version: 1
schedule:
  time: "01:00"
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

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() should not error on old config with ephemeral: %v", err)
	}

	// Verify the other fields still work
	if !cfg.Tailscale.Enabled {
		t.Error("Tailscale.Enabled should be true")
	}
	if cfg.Tailscale.AuthKey != "tskey-auth-test123" {
		t.Errorf("Tailscale.AuthKey = %q, want %q", cfg.Tailscale.AuthKey, "tskey-auth-test123")
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
		Version: 1,
		Schedule: ScheduleConfig{
			Time:     "03:00",
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

	if loaded.Schedule.Time != cfg.Schedule.Time {
		t.Errorf("Loaded Schedule.Time = %q, want %q", loaded.Schedule.Time, cfg.Schedule.Time)
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
			yaml: `version: 1
schedule:
  time: "01:00"
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
			yaml: `version: 1
schedule:
  time: "01:00"
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
			yaml: `version: 1
schedule:
  time: "01:00"
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
			yaml: `version: 1
schedule:
  time: "01:00"
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
			yaml: `version: 1
schedule:
  time: "01:00"
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
			yaml: `version: 1
schedule:
  time: "01:00"
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
			yaml: `version: 1
schedule:
  time: "01:00"
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
