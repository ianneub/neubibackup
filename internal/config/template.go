package config

import (
	"fmt"
	"os"
)

// DefaultConfigTemplate is the template for a new config file with helpful comments.
const DefaultConfigTemplate = `# NeubiBackup Configuration
# Documentation: https://github.com/ianneub/neubibackup
version: 1

# Log verbosity: debug, info, warn, error (default: info)
# log_level: "info"

# Schedule settings
schedule:
  time: "01:00"              # 24-hour format, local time
  # timezone: ""             # Optional, defaults to system timezone (e.g., "America/New_York")
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
    - ".git"
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
