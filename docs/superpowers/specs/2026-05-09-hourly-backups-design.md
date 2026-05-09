# Configurable backup interval (hourly + sub-daily backups)

**Date:** 2026-05-09
**Status:** Draft for review

## Summary

Replace the fixed daily-at-`schedule.time` scheduling with a single, configurable `schedule.interval` field that accepts a Go duration string (e.g. `"1h"`, `"6h"`, `"24h"`). When `interval` is unset, the scheduler defaults to `"24h"` so legacy installs keep running on roughly the same cadence. The existing `schedule.time` field becomes a deprecated no-op kept only so legacy YAML still parses.

## Motivation

Users want backups more frequent than once per day (notably hourly). Today the scheduler only supports a single daily run anchored at a wall-clock time. Adding a second mode would mean carrying two scheduling code paths forever; collapsing onto one interval-based path keeps the codebase simple while unlocking hourly, every-6-hour, etc.

## Goals

- Let users configure backup cadence via a single `schedule.interval` Go duration.
- Default to `24h` so installs without the field continue working unchanged in spirit.
- Remove the daily-anchor scheduling code path entirely; one mechanism, one code path.
- Preserve existing pre-flight gates (battery, allowed SSIDs, idle limit) and manual-trigger behavior.

## Non-goals

- No cron-style "run at 01:00 every day" anchoring. Cadence is interval-from-last-success.
- No per-day or per-day-of-week schedules.
- No sub-15-minute intervals (the scheduler ticker is 15m and that sets the practical floor).
- No automatic config migration that rewrites the user's `config.yaml`. The `time` field is silently ignored with a deprecation log.

## Design

### Config schema

`internal/config/config.go` — `ScheduleConfig` gains an `Interval` field:

```go
type ScheduleConfig struct {
    Interval      string   `yaml:"interval"`        // NEW. Go duration ("1h", "6h", "24h"). Default: "24h".
    Time          string   `yaml:"time"`            // DEPRECATED. Ignored at runtime. Kept for YAML compat.
    Timezone      string   `yaml:"timezone"`
    SkipOnBattery bool     `yaml:"skip_on_battery"`
    AllowedSSIDs  []string `yaml:"allowed_ssids"`
}
```

User-facing yaml (new installs):

```yaml
schedule:
  interval: "24h"            # Go duration. Min 15m. Examples: "30m", "1h", "6h", "24h"
  # timezone: ""             # Optional, only used for log timestamps and SkipOnBattery messaging
  # skip_on_battery: false
  # allowed_ssids: []
```

Legacy yaml (still parses, `time` is ignored with warning):

```yaml
schedule:
  time: "01:00"              # deprecated, ignored
```

### Validation (`config.Validate`)

Replace the `if c.Schedule.Time == ""` requirement:

- If `Schedule.Interval` is empty, treat as `"24h"` (no error).
- If `Schedule.Interval` is set:
  - Must parse via `time.ParseDuration`. Bad parse → `fmt.Errorf("schedule.interval is not a valid Go duration: %w", err)`.
  - Must be `>= 15 * time.Minute`. Smaller → `fmt.Errorf("schedule.interval must be at least 15m (got %s)", d)`.
- `Schedule.Time` is no longer required and is no longer used by the scheduler. If `Schedule.Time != ""`, log once at INFO level: `"schedule.time is deprecated and ignored; backups now use schedule.interval (default 24h)"`. The warning is fired from config load, not from `Validate`, so it surfaces even on first run.

Add a small helper to centralize default + parse:

```go
// IntervalOrDefault returns the parsed interval or the 24h default. Assumes Validate has succeeded.
func (s ScheduleConfig) IntervalOrDefault() time.Duration {
    if s.Interval == "" {
        return 24 * time.Hour
    }
    d, _ := time.ParseDuration(s.Interval)
    return d
}
```

### Scheduler (`internal/scheduler/scheduler.go`)

Cache the parsed interval on the struct so we don't re-parse every 15-minute tick:

```go
type Scheduler struct {
    config   *config.Config
    state    *state.State
    onBackup BackupFunc
    location *time.Location
    interval time.Duration   // NEW. Cached from config.Schedule.IntervalOrDefault().
    mu       sync.Mutex
    running  bool
}
```

