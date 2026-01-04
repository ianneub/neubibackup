# Issue #7: Windows Non-Admin User Support - Options Comparison

## Problem Statement

NeubiBackup currently requires administrator privileges on Windows. Users without admin rights cannot run the application. The goal is to allow non-admin users to backup their own files.

---

## Option 1: Adaptive Single Build (Minimal Change)

**Approach:** Keep current tray app architecture, but detect admin status at runtime and gracefully degrade features.

### How It Works

- Change Windows manifest from `requireAdministrator` to `asInvoker`
- Add `IsAdmin()` detection function
- Conditionally enable/disable features based on privileges

### Changes Required

| File | Change |
| ---- | ------ |
| `winres/winres.json` | Change manifest to `asInvoker` |
| `internal/admin/admin_windows.go` | **NEW** - Admin detection |
| `internal/restic/runner.go` | Skip `--use-fs-snapshot` when not admin |
| `internal/autostart/autostart_windows.go` | Remove `/RL HIGHEST` when not admin |
| `internal/updater/updater.go` | Skip updates if can't write to install location |

### Feature Comparison

| Feature | Admin Mode | Non-Admin Mode |
| ------- | ---------- | -------------- |
| VSS snapshots | ✅ Yes | ❌ No (files in use may fail) |
| Backup user files | ✅ Yes | ✅ Yes |
| Backup system files | ✅ Yes | ❌ Permission denied |
| Auto-updates | ✅ Yes | ⚠️ Disabled if in Program Files |
| Autostart | ✅ Elevated task | ✅ Normal task |

### Pros

- Minimal code changes (~5 files)
- No architectural changes
- Backward compatible
- Quick to implement

### Cons

- Windows-only solution
- User must still run the app themselves
- No VSS = files in use may fail to backup
- Different experience for admin vs non-admin users

---

## Option 2: Windows Service Only

**Approach:** Create a Windows Service that runs backups in the background, installed once by admin.

### How It Works

