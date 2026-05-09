# NeubiBackup

[![Test](https://github.com/ianneub/neubibackup/actions/workflows/test.yml/badge.svg)](https://github.com/ianneub/neubibackup/actions/workflows/test.yml)

A simple system tray application that automatically backs up your files using [restic](https://restic.net/).

## Features

- **Configurable schedule** - Cron expression or `@every <duration>` (hourly, daily, custom cadence)
- **Missed backup detection** - If your computer was off or asleep, backs up when you return
- **Retry with backoff** - Automatically retries failed backups (up to 5 attempts)
- **System tray interface** - Runs quietly in the background with status at a glance
- **Progress display** - See real-time progress during backups
- **Monitoring integrations** - Optional healthchecks.io and Pushover notifications
- **No restic installation required** - Restic is bundled with the app
- **Automatic updates** - Updates are downloaded and applied silently in the background
- **Tailscale integration** - Optional support for accessing private repos via Tailscale
- **WiFi network filtering** - Optional: Only run scheduled backups on specific networks

## Supported Platforms

- macOS (Apple Silicon and Intel)
- Windows (64-bit)

## Installation

Download the latest release for your platform:

**[Download from GitHub Releases](https://github.com/ianneub/neubibackup/releases)**

### macOS

1. Download the `.dmg` file
2. Open it and drag NeubiBackup to Applications
3. Remove the quarantine attribute (required for unsigned apps):

   ```bash
   xattr -d com.apple.quarantine /Applications/NeubiBackup.app
   ```

4. On first launch, you may need to allow it in System Settings > Privacy & Security

### Windows

1. Download the `-setup.exe` installer
2. Run the installer and follow the prompts
3. On launch, you'll see a UAC prompt - this is required for VSS snapshots (consistent backups of open files) and automatic updates

## Quick Start

1. **Launch NeubiBackup** - The app starts in your system tray (menu bar on macOS)
2. **Edit configuration** - On first run, a config file opens automatically. If not, click the tray icon and select "Open Config File"
3. **Set your repository** - Add your restic repository URL and password
4. **Add backup paths** - List the folders you want to back up
5. **Save and close** - The app automatically reloads your configuration

## Configuration

Configuration is stored in `~/neubibackup/config.yaml`. Here's a complete example:

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

# Your restic repository
repository:
  # Examples:
  # - Local: /Volumes/Backup/restic-repo
  # - REST server: rest:https://user:pass@backup.example.com/repo
  # - S3: s3:s3.amazonaws.com/bucket-name
  # - Backblaze B2: b2:bucket-name:path/to/repo
  path: "rest:https://user:pass@backup.example.com/repo"

  # Password (choose one method):
  password: "your-repository-password"
  # OR use a file:
  # password_file: "/path/to/password-file"
  # OR use a command (most secure):
  # password_command: "security find-generic-password -s restic -w"

# What to back up
backup:
  paths:
    # macOS:
    - "/Users/yourname/Documents"
    - "/Users/yourname/Pictures"
    # Windows (use forward slashes OR single quotes with backslashes):
    # - "C:/Users/yourname/Documents"
    # - 'C:\Users\yourname\Pictures'
  excludes:
    - "*.tmp"
    - ".DS_Store"
    - "node_modules"
    - ".Trash"
  # exclude_file: "/path/to/excludes.txt"  # Optional: file with exclude patterns (one per line)

# Optional: Additional restic arguments
restic_args:
  global: []                 # Args for all commands
  backup:                    # Args for backup command
    - "--verbose"
  # Note: The following flags are always added automatically:
  #   --one-file-system    (don't cross filesystem boundaries)
  #   --exclude-caches     (skip directories with CACHEDIR.TAG)
  #   --use-fs-snapshot    (Windows only: use VSS for consistent snapshots)

# Optional: healthchecks.io monitoring
healthchecks:
  enabled: false
  ping_url: "https://hc-ping.com/your-uuid-here"
  send_logs_on_failure: true

# Optional: Pushover notifications
pushover:
  enabled: false
  user_key: "your-user-key"
  api_token: "your-api-token"
  on_failure: true
  on_success: false

# Optional: Tailscale integration
# Enable this to access restic REST servers that are only reachable via Tailscale.
# The device stays registered in your tailnet - auth key is only needed for initial setup.
tailscale:
  enabled: false
  auth_key: ""               # Get from https://login.tailscale.com/admin/settings/keys
  hostname: "neubibackup"    # Hostname for this device in your tailnet
```

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

### Setting Up a Repository

Before using NeubiBackup, you need a restic repository. See the [restic documentation](https://restic.readthedocs.io/en/latest/030_preparing_a_new_repo.html) for setup instructions.

Common options:

- **Local drive**: External hard drive or NAS
- **Cloud storage**: Backblaze B2, AWS S3, Google Cloud Storage
- **REST server**: Self-hosted [rest-server](https://github.com/restic/rest-server)

## Usage

### Tray Menu Options

Click the tray icon to access:

- **Status** - Shows time since last successful backup, or progress during backup
- **Backup Now** - Start an immediate backup (becomes "Stop Backup" while running)
- **Open Config File** - Edit your configuration
- **Open Logs Folder** - View backup logs
- **Start at Login** - Toggle automatic startup
- **Check for Updates** - Check for and install new versions
- **Version** - Shows current app and restic versions

### Icon Status

- **Green** - Last backup succeeded
- **Animated** - Backup in progress
- **Red** - Last backup failed or not configured
- **Gray** - Configured but no backup yet

## Data Location

All NeubiBackup data is stored in `~/neubibackup/`:

```
~/neubibackup/
├── config.yaml    # Your configuration
├── state.yaml     # Backup state (managed by app)
└── logs/          # Backup logs (last 25 kept)
```

## Troubleshooting

### Backup keeps failing

1. Click **Open Logs Folder** from the tray menu to see backup-specific logs
2. Click **Open App Log** to see application logs (useful for update errors and startup issues)
3. Open the most recent log file to see error details
4. Common issues:
   - Repository not accessible (check network/credentials)
   - Invalid paths in backup configuration
   - Insufficient permissions

### App won't start

- **macOS**: Check System Settings > Privacy & Security for blocked apps
- **Windows**: Allow the UAC prompt - admin privileges are required for VSS snapshots

### Missed backups not running

The app detects missed backups when:

- It's running (either at startup or after wake from sleep)
- The schedule has fired at least once since the last successful backup (e.g. with `cron: "@every 1h"` and the last success more than an hour ago)

Make sure the app is set to **Start at Login** for reliable scheduling.

### Battery power

If you enable `skip_on_battery: true` in your config, scheduled backups will be deferred while running on battery power. The backup will run automatically when AC power is restored (if the schedule has fired since the last successful backup). Manual backups via "Backup Now" always run regardless of power state.

### WiFi SSID restrictions

If you configure `allowed_ssids`, scheduled backups will only run when connected to one of the listed WiFi SSIDs. Behavior:

- If your current SSID matches any in the list: backup runs
- If your SSID doesn't match: backup is skipped (will retry on next schedule check)
- If WiFi is off/disconnected/cannot be detected: backup runs (fail-open for reliability)
- Manual backups via "Backup Now" always run regardless of SSID

Note: SSID matching is case-sensitive.

**macOS Location Permission Required**: On macOS 14 (Sonoma) and later, accessing the WiFi SSID requires Location Services permission. When you first configure `allowed_ssids`, macOS will prompt you to grant location access. If you miss the prompt or need to enable it manually:

1. Open **System Settings** > **Privacy & Security** > **Location Services**
2. Enable Location Services (if disabled)
3. Find **NeubiBackup** in the app list and enable it

If Location Services permission is not granted, SSID detection will fail and backups will proceed (fail-open behavior).

### User activity requirement

Scheduled backups only run when you've been active at your computer within the last 2 hours. This prevents backups from attempting to run overnight when your keychain might be locked (which would cause `password_command` to fail).

When the app checks if a backup should run:

- If you've had keyboard/mouse activity in the last 2 hours: backup proceeds
- If you've been idle for more than 2 hours: backup is skipped (will retry on next schedule check)

This means backups automatically run when you're actively using your computer (keychain unlocked), and wait when you're away. Manual backups via "Backup Now" always run regardless of activity.

The activity detection works on macOS and Windows.

### Windows autostart not working

NeubiBackup uses Windows Task Scheduler for automatic startup, which allows the app to start with administrator privileges needed for VSS snapshots.

If "Start at Login" isn't working:

1. Open Task Scheduler (search for it in the Start menu)
2. Look for "NeubiBackup" task in the Task Scheduler Library
3. If missing, toggle the "Start at Login" option off and on in the tray menu

## Automatic Updates

NeubiBackup checks for updates automatically every 24 hours and on startup. Updates are applied silently in the background:

- If a backup is running, the update waits for it to complete
- The app downloads and applies the update automatically
- The app restarts itself with the new version
- No user interaction required

The tray menu shows the current update status:

- **"Check for Updates"** - Click to manually check for updates
- **"Update Available (vX.Y.Z)"** - An update was found (will be applied automatically)
- **"Updating..."** - Update is being downloaded and applied

### macOS Note

After updating, macOS Gatekeeper may block the app because it's not code-signed. If this happens, run:

```bash
xattr -d com.apple.quarantine /Applications/NeubiBackup.app
```

Then relaunch the app.

### App Bundle Updates

Auto-updates on macOS only update the executable binary, not the full app bundle. If a new version adds features requiring macOS permissions (like Location Services for WiFi SSID detection), you'll need to manually reinstall from the DMG to get the updated Info.plist with the new permission descriptions.

## License

MIT
