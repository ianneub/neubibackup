package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration.
type Config struct {
	Version int `yaml:"version"`

	LogLevel string `yaml:"log_level"` // debug, info, warn, error (default: info)

	Schedule ScheduleConfig `yaml:"schedule"`

	Repository RepositoryConfig `yaml:"repository"`

	Backup BackupConfig `yaml:"backup"`

	ResticArgs ResticArgsConfig `yaml:"restic_args"`

	Healthchecks HealthchecksConfig `yaml:"healthchecks"`

	Pushover PushoverConfig `yaml:"pushover"`

	Tailscale TailscaleConfig `yaml:"tailscale"`
}

// ScheduleConfig defines when backups should run.
type ScheduleConfig struct {
	Time          string   `yaml:"time"`            // 24-hour format, e.g., "01:00"
	Timezone      string   `yaml:"timezone"`        // Optional, defaults to system timezone
	SkipOnBattery bool     `yaml:"skip_on_battery"` // Skip scheduled backups when on battery power
	AllowedSSIDs  []string `yaml:"allowed_ssids"`   // Only run scheduled backups on these WiFi SSIDs (empty = no restriction)
}

// RepositoryConfig defines the restic repository settings.
type RepositoryConfig struct {
	Path            string `yaml:"path"`             // Repository path or URL
	Password        string `yaml:"password"`         // Password directly (less secure)
	PasswordFile    string `yaml:"password_file"`    // Path to password file
	PasswordCommand string `yaml:"password_command"` // Command to get password
}

// BackupConfig defines what to backup.
type BackupConfig struct {
	Paths       []string `yaml:"paths"`        // Paths to backup
	Excludes    []string `yaml:"excludes"`     // Patterns to exclude
	ExcludeFile string   `yaml:"exclude_file"` // Path to exclude patterns file
}

// ResticArgsConfig defines additional restic arguments.
type ResticArgsConfig struct {
	Global []string `yaml:"global"` // Args for all commands
	Backup []string `yaml:"backup"` // Args for backup command only
}

// HealthchecksConfig defines healthchecks.io integration.
type HealthchecksConfig struct {
	Enabled           bool   `yaml:"enabled"`
	PingURL           string `yaml:"ping_url"`
	SendLogsOnFailure bool   `yaml:"send_logs_on_failure"`
}

// PushoverConfig defines Pushover notification settings.
type PushoverConfig struct {
	Enabled   bool   `yaml:"enabled"`
	UserKey   string `yaml:"user_key"`
	APIToken  string `yaml:"api_token"`
	OnSuccess bool   `yaml:"on_success"`
	OnFailure bool   `yaml:"on_failure"`
}

// TailscaleConfig defines Tailscale network settings for accessing private repositories.
type TailscaleConfig struct {
	Enabled  bool     `yaml:"enabled"`   // Enable Tailscale connectivity
	AuthKey  string   `yaml:"auth_key"`  // Tailscale auth key (tskey-auth-xxx) or OAuth client secret (tskey-client-xxx)
	Hostname string   `yaml:"hostname"`  // Node name in tailnet (defaults to "neubibackup")
	Tags     []string `yaml:"tags"`      // ACL tags to advertise (required for OAuth client secrets)
}

// Load reads the config from the default config file.
func Load() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, fmt.Errorf("getting config path: %w", err)
	}
	return LoadFromFile(configPath)
}

// LoadFromFile reads the config from a specific file.
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return &cfg, nil
}

// Save writes the config to the default config file.
func (c *Config) Save() error {
	configPath, err := GetConfigPath()
	if err != nil {
		return fmt.Errorf("getting config path: %w", err)
	}
	return c.SaveToFile(configPath)
}

// SaveToFile writes the config to a specific file.
func (c *Config) SaveToFile(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// Validate checks if the config has the minimum required fields.
func (c *Config) Validate() error {
	if c.Repository.Path == "" {
		return fmt.Errorf("repository.path is required")
	}
	if c.Repository.Password == "" && c.Repository.PasswordFile == "" && c.Repository.PasswordCommand == "" {
		return fmt.Errorf("repository.password, repository.password_file, or repository.password_command is required")
	}
	if len(c.Backup.Paths) == 0 {
		return fmt.Errorf("backup.paths is required")
	}
	if c.Schedule.Time == "" {
		return fmt.Errorf("schedule.time is required")
	}
	return nil
}

// IsConfigured returns true if the config has been filled out by the user.
// This checks for the presence of critical fields that wouldn't be in the template.
func (c *Config) IsConfigured() bool {
	return c.Repository.Path != "" &&
		c.Repository.Path != "rest:https://user:pass@backup.example.com/repo" &&
		len(c.Backup.Paths) > 0
}

// IsTailscaleEnabled returns true if Tailscale is configured and enabled.
func (c *Config) IsTailscaleEnabled() bool {
	return c.Tailscale.Enabled && c.Tailscale.AuthKey != ""
}
