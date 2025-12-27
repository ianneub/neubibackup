package config

import (
	"fmt"
	"os"
)

// DefaultConfigTemplate is the template for a new config file with helpful comments.
const DefaultConfigTemplate = `# NeubiBackup Configuration
# Documentation: https://github.com/yourusername/neubibackup
version: 1

# Schedule settings
schedule:
  time: "01:00"              # 24-hour format, local time
  # timezone: ""             # Optional, defaults to system timezone (e.g., "America/New_York")

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
    - ""  # Add paths to backup, e.g., "/Users/username/Documents"
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
  #   --pack-size 95       (optimal for REST server)
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
tailscale:
  enabled: false
  # Auth key for headless login (get from https://login.tailscale.com/admin/settings/keys)
  # Use a reusable key for long-term operation.
  auth_key: ""
  # Hostname for this device in your tailnet (defaults to "neubibackup")
  hostname: "neubibackup"
  # Ephemeral mode: device is removed from tailnet when app closes
  # Recommended: false (keeps device registered for easier management)
  ephemeral: false

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
