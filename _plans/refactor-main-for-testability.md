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

### Phase 1: Extract Thread-Safe State Holders

**Files to create:**
- `internal/app/backup_state.go` - Extract `backupMu`, `backupRunning`, `backupCancel`, `backupProgress`
- `internal/app/update_state.go` - Extract `updateMu`, `updateInProgress`, `availableVersion`

**Key types:**
```go
type BackupState struct {
    mu       sync.RWMutex
    running  bool
    cancel   context.CancelFunc
    progress *restic.BackupProgress
}

type UpdateState struct {
    mu         sync.RWMutex
    inProgress bool
    available  string
}
```

**Tests:** Thread-safety with concurrent access, state transitions.

---

### Phase 2: Extract Backup Orchestration

**Files to create:**
- `internal/backup/orchestrator.go`
- `internal/backup/notifier.go`

**Key interfaces:**
```go
type Notifier interface {
    NotifyStart() error
    NotifySuccess() error
    NotifyFailure(err error, logs string) error
}

type TailscaleProvider interface {
    Connect(ctx context.Context) (proxyAddr string, err error)
    Disconnect() error
}

type StateRecorder interface {
    RecordSuccess()
    RecordFailure(err error)
    Save() error
}
```

**Key type:**
```go
type Orchestrator struct {
    cfg       *config.Config
    notifier  Notifier
    tailscale TailscaleProvider
    state     StateRecorder
    progress  func(*restic.BackupProgress)
}

func (o *Orchestrator) Run(ctx context.Context) Result
```

**Migration:** Extract `runBackup()` (175 lines) logic into `Orchestrator.Run()`.

**Tests:** Success path, Tailscale failure, backup failure with retries, cancellation.

---

### Phase 3: Expand Tray Package

**Files to create:**
- `internal/tray/icon.go` - Icon state determination
- `internal/tray/menu.go` - Menu setup and event handling

**Key interfaces:**
```go
type MenuCallbacks interface {
    OnBackupNow()
    OnStopBackup()
    OnToggleAutostart()
    OnOpenConfig()
    OnOpenLogs()
    OnOpenAppLog()
    OnUpdateClick()
    OnQuit()
}

type MenuState interface {
    IsConfigured() bool
    IsBackupRunning() bool
    IsAutostartEnabled() bool
    BackupProgress() *restic.BackupProgress
    State() *state.State
    AvailableVersion() string
    Version() string
}
```

**Key type:**
```go
type Menu struct {
    status, backupNow, stopBackup, autostart, updateStatus *systray.MenuItem
    callbacks MenuCallbacks
    state     MenuState
}

func NewMenu(callbacks MenuCallbacks, state MenuState, version string) *Menu
func (m *Menu) StartEventLoop()
func (m *Menu) UpdateStatus()
```

**Migration:** Extract `setupMenu()` (111 lines) to `tray.NewMenu()`.

**Tests:** Icon state determination, status formatting (existing tests cover some).

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

| File | Purpose |
|------|---------|
| `internal/app/app.go` | Main App struct and lifecycle |
| `internal/app/app_test.go` | App unit tests |
| `internal/app/backup_state.go` | Thread-safe backup state |
| `internal/app/backup_state_test.go` | Backup state tests |
| `internal/app/update_state.go` | Thread-safe update state |
| `internal/app/update_state_test.go` | Update state tests |
| `internal/backup/orchestrator.go` | Backup orchestration |
| `internal/backup/orchestrator_test.go` | Orchestrator tests |
| `internal/backup/notifier.go` | Notification composition |
| `internal/backup/notifier_test.go` | Notifier tests |
| `internal/tray/menu.go` | Menu setup and events |
| `internal/tray/menu_test.go` | Menu tests |
| `internal/tray/icon.go` | Icon state logic |
| `internal/tray/icon_test.go` | Icon tests |
| `internal/updater/orchestrator.go` | Auto-update orchestration |
| `internal/updater/orchestrator_test.go` | Update orchestrator tests |

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
