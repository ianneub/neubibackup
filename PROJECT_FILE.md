# Restic Backup Scheduler - Project Specification

## Overview

A cross-platform (macOS + Windows) system tray application that manages scheduled restic backups with intelligent retry logic and health monitoring.

## Problem Statement

The user currently uses restic to backup:

1. Their macOS laptop
2. Their grandparents' Windows PC

Currently requires two different solutions for scheduling, error reporting, and handling computers that are off or asleep. Need a single unified solution.

## Core Requirements

### 1. Cross-Platform System Tray App

- Runs on macOS and Windows
- Minimal footprint - sits in status bar/system tray
- Shows backup status at a glance (icon color or badge)
- Not intrusive - no main window, just tray icon with menu

### 2. Smart Scheduling

- Configure a daily backup time (e.g., 1:00 AM)
- **Missed schedule detection**: If computer was off/asleep at scheduled time, run backup as soon as possible when computer wakes
- Track last successful backup date to ensure at least one backup per day
- Prevent duplicate runs (don't backup twice if already succeeded today)

### 3. Healthchecks.io Integration

- Ping start endpoint when backup begins: `GET https://hc-ping.com/{uuid}/start`
- Ping success endpoint on completion: `GET https://hc-ping.com/{uuid}`
- Ping fail endpoint on error: `GET https://hc-ping.com/{uuid}/fail`
- Optional: Send log output in POST body on failure

### 4. Embedded Restic Binary

- Bundle restic binary inside the Go application using `//go:embed`
- Extract to user's cache directory on first run
- Eliminates need for users to install restic separately
- Simplifies setup especially for non-technical users (grandparents)

## Technical Decisions

### Language: Go

**Rationale:**

- Excellent cross-compilation (`GOOS=windows go build` from macOS)
- Simple process spawning via `os/exec`
- Easy concurrency with goroutines
- Single binary output, no runtime dependencies
- `//go:embed` for bundling restic binary

### System Tray Library

- `github.com/getlantern/systray` - Popular, well-maintained

### Autostart Library

- `github.com/emersion/go-autostart` - Cross-platform launch at login
  - macOS: XDG autostart (desktop entries)
  - Windows: Registry keys
  - Simple API: Enable/Disable/IsEnabled

### Notifications

- Pushover API - Cross-platform push notifications
  - Simple HTTP POST to send notifications
  - Works on iOS, Android, and desktop
  - Better for non-technical users (grandparents can get phone notifications)

### Configuration Format

- YAML file in user's home directory (using gopkg.in/yaml.v3)
- All platforms: `~/neubibackup/config.yaml`
- Cross-platform path resolution via `os.UserHomeDir()` + `filepath.Join()`

### Data Directory Structure

```
~/neubibackup/
├── config.yaml      # User-edited configuration
├── state.yaml       # App-managed backup state
└── logs/            # Backup run logs
    ├── 2024-01-15T01-00-00.log
    ├── 2024-01-16T01-00-00.log
    └── ...
```

### First-Run Configuration Flow

On first launch when no config exists:

1. Create `~/neubibackup/` directory
2. Write template `config.yaml` with helpful comments
3. Open config file in default editor (`open` on macOS, `notepad` on Windows)
4. Show tray icon in "unconfigured" state
5. Tray menu shows "Configuration required..." at top

### Log Retention

- Log files named with timestamp: `YYYY-MM-DDTHH-MM-SS.log` (colons replaced for Windows compatibility)
- Keep last 25 log files regardless of success/failure
- After each backup, delete oldest files if count exceeds 25
- Natural sort order due to ISO-8601 naming

## Project Structure

```
restic-scheduler/
├── main.go                     # Entry point, tray setup, event loop
├── go.mod
├── go.sum
├── CLAUDE.md                   # Context for Claude Code
│
├── binaries/                   # Embedded restic binaries (git-ignored, downloaded during build)
│   ├── restic-darwin-amd64
│   ├── restic-darwin-arm64
│   └── restic-windows-amd64.exe
│
├── internal/
│   ├── config/
│   │   └── config.go           # Load/save YAML config, default paths
│   │
│   ├── scheduler/
│   │   └── scheduler.go        # Schedule tracking, "should run now?" logic
│   │
│   ├── restic/
│   │   ├── embed.go            # //go:embed directives and extraction
│   │   └── runner.go           # Execute restic commands, capture output
│   │
│   ├── healthchecks/
│   │   └── ping.go             # Healthchecks.io HTTP client
│   │
│   ├── power/
│   │   ├── power.go            # Interface definition
│   │   ├── power_darwin.go     # macOS wake/sleep detection
│   │   └── power_windows.go    # Windows wake/sleep detection
│   │
│   ├── tray/
│   │   └── tray.go             # System tray setup, menu items, status updates
│   │
│   ├── autostart/
│   │   └── autostart.go        # Wrapper around emersion/go-autostart
│   │
│   ├── state/
│   │   └── state.go            # Backup state tracking (last run, errors, etc.)
│   │
│   ├── logging/
│   │   └── logging.go          # Log file management, retention cleanup
│   │
│   └── pushover/
│       └── pushover.go         # Pushover notification API client
│
├── assets/
│   ├── assets.go               # go:embed for tray icons
│   ├── icon_success.png        # Tray icon for success state
│   ├── icon_error.png          # Tray icon for error state
│   └── icon_running.png        # Tray icon for backup in progress
│
├── scripts/
│   └── download-restic.sh      # Script to download restic binaries for embedding
│
├── build/
│   ├── Makefile                # Build targets for both platforms
│   └── README.md               # Build instructions
│
└── .github/
    └── workflows/
        ├── ci.yml              # Test and lint on push/PR
        └── release.yml         # Build and publish on tag
```

## Configuration File Schema

```yaml
# config.yaml
version: 1

# Schedule settings
schedule:
  time: "01:00"              # 24-hour format, local time
  timezone: "America/New_York"  # Optional, defaults to system timezone

# Restic repository settings (append-only REST server)
repository:
  path: "rest:https://user:pass@backup.example.com/repo"  # REST server URL
  password_file: "/path/to/password-file"  # Or use password_command
  # password_command: "security find-generic-password -s restic -w"  # macOS keychain example

# What to backup
backup:
  paths:
    - "/Users/username/Documents"
    - "/Users/username/Pictures"
  excludes:
    - "*.tmp"
    - ".DS_Store"
    - "node_modules"
  exclude_file: ""           # Optional path to exclude patterns file

# Restic additional arguments
restic_args:
  global: []                 # Args for all commands
  backup: ["--verbose"]      # Args for backup command
  # Hardcoded flags in runner:
  #   --one-file-system    (don't cross filesystem boundaries)
  #   --exclude-caches     (skip directories with CACHEDIR.TAG)
  #   --use-fs-snapshot    (Windows only: use VSS for consistent snapshots)

# Healthchecks.io integration
healthchecks:
  enabled: true
  ping_url: "https://hc-ping.com/your-uuid-here"
  send_logs_on_failure: true

# Pushover notifications
pushover:
  enabled: true
  user_key: "your-pushover-user-key"
  api_token: "your-pushover-api-token"
  on_success: false          # Notify on successful backup
  on_failure: true           # Notify on failed backup

# Note: state.yaml and logs/ are stored in ~/neubibackup/ alongside this config
```

## State File Schema

```yaml
# ~/neubibackup/state.yaml (managed by app, not user-edited)
last_backup_attempt: "2024-01-15T10:30:00Z"
last_backup_success: "2024-01-15T10:35:00Z"
last_backup_error: ""
consecutive_failures: 0
```

## Log File Format

Each log file (`~/neubibackup/logs/YYYY-MM-DDTHH-MM-SS.log`) contains:

```
=== Backup Started: 2024-01-15T01:00:00 ===
Repository: rest:https://backup.example.com/repo
Paths: /Users/ian/Documents, /Users/ian/Pictures

[restic stdout/stderr output here]

=== Backup Completed: 2024-01-15T01:05:32 ===
Status: Success
Duration: 5m32s
```

## Key Algorithms

### Retry Logic

On backup failure, retry with exponential backoff:

```
Attempt 1 fails → wait 1 min
Attempt 2 fails → wait 2 min
Attempt 3 fails → wait 4 min
Attempt 4 fails → wait 8 min
Attempt 5 fails → stop, mark as failed
```

Rules:

- Maximum 5 attempts per scheduled backup
- Retry counter resets at next scheduled backup time (next day)
- "Backup Now" manual trigger also gets 5 retry attempts
- Tray icon stays in "running" state during retries (no separate retry icon)
- Healthchecks.io: ping /start once at beginning, only ping /fail after all attempts exhausted
- Pushover notification only sent after final failure

### Missed Schedule Detection

```
On app start OR on wake from sleep:
  1. Load state file (last_backup_success timestamp)
  2. Get today's date in configured timezone
  3. Get last successful backup date
  
  IF last_backup_success is before today AND current_time > scheduled_time:
      → Trigger backup immediately (we missed today's window)
  ELSE IF last_backup_success is before today AND current_time < scheduled_time:
      → Wait for scheduled time (backup will run on schedule)
  ELSE:
      → Already backed up today, do nothing
```

### Config File Watching

- App watches `~/neubibackup/config.yaml` for changes using filesystem notifications
- On change detected: reload config and apply immediately
- If new config is invalid: keep using old config, show error in tray menu
- No manual "Reload" menu item needed

### Concurrent Backup Prevention

- Only one backup can run at a time
- While backup is running:
  - "Backup Now" menu item shows "Backup in progress..." (disabled/grayed out)
  - Scheduled triggers are ignored if backup already running
- After backup completes, menu item re-enables

### Graceful Shutdown

- If user clicks "Quit" while backup is running:
  - Send SIGTERM/SIGINT to restic process
  - Quit immediately (don't wait for backup to finish)
  - Incomplete backup is fine - restic handles interrupted backups gracefully
  - Next run will complete the backup

### Backup Execution Flow

```
1. Update tray icon to "running" state
2. Ping healthchecks.io /start endpoint
3. Extract restic binary if needed
4. FOR attempt = 1 to 5:
   a. Execute: restic backup [global_args] -r [repository] [excludes] [backup_args] [paths]
   b. Capture stdout/stderr to log buffer
   c. IF success:
      - Ping healthchecks.io success endpoint
      - Update state file with success timestamp
      - Update tray icon to "success" state
      - EXIT loop
   d. IF failure AND attempt < 5:
      - Wait (2^attempt) minutes before retry
      - Continue to next attempt
   e. IF failure AND attempt == 5:
      - Ping healthchecks.io /fail endpoint (with logs if configured)
      - Update state file with error
      - Send Pushover notification (if enabled)
      - Update tray icon to "error" state
```

## System Tray Menu Structure

```text
[Icon: backup status indicator]
├── Status: Last backup 2 hours ago ✓
├── ─────────────────
├── Backup Now
├── ─────────────────
├── Open Config File
├── Open Logs Folder
├── ─────────────────
├── Start at Login [✓]
├── ─────────────────
├── Version 1.0.0
└── Quit
```

## Installation

### macOS

**Installer:** `.dmg` disk image with drag-to-Applications

```text
1. Download NeubiBackup-x.x.x-darwin-arm64.dmg (Apple Silicon) or -amd64.dmg (Intel)
2. Open DMG, drag NeubiBackup.app to Applications folder
3. Launch from Applications (may need to right-click → Open first time due to Gatekeeper)
4. First run creates ~/neubibackup/ and opens config.yaml in TextEdit
5. Edit config, save, then use "Backup Now" or wait for scheduled time
```

**Install location:** `/Applications/NeubiBackup.app`

### Windows

**Installer:** `.msi` Windows Installer package

```text
1. Download NeubiBackup-x.x.x-windows-amd64.msi
2. Run installer (may require admin privileges)
3. Installer creates Start Menu shortcut
4. Launch from Start Menu
5. First run creates ~/neubibackup/ and opens config.yaml in Notepad
6. Edit config, save, then use "Backup Now" or wait for scheduled time
```

**Install location:** `C:\Program Files\NeubiBackup\`

## Platform-Specific Notes

### macOS Platform Notes

- **App bundle:** Must build as `.app` bundle (not raw binary) for proper Mac experience
- Wake detection: Subscribe to `NSWorkspaceDidWakeNotification` via cgo or a helper
- Alternative: Use `caffeinate` awareness or poll system uptime
- Launch at login: Handled by `emersion/go-autostart` library
- Code signing: May need to sign binary or instruct users to allow in Security settings
- Restic binary architectures: Need both amd64 and arm64 (Intel and Apple Silicon)

### Windows Platform Notes

- Wake detection: Subscribe to `WM_POWERBROADCAST` messages for `PBT_APMRESUMEAUTOMATIC`
- Alternative: Check system uptime and compare to last known time
- Launch at login: Handled by `emersion/go-autostart` library (uses Registry)
- VSS snapshots: Runner adds `--use-fs-snapshot` flag to backup open/locked files correctly

## Build Process

### Download Restic Binaries (one-time setup)

```bash
# scripts/download-restic.sh
RESTIC_VERSION="0.18.1"

mkdir -p binaries

# macOS Intel
curl -L "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_darwin_amd64.bz2" | bunzip2 > binaries/restic-darwin-amd64

# macOS Apple Silicon  
curl -L "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_darwin_arm64.bz2" | bunzip2 > binaries/restic-darwin-arm64

# Windows
curl -L "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_windows_amd64.zip" -o restic-windows.zip
unzip -p restic-windows.zip > binaries/restic-windows-amd64.exe
rm restic-windows.zip

chmod +x binaries/restic-*
```

### Build Commands

```bash
# Build for current platform
go build -o restic-scheduler .

# Cross-compile for Windows (from macOS)
GOOS=windows GOARCH=amd64 go build -o restic-scheduler.exe .

# Cross-compile for macOS Intel (from macOS ARM)
GOOS=darwin GOARCH=amd64 go build -o restic-scheduler-intel .
```

## Dependencies (go.mod)

```
module restic-scheduler

go 1.25

require (
    github.com/emersion/go-autostart v0.0.0-20210130080809-00ed301c8e9a
    github.com/getlantern/systray v1.2.2
    gopkg.in/yaml.v3 v3.0.1
)
```

## CI/CD (GitHub Actions)

### ci.yml - Continuous Integration
Runs on every push and pull request:
- Go version: 1.25
- `go vet ./...`
- `golangci-lint` (static analysis)
- `go test ./...` (unit tests)
- Build smoke test (compile without running)

### release.yml - Release Builds

Triggered on version tags (e.g., `v1.0.0`):

1. Download restic binaries for all platforms
2. Build Go binary for each target:
   - `darwin/amd64` (macOS Intel)
   - `darwin/arm64` (macOS Apple Silicon)
   - `windows/amd64` (Windows 64-bit)
3. Create installers:
   - **macOS:** Package as `.app` bundle, then create `.dmg` disk image
   - **Windows:** Create `.msi` installer using WiX Toolset or similar
4. Create GitHub Release with installers attached:
   - `NeubiBackup-x.x.x-darwin-arm64.dmg`
   - `NeubiBackup-x.x.x-darwin-amd64.dmg`
   - `NeubiBackup-x.x.x-windows-amd64.msi`

### Build Matrix
```yaml
strategy:
  matrix:
    include:
      - os: macos-latest
        goos: darwin
        goarch: amd64
        artifact: restic-scheduler-darwin-amd64
      - os: macos-latest
        goos: darwin
        goarch: arm64
        artifact: restic-scheduler-darwin-arm64
      - os: windows-latest
        goos: windows
        goarch: amd64
        artifact: restic-scheduler-windows-amd64.exe
```

## Future Enhancements (Out of Scope for v1)

- [ ] Multiple backup profiles in one app
- [ ] GUI settings window (consider Wails if needed)
- [ ] Bandwidth throttling options
- [ ] Pre/post backup hooks (scripts)
- [ ] Backup verification (restic check) on schedule
- [ ] Restore UI
- [ ] Tray icon tooltip with next scheduled time

## Getting Started with Claude Code

After creating the project directory with this spec:

```bash
mkdir restic-scheduler
cd restic-scheduler
# Copy this file as PROJECT_SPEC.md

claude "Read PROJECT_SPEC.md and help me scaffold the initial Go project structure with go.mod and the basic files"
```

Suggested implementation order:

1. Basic Go project structure and go.mod
2. Config loading/saving
3. Restic binary embedding and extraction
4. Restic command execution
5. Scheduler logic (should we backup now?)
6. System tray integration
7. Healthchecks.io pings
8. Wake/sleep detection (platform-specific)
9. Launch at login functionality
10. Testing on both platforms