`New` and `UpdateConfig` set `s.interval = cfg.Schedule.IntervalOrDefault()` under the lock.

Replace `isBackupDue` with a single interval-based check:

```go
// isBackupDue returns true if at least s.interval has elapsed since the last
// successful backup, or if no successful backup has ever happened.
// Must be called with mu held.
func (s *Scheduler) isBackupDue() bool {
    last := s.state.GetLastSuccess()
    if last.IsZero() {
        return true
    }
    return time.Since(last) >= s.interval
}
```

`shouldRunNow` is unchanged in structure (calls `isBackupDue`, then the battery/SSID/idle gates in order). Pre-flight gates are not modified.

Replace `NextBackupTime`:

```go
func (s *Scheduler) NextBackupTime() (time.Time, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    last := s.state.GetLastSuccess()
    if last.IsZero() {
        return time.Now(), nil
    }
    return last.Add(s.interval), nil
}
```

`NextBackupTime` no longer returns an error in practice (interval is pre-validated), but the signature stays for API compatibility with current callers/tests.

Delete:
- `parseTime` (no longer used).
- The today-anchor block inside `isBackupDue` and `NextBackupTime`.

`getLocation` and the `location` field stay — they may still be useful for log formatting and remain wired through `Location()` for any external callers.

### State (`internal/state/state.go`)

- `LastSuccessAge`, `GetLastSuccess`, `RecordSuccess`, `RecordFailure` — unchanged.
- Remove `HasBackedUpToday` and `HasBackedUpTodayAfter`. They have no remaining callers once the daily code path is gone. (Verify with `grep` before deleting.)

### Logging retention (`internal/logging/logging.go`)

Hourly users will produce ~24 logs/day; current retention (the package-level `maxLogFiles = 25` constant in `logging.go`) wipes a day's worth in a single afternoon. Bump retention so daily users are unaffected and hourly users keep ~7 days.

- Change `CleanupOldLogs()` to `CleanupOldLogs(maxFiles int)` — the caller (the `app` wiring layer) passes the computed cap so the `logging` package stays decoupled from config types.
- Add a small helper next to it: `RetentionFor(interval time.Duration) int` returning `min(500, max(25, int(math.Ceil((7*24*time.Hour).Seconds() / interval.Seconds()))))`.
- The `app` layer computes `RetentionFor(cfg.Schedule.IntervalOrDefault())` and passes it into `CleanupOldLogs` at every call site.
- Sanity table:
  - 24h interval → 25 logs (unchanged).
  - 1h interval → 168 logs.
  - 30m interval → 336 logs.
  - sub-15m (rejected by Validate) would clamp at 500.

### Config template (`internal/config/template.go`)

Replace the `schedule` block:

```yaml
# Schedule settings
schedule:
  interval: "24h"          # Go duration: how often to run backups. Min 15m.
                           # Examples: "30m", "1h", "6h", "12h", "24h"
  # timezone: ""           # Optional, defaults to system timezone
  # skip_on_battery: false # Skip scheduled backups when on battery power (manual backups always run)
  # allowed_ssids: []      # Only run scheduled backups on these WiFi SSIDs (empty = no restriction)
  #   - "HomeWiFi"
  #   - "OfficeNetwork"
```

### Tray / status

`FormatStatus` already prints "Last backup: 23m ago", which works at any cadence. `FormatNextBackup` keeps working because `NextBackupTime` still returns a usable time.

No tray code changes are required for this feature.

### README

- Replace the daily-schedule wording with interval-based wording.
- Document the legacy behavior change (`schedule.time` is deprecated, behavior anchored to last success rather than 01:00 wall clock).
- Add an "Hourly / sub-daily backups" subsection covering trade-offs:
  - Healthchecks.io: pings now fire every interval; tune the schedule grace there.
  - Pushover with `on_success: true`: produces one notification per run.
  - Logs: retention scales automatically (see above).

## Behavior change notes

A user upgrading with only `time: "01:00"` set will, after upgrade:

