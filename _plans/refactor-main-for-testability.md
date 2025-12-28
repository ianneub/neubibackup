# Refactoring main.go for Testability

## Goal
Refactor main.go (1,009 lines, 26 global variables) into testable internal packages, reducing main.go to ~50 lines.

## New Package Structure

```
internal/
├── app/                    # NEW: Application lifecycle and state
│   ├── app.go              # App struct, lifecycle methods
│   ├── app_test.go
│   ├── backup_state.go     # Thread-safe backup state
│   ├── backup_state_test.go
│   ├── update_state.go     # Thread-safe update state
│   └── update_state_test.go
├── backup/                 # NEW: Backup orchestration
│   ├── orchestrator.go     # Coordinates backup execution
│   ├── orchestrator_test.go
│   ├── notifier.go         # Combines healthchecks + pushover
│   └── notifier_test.go
├── tray/                   # EXPANDED: Menu setup + callbacks
│   ├── status.go           # Existing (keep)
│   ├── status_test.go      # Existing (keep)
│   ├── menu.go             # NEW: Menu builder and event loop
│   ├── menu_test.go
│   ├── icon.go             # NEW: Icon state management
│   └── icon_test.go
├── updater/                # EXPANDED
│   ├── updater.go          # Existing
│   ├── orchestrator.go     # NEW: Auto-update logic
│   └── orchestrator_test.go
└── ... (existing packages unchanged)
```

## Implementation Phases

### Phase 1: Extract Thread-Safe State Holders - COMPLETED

**Status:** Completed in commit `a9cb50f`

**Files created:**
- `internal/app/backup_state.go` - Thread-safe backup state with `BackupState` struct
- `internal/app/backup_state_test.go` - Comprehensive tests including concurrency
- `internal/app/update_state.go` - Thread-safe update state with `UpdateState` struct
- `internal/app/update_state_test.go` - Comprehensive tests including concurrency

**Files modified:**
- `main.go` - Replaced global mutex variables with state types
- `main_test.go` - Updated tests to use new state types
- `internal/updater/updater.go` - Added semver validation to prevent panic on dev builds
- `internal/logging/applog.go` - Added `Sync()` method and sync before close for reliable logging

**Implemented types:**
```go
type BackupState struct {
    mu       sync.RWMutex
    running  bool
    cancel   context.CancelFunc
    progress *restic.BackupProgress
}

// Methods: NewBackupState(), IsRunning(), SetRunning(), GetProgress(),
// SetProgress(), StartBackup(), StopBackup(), Reset(), GetCancel()

type UpdateState struct {
    mu               sync.RWMutex
    inProgress       bool
    availableVersion string
}

// Methods: NewUpdateState(), IsInProgress(), SetInProgress(),
// GetAvailableVersion(), SetAvailableVersion(), TryStartUpdate(),
// FinishUpdate(), HasUpdate(), ClearAvailableVersion()
```

**Additional fixes included:**
- Fixed race conditions in `setupMenu()`, `updateIcon()`, `reloadConfig()` for unprotected `backupRunning` reads
- Fixed race condition for `availableVersion` access
- Added `isValidSemver()` check in updater to skip update checks for dev builds (prevents panic)
- Added `Sync()` to rotatingWriter to ensure logs are flushed before app exit

**Tests:** All tests pass with `-race` flag. Coverage includes thread-safety with concurrent access, state transitions, and edge cases.

---

### Phase 2: Extract Backup Orchestration - COMPLETED

**Status:** Completed

**Files created:**
- `internal/backup/notifier.go` - Notifier interface with CompositeNotifier and NullNotifier implementations
- `internal/backup/notifier_test.go` - Comprehensive notifier tests
- `internal/backup/orchestrator.go` - Orchestrator struct coordinating backup execution
- `internal/backup/orchestrator_test.go` - Orchestrator tests (unit tests, no restic execution)
- `internal/backup/tailscale_adapter.go` - TailscaleProvider adapter for tailscale.Manager

**Files modified:**
- `main.go` - Replaced 170-line `runBackup()` with 80-line version using orchestrator
- Removed unused `initTailscale()` function
- Removed unused `tailscaleMgr` global variable
- Removed unused imports (`io`, `errors`, `healthchecks`, `pushover`)

