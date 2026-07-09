# Race conditions between backup and auto-update at startup

## Summary

There are race conditions between the backup scheduler and the auto-update system at startup that can lead to unexpected behavior.

## Issue 1: Race Condition at Startup

At startup, both the scheduler and update checker start nearly simultaneously (in `app.Run()`):

```go
// Line 212-214: Scheduler starts first
a.initScheduler()

// Line 229-230: Update check starts second
go a.updateOrch.CheckIfNeeded(a.ctx)
```

The problem is that there are **two separate `running` flags**:
- `scheduler.running` (scheduler.go:28) - set when scheduler decides to trigger
- `backupState.running` (backup_state.go:19) - what the updater actually checks via `IsRunning()`

The backup trigger flow has multiple goroutine hops before `backupState.running` is set:
1. `checkAndTrigger()` sets `s.running = true` and calls `go s.onBackup()`
2. `s.onBackup()` → `TriggerBackup()` calls `go a.runBackup()`
3. `runBackup()` finally calls `backupState.StartBackup()` which sets `backupState.running = true`

During this window, the updater could check `backupState.IsRunning()`, see `false`, and proceed to download/apply an update while a backup is about to start.

## Issue 2: Backups Longer Than 2 Hours Block Updates

In `AttemptAutoUpdate()` (orchestrator.go:127-148), if a backup is running, the updater waits up to 2 hours:

```go
maxWait := 2 * time.Hour

for {
    if !o.backupChecker.IsRunning() {
        break
    }
    if time.Since(waitStart) > maxWait {
        slog.Info("Auto-update: gave up waiting for backup to complete")
        return  // Update abandoned, not retried
    }
    // ... waits 30 seconds and checks again
}
```

If a backup takes longer than 2 hours, the update is **silently abandoned** until the next 24-hour check cycle.

## Issue 3: Hung Backup Prevents All Auto-Updates

If there's a bug that causes a backup to hang indefinitely (never completes, `backupState.running` stays `true`), then:

1. Every 24-hour update check finds an update available
2. `AttemptAutoUpdate()` waits 2 hours, then gives up
3. The update is never applied
4. This cycle repeats indefinitely

A critical security update could never be applied if the backup system has a bug.

## Suggested Fixes

### For Issue 1 (Race Condition)
- Set `backupState.running = true` synchronously in `checkAndTrigger()` before spawning the goroutine
- Or have the updater check both `scheduler.IsRunning()` and `backupState.IsRunning()`
- Or add a "backup pending" state that's set before the goroutine chain starts

### For Issues 2 & 3 (Long/Hung Backups)
- Persist the "update available" state and retry the auto-update on next app startup
- Add a maximum backup duration timeout that force-resets the backup state
- Consider allowing updates to proceed if backup has been "running" for an unreasonably long time (e.g., 4+ hours)
- Add monitoring/alerting for backups that exceed expected duration

## Related Code Locations

- `internal/app/app.go:198-246` - `Run()` method with startup sequence
- `internal/scheduler/scheduler.go:121-144` - `checkAndTrigger()`
- `internal/app/backup_state.go:79-93` - `StartBackup()`
- `internal/updater/orchestrator.go:119-192` - `AttemptAutoUpdate()`