- Schedule on a rolling 24h cadence keyed off `LastSuccess`, not the 01:00 wall clock.
- If `LastSuccess` was < 24h ago, the next run fires at `LastSuccess + 24h`. Otherwise it fires on the next 15-min tick.
- See a `schedule.time is deprecated...` log message in `app.log` on startup.

This is the only intentional regression. It is documented in CHANGELOG / release notes.

## Testing

All tests live next to the code they cover (`*_test.go`) and use the existing `NEUBIBACKUP_APP_DIR` test pattern.

### `internal/config/config_test.go`

- `Validate` accepts empty `Interval` (defaults to 24h, no error).
- `Validate` accepts `"1h"`, `"30m"`, `"24h"`, `"15m"`.
- `Validate` rejects `"10m"` (sub-15m), `"0s"`, `"garbage"`.
- `Validate` does not error when `Time` is empty.
- `Validate` does not error when `Time` is set (deprecated but harmless).
- `IntervalOrDefault` returns 24h for empty, parsed value otherwise.

### `internal/scheduler/scheduler_test.go`

- `isBackupDue` table:
  - never-run → true.
  - last success 30m ago, interval 1h → false.
  - last success 2h ago, interval 1h → true.
  - last success 25h ago, interval 24h → true.
  - last success 1h ago, interval 24h → false.
- `NextBackupTime`:
  - never-run → returned time within 1s of `time.Now()`.
  - last success at known T, interval 1h → returns T+1h exactly.
- `UpdateConfig` re-caches the parsed interval (table: change interval, observe `isBackupDue` answer flip).
- Drop or rewrite `TestNextBackupTime` (which tested today vs tomorrow anchoring) and `TestIsBackupDue` (which tested the 23:59 / 00:01 edge cases) to match the new semantics. Existing battery/SSID/idle/manual-trigger tests stay as-is.

### `internal/state/state_test.go`

- Remove tests for `HasBackedUpToday` / `HasBackedUpTodayAfter` along with the methods.

### `internal/logging` (or wherever retention lives)

- Retention helper: 24h → 25, 1h → 168, 30m → 336, 5m clamps to 500.

### Manual smoke

- Build, set `interval: "1h"` in `.dev-data/config.yaml`, run, observe a backup at startup if `LastSuccess` is older than 1h, and a second backup ~1h later (advance by sleeping a laptop or by manually editing `state.yaml`'s `last_success`).
- Reload config (edit `interval: "30m"`, save) and confirm the next-due time updates.

## Files touched

- `internal/config/config.go` — add `Interval`, add `IntervalOrDefault`, update `Validate`.
- `internal/config/template.go` — new schedule block.
- `internal/config/config_test.go` — new validation + helper tests.
- `internal/scheduler/scheduler.go` — interval cache, rewritten `isBackupDue` and `NextBackupTime`, drop `parseTime`.
- `internal/scheduler/scheduler_test.go` — replace daily-anchor tests with interval tests.
- `internal/state/state.go` — drop `HasBackedUpToday*`.
- `internal/state/state_test.go` — drop matching tests.
- `internal/logging/logging.go` — change `CleanupOldLogs` signature to take a `maxFiles int`, add `RetentionFor(time.Duration) int`.
- `internal/logging/logging_test.go` — update existing tests for the new signature, add table tests for `RetentionFor`.
- `internal/config/config.go` — emit the `schedule.time` deprecation warning from `LoadFromFile` (covers both `Load` and direct loaders, fires once per load) using `slog.Warn` gated on `cfg.Schedule.Time != ""`.
- `internal/app/app.go` — call `logging.CleanupOldLogs(logging.RetentionFor(a.cfg.Schedule.IntervalOrDefault()))` at every existing `CleanupOldLogs` call site.
- `README.md` — rewrite schedule docs, document upgrade behavior.

## Risks and rollback

- **Risk:** users relying on the precise 01:00 wall-clock anchor get a slightly drifting cadence. *Mitigation:* document in release notes; the rolling cadence is what most users actually want and the missed-schedule wake handler already breaks the strict anchor today.
- **Risk:** retention bump for hourly users surprises someone watching disk usage. *Mitigation:* 500-log cap + each log is small.
- **Rollback:** revert the PR. The `interval` field would simply be ignored by the previous binary; users on legacy `time` keep working.