**Implemented interfaces:**
```go
type Notifier interface {
    NotifyStart() error
    NotifySuccess(message string) error
    NotifyFailure(errMsg string, logs string) error
}

type TailscaleProvider interface {
    Connect(ctx context.Context) (proxyAddr string, err error)
    Disconnect() error
}
```

**Key types:**
```go
type Orchestrator struct {
    cfg        *config.Config
    state      *state.State
    notifier   Notifier
    tailscale  TailscaleProvider
    onProgress ProgressCallback
}

type Result struct {
    Success   bool
    Cancelled bool
    Error     error
    LogPath   string
}

func (o *Orchestrator) Run(ctx context.Context) Result
```

**Additional implementations:**
- `CompositeNotifier` - Combines healthchecks and pushover notifications
- `NullNotifier` - No-op implementation for testing
- `TailscaleAdapter` - Adapts tailscale.Manager to TailscaleProvider interface
- Functional options pattern: `WithNotifier()`, `WithTailscale()`, `WithProgressCallback()`

**Tests:** All tests pass with `-race` flag

---

### Phase 3: Expand Tray Package - COMPLETED

**Status:** Completed

**Files created:**
- `internal/tray/icon.go` (54 lines) - Icon state determination with `DetermineIconState()` and `GetIconBytes()`
- `internal/tray/icon_test.go` (126 lines) - Table-driven tests for icon state logic
- `internal/tray/menu.go` (258 lines) - Menu struct with configuration and event handling
- `internal/tray/menu_test.go` (242 lines) - Mock-based tests for menu configuration

**Implemented interfaces:**
```go
type BackupStateProvider interface {
    IsRunning() bool
    GetProgress() *restic.BackupProgress
}

type UpdateStateProvider interface {
    HasUpdate() bool
    GetAvailableVersion() string
}

type AutostartProvider interface {
    IsEnabled() bool
    Toggle() error
}
```

**Key types:**
```go
type IconState int // IconStateIdle, IconStateSuccess, IconStateError, IconStateRunning

type MenuConfig struct {
    Version, ResticVersion string
    AppState               func() *state.State
    BackupState            BackupStateProvider
    UpdateState            UpdateStateProvider
    IsConfigured           func() bool
    Autostart              AutostartProvider
    OnBackupNow, OnStopBackup, OnOpenConfig, OnOpenLogs, OnOpenAppLog, OnUpdateClick, OnQuit func()
}

type Menu struct { /* internal menu item refs */ }

func NewMenu(cfg MenuConfig) *Menu
func (m *Menu) UpdateStatus()
func (m *Menu) SetBackupRunning(running bool)
func (m *Menu) SetUpdateStatus(text string, enabled bool)
func (m *Menu) RefreshOnConfigChange()
```

**Migration completed:**
- Replaced `setupMenu()` with `tray.NewMenu(MenuConfig{...})`
- Replaced `updateIcon()` with `tray.DetermineIconState()` and `tray.GetIconBytes()`
- Replaced `updateStatus()` with `menu.UpdateStatus()`
- Replaced `toggleAutostart()` - now handled inside Menu
- Replaced all direct `mUpdateStatus` references with `menu.SetUpdateStatus()`
- Added helper callback functions: `openConfig()`, `openLogs()`, `openAppLog()`, `handleUpdateClick()`

**Results:**
- main.go reduced from 836 lines to 726 lines (-110 lines)
- New tray package files: 680 lines of code + tests

**Tests:** All tests pass with `-race` flag.

---

### Phase 4: Create App Package

**Files to create:**
- `internal/app/app.go`

**Key type:**
```go
type App struct {
    // Config & state
    cfg      *config.Config
    state    *state.State
    version  string

    // Managers
    sched        *scheduler.Scheduler
    autostartMgr *autostart.Manager
    powerWatcher *power.Watcher
    tailscaleMgr *tailscale.Manager
    updater      *updater.Updater

    // Internal state
    backupState *BackupState
    updateState *UpdateState

    // Context
    ctx    context.Context
    cancel context.CancelFunc

    // UI
    menu *tray.Menu
}

func New(version string, opts ...Option) (*App, error)
func (a *App) Initialize() error
func (a *App) Run() error
func (a *App) Shutdown()

// Backup methods
func (a *App) TriggerBackup()
func (a *App) StopBackup()
func (a *App) IsBackupRunning() bool

// Config methods
func (a *App) ReloadConfig() error

// Implements MenuCallbacks and MenuState interfaces
```

