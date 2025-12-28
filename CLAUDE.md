# NeubiBackup

Cross-platform backup scheduler wrapping restic.

## Tech Stack

- Go 1.25+ with `github.com/getlantern/systray` for tray icon
- Embedded restic 0.18.1 binary via `//go:embed`
- YAML config via `gopkg.in/yaml.v3`
- `github.com/emersion/go-autostart` for launch at login
- `github.com/fsnotify/fsnotify` for config file watching
- `github.com/creativeprojects/go-selfupdate` for automatic updates
- `tailscale.com` for Tailscale network integration

## Key Features

- Daily scheduled backups with configurable time
- Missed schedule detection (wake from sleep → check if backup needed)
- Retry logic: 5 attempts with exponential backoff (1, 2, 4, 8, 16 min)
- healthchecks.io integration (ping start/success/fail)
- Pushover notifications on failure
- Auto-reload config on file change
- Automatic updates (background download/install every 24 hours)
- Tailscale integration for accessing private restic repositories
- macOS Full Disk Access detection and prompt on first run

## Data Directory

All files stored in `~/neubibackup/`:

- `config.yaml` - User configuration
- `state.yaml` - App-managed state (last backup time, errors)
- `app.log` - Application log (truncated at 1MB, useful for debugging)
- `logs/` - Last 25 backup logs (YYYY-MM-DDTHH-MM-SS.log)

## Project Structure

```text
internal/
├── config/      # Load/save YAML config, default paths, FDA detection
├── scheduler/   # Schedule tracking, "should run now?" logic
├── restic/      # Binary embedding, extraction, command execution
├── healthchecks/# Healthchecks.io HTTP client
├── power/       # Platform-specific wake/sleep detection
├── tray/        # System tray setup, menu items, status updates
├── autostart/   # Wrapper around go-autostart
├── state/       # Backup state and update tracking
├── logging/     # Log file management, retention cleanup
├── pushover/    # Pushover notification API client
├── tailscale/   # Tailscale network integration via tsnet
└── updater/     # Automatic update checking and installation
```

## Build Commands

```bash
# Download restic binaries first
./scripts/download-restic.sh

# Build for current platform
go build -o neubibackup .

# Cross-compile
GOOS=windows GOARCH=amd64 go build -o neubibackup.exe .
GOOS=darwin GOARCH=arm64 go build -o neubibackup-arm64 .
```

## Platform Notes

### macOS

- Build as `.app` bundle for proper Mac experience
- Wake detection via `NSWorkspaceDidWakeNotification`
- Open files with `open` command
- Full Disk Access detection on first run with prompt to open System Settings

### Windows

- Runs with admin privileges (embedded manifest requests `requireAdministrator`)
- Admin required for VSS snapshots (`--use-fs-snapshot`) and updates to Program Files
- Wake detection via `WM_POWERBROADCAST` / `PBT_APMRESUMEAUTOMATIC`
- `--use-fs-snapshot` flag added automatically for VSS snapshots
- Open files with default application via `rundll32`
- Update artifact cleanup after automatic updates
- Uses `go-winres` in CI to embed Windows manifest with admin requirement

## Testing (REQUIRED)

**Tests are mandatory for all new code.** When planning any feature or bug fix:

1. **Include tests in the plan** - Every implementation plan must include a testing section
2. **Write tests alongside code** - Tests should be written as part of the same task, not as a follow-up
3. **No code is complete without tests** - A feature is not done until its tests are written and passing

### Test Guidelines

- Write unit tests for new functions and logic
- Prefer real implementations over mocks when possible
- Use table-driven tests for testing multiple cases
- Test files go next to the code: `foo.go` → `foo_test.go`
- Run `go test ./...` to verify all tests pass before committing

### What to Test

- New structs and their methods
- New exported functions
- Error handling paths
- Edge cases (empty inputs, nil values, boundary conditions)
- Concurrent access if applicable

## Versioning

This project uses [Semantic Versioning (semver)](https://semver.org/). All version numbers must follow the format `MAJOR.MINOR.PATCH`:

- **MAJOR**: Incompatible API/config changes
- **MINOR**: New functionality in a backward-compatible manner
- **PATCH**: Backward-compatible bug fixes

## Documentation

When adding new features or making user-facing changes, always update the README.md file to reflect those changes. This includes:

- New features in the Features section
- New menu items in the Tray Menu Options section
- New configuration options with examples
- Troubleshooting information for common issues
