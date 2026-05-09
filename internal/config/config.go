package config

import (
	"bytes"
	"fmt"
	"os"
	"time"

	cron "github.com/robfig/cron/v3"
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
	Cron          string   `yaml:"cron"`            // Cron expression or "@every <duration>". Default: "@every 24h".
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
	Enabled  bool   `yaml:"enabled"`   // Enable Tailscale connectivity
	AuthKey  string `yaml:"auth_key"`  // Tailscale auth key (tskey-auth-xxx or reusable key)
	Hostname string `yaml:"hostname"`  // Node name in tailnet (defaults to "neubibackup")
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
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w (see README for v2 schema migration)", err)
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
		return nil, 0, fmt.Errorf("schedule.cron fires too frequently: current gap is %s, must be at least %s", minGap, minScheduleGap)
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