**Migration:**
- Move `onReady()` logic to `App.Initialize()`
- Move `triggerBackupNow()`, `stopBackup()` to App methods
- Move `reloadConfig()` to App
- Move `toggleAutostart()` to App

**Tests:** Initialization, shutdown, backup triggering, config reload.

---

### Phase 5: Extract Update Orchestration

**Files to create:**
- `internal/updater/orchestrator.go`

**Key type:**
```go
type UpdateOrchestrator struct {
    updater       *Updater
    state         *state.State
    backupChecker interface{ IsBackupRunning() bool }
    updateState   *app.UpdateState
}

func (o *UpdateOrchestrator) CheckForUpdates(ctx context.Context) (string, bool, error)
func (o *UpdateOrchestrator) AutoUpdate(ctx context.Context, version string) error
```

**Migration:** Extract `attemptAutoUpdate()` (87 lines), `checkForUpdates()`.

**Tests:** Update checking, waiting for backup to complete, state transitions.

---

### Phase 6: Final main.go

Target main.go (~50 lines):
```go
package main

import (
    "log"
    "neubibackup/internal/app"
    "github.com/getlantern/systray"
)

var version = "dev"
var application *app.App

func main() {
    systray.Run(onReady, onExit)
}

func onReady() {
    var err error
    application, err = app.New(version)
    if err != nil {
        log.Fatalf("Failed to create app: %v", err)
    }
    if err := application.Initialize(); err != nil {
        log.Fatalf("Failed to initialize: %v", err)
    }
    if err := application.Run(); err != nil {
        log.Fatalf("Failed to run: %v", err)
    }
}

func onExit() {
    if application != nil {
        application.Shutdown()
    }
}
```

---

## Files to Modify

| File | Action |
|------|--------|
| `main.go` | Refactor to ~50 lines |
| `internal/tray/status.go` | Keep as-is |
| `internal/updater/updater.go` | Keep as-is |

## New Files to Create

| File | Purpose | Status |
|------|---------|--------|
| `internal/app/app.go` | Main App struct and lifecycle | Pending (Phase 4) |
| `internal/app/app_test.go` | App unit tests | Pending (Phase 4) |
| `internal/app/backup_state.go` | Thread-safe backup state | ✅ Created |
| `internal/app/backup_state_test.go` | Backup state tests | ✅ Created |
| `internal/app/update_state.go` | Thread-safe update state | ✅ Created |
| `internal/app/update_state_test.go` | Update state tests | ✅ Created |
| `internal/backup/orchestrator.go` | Backup orchestration | ✅ Created |
| `internal/backup/orchestrator_test.go` | Orchestrator tests | ✅ Created |
| `internal/backup/notifier.go` | Notification composition | ✅ Created |
| `internal/backup/notifier_test.go` | Notifier tests | ✅ Created |
| `internal/backup/tailscale_adapter.go` | TailscaleProvider adapter | ✅ Created |
| `internal/tray/menu.go` | Menu setup and events | ✅ Created |
| `internal/tray/menu_test.go` | Menu tests | ✅ Created |
| `internal/tray/icon.go` | Icon state logic | ✅ Created |
| `internal/tray/icon_test.go` | Icon tests | ✅ Created |
| `internal/updater/orchestrator.go` | Auto-update orchestration | Pending (Phase 5) |
| `internal/updater/orchestrator_test.go` | Update orchestrator tests | Pending (Phase 5) |

## Testing Strategy

- **Pattern:** Table-driven tests (follow existing scheduler_test.go pattern)
- **Approach:** Real implementations over mocks where possible
- **Coverage target:** 80%+ for new packages
- **Race detection:** Run all tests with `-race` flag

## Risks

| Risk | Mitigation |
|------|------------|
| Breaking systray callbacks | Keep onReady/onExit in main.go, delegate to App |
| Thread safety regressions | Comprehensive mutex tests, CI with `-race` |
| Platform-specific breakage | Test on macOS and Windows before merge |

## Success Criteria

1. main.go reduced to ~50 lines
2. All new packages have >80% test coverage
3. All existing tests pass
4. No goroutine leaks (verified with `-race`)
5. Application behavior unchanged