- New service binary runs as SYSTEM
- Config stored in `C:\ProgramData\NeubiBackup\`
- Runs continuously, even when no user logged in
- No tray UI (logs only)

### Changes Required

| File | Change |
| ---- | ------ |
| `cmd/service/main.go` | **NEW** - Service entry point |
| `internal/service/service_windows.go` | **NEW** - Windows service wrapper |
| `internal/config/paths.go` | Add `GetSystemDataDir()` |
| `internal/app/app.go` | Refactor for headless mode |
| `internal/scheduler/scheduler.go` | Skip user idle/WiFi checks |

### Pros

- Non-admin users don't need to run anything
- VSS works (SYSTEM has full access)
- Backups run even when no user logged in
- Multi-user support (single config backs up configured paths)

### Cons

- Windows-only solution
- Different architecture than macOS version
- No user-facing UI (status via logs only)
- Admin must configure backup paths

---

## Option 3: Unified Service Architecture (Recommended)

**Approach:** Rebuild with service-first architecture on all platforms. Two processes: background service + tray UI.

### How It Works

```text
┌─────────────────────────────────────────────────────────────┐
│                    Background Service                        │
│  (Windows Service / macOS LaunchDaemon / Linux systemd)     │
│                                                              │
│  - Scheduler runs every 15 minutes                          │
│  - Executes backups via restic                              │
│  - Exposes IPC for status/commands                          │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ IPC (Unix socket / Named pipe)
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                      Tray Application                        │
│  (Runs per-user, no admin required)                         │
│                                                              │
│  - View backup status                                        │
│  - Trigger "Backup Now"                                      │
│  - Open logs/config                                          │
└─────────────────────────────────────────────────────────────┘
```

### Key Technology

- **`github.com/kardianos/service`** - Cross-platform service management
  - Windows → Windows Service Manager
  - macOS → launchd (LaunchDaemon)
  - Linux → systemd/Upstart/SysV

#### ⚠️ kardianos/service Risk Assessment

**Repository Status:** 4,800+ stars, 137 open issues, 25 open PRs. Moderately maintained but with significant backlog.

**Critical Issues Identified:**

| Issue | Severity | Impact | Workaround |
|-------|----------|--------|------------|
| [#354](https://github.com/kardianos/service/issues/354) - v1.2.2 Windows regression | **HIGH** | Services spawning child processes (like restic) may fail to start | Pin to v1.2.1 |
| [#380](https://github.com/kardianos/service/issues/380) - Non-SYSTEM account fails | **HIGH** | Service won't start if configured to run as non-SYSTEM user | Pin to commit `804642397ef` |
| [#398](https://github.com/kardianos/service/issues/398) - macOS Big Sur+ install failures | **MEDIUM** | "Load failed: 5: Input/output error" during service installation | See below |
| [#368](https://github.com/kardianos/service/issues/368) - macOS 12+ syslog broken | **LOW** | System logging doesn't work | Use file-based logging (already implemented) |

**macOS "Load failed: 5" Root Cause:**

This is **NOT Apple Silicon specific** — it affects all Macs running Big Sur (11.0) or later. The root cause is that kardianos/service uses **deprecated launchctl commands**:

| Current (Legacy) | Should Use (Modern) |
|------------------|---------------------|
| `launchctl load` | `launchctl bootstrap` |
| `launchctl unload` | `launchctl bootout` |

Apple deprecated `load`/`unload` in macOS 10.10, and they've become increasingly unreliable in Big Sur+. The library has an [open PR #287](https://github.com/kardianos/service/pull/287) to fix macOS issues, but it's been **unmerged since July 2021**.

**Common triggers for the error:**
- Plist file permissions (must be `chmod 644`, owned by `root:wheel`)
- GUI session requirement (fails via SSH)
- Service already loaded (need unload first)

**Mitigation Options:**
1. **Pin version carefully** — Use v1.2.1 or specific commits
2. **Add retry/unload logic** — Unload before load on install
3. **Implement native macOS support** — Write launchd integration using modern `bootstrap`/`bootout` commands, use kardianos/service only for Windows/Linux
4. **Provide manual fallback** — Document workarounds for affected users

**Alternative Libraries Evaluated:**

| Library | Status | Windows | macOS | Notes |
|---------|--------|---------|-------|-------|
| [kardianos/service](https://github.com/kardianos/service) | Moderate maintenance | ✅ | ⚠️ Issues | Best cross-platform option despite issues |
| [takama/daemon](https://github.com/takama/daemon) | Last release 2020 | ✅ | ✅ | Similar/worse maintenance |
| [sevlyar/go-daemon](https://github.com/sevlyar/go-daemon) | Maintenance mode | ❌ | ✅ | No Windows support |
| [coreos/go-systemd](https://pkg.go.dev/github.com/coreos/go-systemd/v22/daemon) | Well maintained | ❌ | ❌ | Linux systemd only |

**Conclusion:** kardianos/service remains the best option for cross-platform service management, but requires careful version pinning and may need custom macOS launchd code for reliability on Big Sur+.

### Changes Required

| File | Change |
| ---- | ------ |
| `cmd/neubibackup-service/main.go` | **NEW** - Service entry point |
| `internal/service/service.go` | **NEW** - Service implementation |
| `internal/ipc/server.go` | **NEW** - IPC server (service side) |
| `internal/ipc/client.go` | **NEW** - IPC client (tray side) |
| `internal/ipc/protocol.go` | **NEW** - Shared message types |
| `internal/ipc/transport_unix.go` | **NEW** - Unix socket transport |
| `internal/ipc/transport_windows.go` | **NEW** - Named pipe transport |
| `internal/config/paths.go` | Add `GetSystemDataDir()` |
| `internal/app/app.go` | Refactor for headless mode |
| `internal/scheduler/scheduler.go` | Skip user checks in service mode |
| `main.go` | Add IPC client, fallback to direct mode |
| `go.mod` | Add `github.com/kardianos/service` |

### Pros

- **Unified architecture** across Windows, macOS, and Linux
- Non-admin users get full tray UI experience
- Service runs even when no user logged in
- VSS works on Windows (service has full access)
- Backward compatible (tray works standalone if service not installed)
- Enables future features (remote management, multi-user)

### Cons

- Largest implementation effort (~12 new/modified files)
- Two binaries to build and distribute
- More complex deployment (service install + tray app)
- IPC adds complexity

---

## Summary Comparison

| Aspect | Option 1: Adaptive | Option 2: Windows Service | Option 3: Unified Service |
| ------ | ------------------ | ------------------------- | ------------------------- |
| **Effort** | Small (~5 files) | Medium (~7 files) | Large (~12+ files) |
| **Platforms** | Windows only | Windows only | All platforms |
| **Architecture** | Keep current | New (Windows only) | New (unified) |
| **Non-admin UX** | Degraded features | No UI (service only) | Full tray UI |
| **VSS support** | ❌ When non-admin | ✅ Always | ✅ Always |
| **Runs without login** | ❌ No | ✅ Yes | ✅ Yes |
| **Backward compatible** | ✅ Yes | ❌ No | ✅ Yes |
| **Future-proof** | ❌ Limited | ⚠️ Windows only | ✅ Yes |
| **Dependency risk** | ✅ None | ⚠️ Windows API only | ⚠️ kardianos/service issues |

---

## Recommendation

**Option 3 (Unified Service Architecture)** is still recommended, but with caveats:

### Why Option 3

1. **Consistency** - Same architecture on all platforms
2. **Better UX** - Non-admin users get full tray experience
3. **More capable** - Backups run 24/7, VSS always works
4. **Future-proof** - Enables remote management, multi-user, etc.
5. **Maintainable** - One codebase pattern instead of platform-specific hacks

### Implementation Strategy (Risk Mitigation)

Given the kardianos/service issues identified above, the recommended implementation approach is:

1. **Pin kardianos/service to v1.2.1** to avoid the Windows child process regression (#354)
2. **Use kardianos/service for Windows and Linux** where it's most stable
3. **Consider native launchd implementation for macOS** using modern `launchctl bootstrap`/`bootout` commands to avoid Big Sur+ issues
4. **Implement robust error handling** with retry logic and clear user messaging for service installation failures
5. **Provide fallback mode** where tray app works standalone if service installation fails

### Alternative: Phased Approach

If the kardianos/service risks are concerning, consider a phased rollout:

- **Phase 1:** Implement Option 1 (Adaptive) for quick Windows non-admin support
- **Phase 2:** Add Windows Service (Option 2) for users who want background operation
- **Phase 3:** Extend to unified architecture once kardianos/service stabilizes or native implementations are complete

The extra implementation effort for Option 3 pays off in reduced long-term maintenance and a better product, but the dependency risks should be actively managed.
