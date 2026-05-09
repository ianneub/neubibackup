# Configurable backup schedule (cron + interval)

**Date:** 2026-05-09
**Status:** Draft for review

## Summary

Replace the fixed daily-at-`schedule.time` scheduling with a single configurable `schedule.cron` field that accepts either a standard 5-field cron expression (`"0 1 * * *"`, `"*/30 * * * *"`) or a Go-duration descriptor (`"@every 1h"`, `"@every 6h"`). Parsing is delegated to `github.com/robfig/cron/v3`, which natively handles both forms through a single `cron.Schedule` interface. When `cron` is unset, the scheduler defaults to `"@every 24h"` so legacy installs keep running on roughly the same cadence. The existing `schedule.time` field becomes a deprecated no-op kept only so legacy YAML still parses.

## Motivation

Users want backups more frequent than once per day (notably hourly), and some users prefer wall-clock anchoring ("every day at 01:00") while others prefer rolling intervals ("every hour from the last success"). One field that accepts cron expressions or `@every` descriptors covers both audiences with a single code path and a single dependency.

## Goals

- Let users configure backup cadence via a single `schedule.cron` field accepting cron expressions or `@every <duration>` descriptors.
- Default to `"@every 24h"` so installs without the field continue working unchanged in spirit.
- Reject configurations whose minimum gap between consecutive fires is below 15m (the scheduler ticker floor) at config-load time, with a clear error message.
- Remove the daily-anchor scheduling code path entirely; one mechanism, one code path.
- Preserve existing pre-flight gates (battery, allowed SSIDs, idle limit) and manual-trigger behavior.

## Non-goals

- No second-resolution cron (5-field, no seconds field).
- No automatic config migration that rewrites the user's `config.yaml`. The `time` field is silently ignored with a deprecation log.
- No special UX in the tray for editing the cron expression. Editing is via `config.yaml`, like every other setting.

## Dependency

Add `github.com/robfig/cron/v3` (MIT, widely used). We use:

- `cron.ParseStandard(spec string) (cron.Schedule, error)` — parses 5-field cron expressions plus `@every`, `@daily`, `@hourly`, `@weekly`, etc.
- `Schedule.Next(t time.Time) time.Time` — returns the next fire time strictly after `t`.

Add to `go.mod` via `go get github.com/robfig/cron/v3`.

## Design

### Config schema

`internal/config/config.go` — `ScheduleConfig` gains a `Cron` field:

```go
type ScheduleConfig struct {
    Cron          string   `yaml:"cron"`            // NEW. Cron expression or "@every <duration>". Default: "@every 24h".
    Time          string   `yaml:"time"`            // DEPRECATED. Ignored at runtime. Kept for YAML compat.
    Timezone      string   `yaml:"timezone"`
    SkipOnBattery bool     `yaml:"skip_on_battery"`
    AllowedSSIDs  []string `yaml:"allowed_ssids"`
}
```

User-facing yaml (new installs):

```yaml
schedule:
  cron: "@every 24h"        # Cron expression or "@every <duration>". Min gap: 15m.
                            # Examples:
                            #   "@every 1h"      — every hour, rolling from last success
                            #   "@every 6h"      — every 6 hours
                            #   "0 1 * * *"      — every day at 01:00 (cron)
                            #   "*/30 * * * *"   — every 30 minutes on the half-hour (cron)
                            #   "0 8,18 * * *"   — daily at 08:00 and 18:00
  # timezone: ""            # Optional, defaults to system timezone (used by cron-style expressions)
  # skip_on_battery: false
  # allowed_ssids: []
```

Legacy yaml (still parses, `time` is ignored with warning):

```yaml
schedule:
  time: "01:00"             # deprecated, ignored
```

### Validation (`config.Validate`)

Replace the `if c.Schedule.Time == ""` requirement with cron-spec validation:

- If `Schedule.Cron` is empty, treat as `"@every 24h"` (no error).
- If `Schedule.Cron` is set:
  - Must parse via `cron.ParseStandard`. Bad parse → `fmt.Errorf("schedule.cron is not a valid cron expression or @every descriptor: %w", err)`.
  - Must have a minimum gap between consecutive fires of `>= 15 * time.Minute` (see "Min-gap validation" below). Smaller → `fmt.Errorf("schedule.cron fires too frequently: minimum gap is %s, must be at least 15m", minGap)`.
- `Schedule.Time` is no longer required and is no longer used by the scheduler. If `Schedule.Time != ""`, log once at WARN level on config load: `"schedule.time is deprecated and ignored; backups now use schedule.cron (default \"@every 24h\")"`. The warning is fired from `LoadFromFile` (covers both `Load` and direct loaders, fires once per load), gated on `cfg.Schedule.Time != ""`.

Add a helper that centralizes default + parse + min-gap check, returning both the schedule and the discovered min gap (the latter used for log retention sizing):

```go
const minScheduleGap = 15 * time.Minute

// ParseSchedule validates and parses cfg.Schedule.Cron, returning the parsed
// schedule and the minimum gap between consecutive fires. Empty Cron defaults
// to "@every 24h". Returns an error if the spec parses but fires more often
// than minScheduleGap.
func (s ScheduleConfig) ParseSchedule() (cron.Schedule, time.Duration, error) {
    spec := s.Cron
    if spec == "" {
        spec = "@every 24h"
    }
    sched, err := cron.ParseStandard(spec)
    if err != nil {
        return nil, 0, fmt.Errorf("schedule.cron is not a valid cron expression or @every descriptor: %w", err)
    }
    minGap, err := minGapOf(sched)
    if err != nil {
        return nil, 0, err
    }
    if minGap < minScheduleGap {
        return nil, 0, fmt.Errorf("schedule.cron fires too frequently: minimum gap is %s, must be at least %s", minGap, minScheduleGap)
    }
    return sched, minGap, nil
}
```

#### Min-gap validation

`minGapOf(sched cron.Schedule) (time.Duration, error)` walks the schedule from a fixed anchor and tracks the smallest delta between consecutive fires:

- Anchor at `time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)` (deterministic — never the current clock).
- Loop up to `maxIters = 1000` fires OR until `now.Sub(anchor) >= 366 * 24 * time.Hour` (whichever comes first).
- On each iteration: compute `next := sched.Next(prev)`, record `gap := next.Sub(prev)`, update `minGap`. If `minGap < minScheduleGap`, short-circuit and return immediately (this catches `* * * * *` in ~1 iteration).
- If the loop completes without crossing the threshold, return the minimum gap observed.
- Edge case: `Schedule.Next` returns the zero time when no future fire exists (extremely rare with valid cron specs, e.g. `0 0 30 2 *` — Feb 30, never fires). If `Next` returns zero, return `(0, fmt.Errorf("schedule.cron has no future fires"))`.

This handles every realistic case in a few microseconds and bounds pathological inputs.

### Scheduler (`internal/scheduler/scheduler.go`)

Cache the parsed schedule on the struct so we don't re-parse every 15-minute tick:

```go
type Scheduler struct {
    config   *config.Config
    state    *state.State
    onBackup BackupFunc
    location *time.Location
    schedule cron.Schedule    // NEW. Cached from config.Schedule.ParseSchedule().
    minGap   time.Duration    // NEW. Used by app for log retention sizing.
    mu       sync.Mutex
    running  bool
}
```

`New` and `UpdateConfig` set `s.schedule, s.minGap, _ = cfg.Schedule.ParseSchedule()` under the lock. (Errors at this layer are unreachable because `Validate` has already run; a returned error from `UpdateConfig` is propagated as before.)

Replace `isBackupDue` with a single cron-driven check:

```go
// isBackupDue returns true if the schedule has fired at least once since the
// last successful backup (or ever, if no successful backup has happened).
// Must be called with mu held.
func (s *Scheduler) isBackupDue() bool {
    last := s.state.GetLastSuccess()
    if last.IsZero() {
        return true
    }
    next := s.schedule.Next(last)
    return !time.Now().Before(next)
}
```

`shouldRunNow` is unchanged in structure (calls `isBackupDue`, then the battery/SSID/idle gates in order). Pre-flight gates are not modified.

Replace `NextBackupTime`:

```go
// NextBackupTime returns when the next scheduled backup will fire from now.
func (s *Scheduler) NextBackupTime() (time.Time, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    last := s.state.GetLastSuccess()
    if last.IsZero() {
        return time.Now(), nil
    }
    // Next fire after last success; if that's already in the past, it's "due now"
    // and we surface the next future fire from now.
    next := s.schedule.Next(last)
    if !time.Now().Before(next) {
        return time.Now(), nil
    }
    return next, nil
}
```

`NextBackupTime` keeps its `(time.Time, error)` signature for API compatibility with current callers/tests.

Add a small accessor for the app to read the cached min gap (used by log retention):

```go
// MinGap returns the minimum gap between consecutive fires for the current schedule.
func (s *Scheduler) MinGap() time.Duration {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.minGap
}
```

Delete:
- `parseTime` (no longer used).
- The today-anchor block inside the old `isBackupDue` and `NextBackupTime`.

`getLocation` and the `location` field stay — they're already passed to log timestamps and are wired through `Location()` for any external callers. Note: cron expressions are evaluated by `robfig/cron/v3` against the local time of whatever `time.Time` you pass to `Next`. We pass `time.Time` values that come from `time.Now()` and stored success times, so cron expressions implicitly evaluate in the system's local timezone. If `Schedule.Timezone` is set we apply it via `time.LoadLocation` and convert times to that location before calling `Next`. Update `getLocation` documentation to reflect this.

### State (`internal/state/state.go`)

- `LastSuccessAge`, `GetLastSuccess`, `RecordSuccess`, `RecordFailure` — unchanged.
- Remove `HasBackedUpToday` and `HasBackedUpTodayAfter`. They have no remaining callers once the daily code path is gone. (Verify with `grep` before deleting.)

### Logging retention (`internal/logging/logging.go`)

Hourly users will produce ~24 logs/day; current retention (the package-level `maxLogFiles = 25` constant) wipes a day's worth in a single afternoon. Bump retention so daily users are unaffected and frequent-backup users keep ~7 days.

- Change `CleanupOldLogs()` to `CleanupOldLogs(maxFiles int)` — the caller (the `app` wiring layer) passes the computed cap so the `logging` package stays decoupled from config types.
- Add a small helper next to it: `RetentionFor(minGap time.Duration) int` returning `min(500, max(25, int(math.Ceil((7*24*time.Hour).Seconds() / minGap.Seconds()))))`.
- The `app` layer computes `RetentionFor(scheduler.MinGap())` and passes it into `CleanupOldLogs` at every existing `CleanupOldLogs` call site.
- Sanity table:
  - 24h min gap → 25 logs (unchanged).
  - 1h min gap → 168 logs.
  - 30m min gap → 336 logs.
  - 15m min gap → 500 logs (clamped).

### Config template (`internal/config/template.go`)

Replace the `schedule` block:

```yaml
# Schedule settings
schedule:
  cron: "@every 24h"       # Cron expression or "@every <duration>". Minimum gap: 15m.
                           # Examples:
                           #   "@every 1h"      — every hour, rolling from last success
                           #   "@every 6h"      — every 6 hours
                           #   "0 1 * * *"      — every day at 01:00 (cron)
                           #   "*/30 * * * *"   — every 30 minutes on the half-hour
                           #   "0 8,18 * * *"   — daily at 08:00 and 18:00
  # timezone: ""           # Optional, defaults to system timezone (affects cron expressions)
  # skip_on_battery: false # Skip scheduled backups when on battery power (manual backups always run)
  # allowed_ssids: []      # Only run scheduled backups on these WiFi SSIDs (empty = no restriction)
  #   - "HomeWiFi"
  #   - "OfficeNetwork"
```

### Tray / status

`FormatStatus` already prints "Last backup: 23m ago", which works at any cadence. `FormatNextBackup` keeps working because `NextBackupTime` still returns a usable time.

No tray code changes are required for this feature.

### README

- Replace the daily-schedule wording with cron / interval wording.
- Document the legacy behavior change (`schedule.time` is deprecated, replaced by `schedule.cron`, default behavior is rolling 24h from last success rather than fixed 01:00 wall clock).
- Add a "Backup frequency" subsection covering:
  - Two syntaxes (cron vs `@every`) and when to pick each.
  - The 15-minute minimum gap.
  - Trade-offs of frequent backups: Healthchecks.io ping cadence (tune the schedule grace there), Pushover with `on_success: true` produces one notification per run, log retention scales automatically (see above).

## Behavior change notes

A user upgrading with only `time: "01:00"` set will, after upgrade:

- Schedule on a rolling 24h cadence keyed off `LastSuccess`, not the 01:00 wall clock.
- If `LastSuccess` was < 24h ago, the next run fires at `LastSuccess + 24h`. Otherwise it fires on the next 15-min tick.
- See a `schedule.time is deprecated...` log message in `app.log` on startup.

Users who want to keep the old "01:00 daily" behavior can set `cron: "0 1 * * *"`.

This is the only intentional regression. It is documented in CHANGELOG / release notes.

## Testing

All tests live next to the code they cover (`*_test.go`) and use the existing `NEUBIBACKUP_APP_DIR` test pattern.

### `internal/config/config_test.go`

- `Validate` accepts empty `Cron` (defaults to `@every 24h`, no error).
- `Validate` accepts `"@every 1h"`, `"@every 30m"`, `"@every 24h"`, `"@every 15m"`.
- `Validate` accepts cron expressions: `"0 1 * * *"`, `"*/15 * * * *"`, `"0 8,18 * * *"`, `"@daily"`, `"@hourly"`.
- `Validate` rejects `"@every 10m"` (sub-15m), `"@every 0s"`, `"*/10 * * * *"` (10m gap), `"0,5 * * * *"` (5m gap on the hour boundary), `"* * * * *"`, `"garbage"`, `""` for syntax errors but accepts empty for default.
- `Validate` does not error when `Time` is empty.
- `Validate` does not error when `Time` is set (deprecated but harmless).
- `ParseSchedule` returns the expected min gap: `@every 1h` → 1h; `0 1,2 * * *` → 1h; `0 1 * * *` → 24h; `*/15 * * * *` → 15m.
- `ParseSchedule` short-circuits within 5ms on `* * * * *` (regression guard).

### `internal/scheduler/scheduler_test.go`

- `isBackupDue` table:
  - never-run → true.
  - last success 30m ago, schedule `@every 1h` → false.
  - last success 2h ago, schedule `@every 1h` → true.
  - last success 25h ago, schedule `@every 24h` → true.
  - last success 1h ago, schedule `@every 24h` → false.
  - cron `0 1 * * *`, last success today at 01:30, current time today 12:00 → false (next fire is tomorrow 01:00).
  - cron `0 1 * * *`, last success yesterday at 01:30, current time today 12:00 → true (next fire is today 01:00, which has passed).
- `NextBackupTime`:
  - never-run → returned time within 1s of `time.Now()`.
  - last success at known T, schedule `@every 1h` → returns T+1h exactly when T+1h is in the future, otherwise within 1s of `time.Now()`.
- `UpdateConfig` re-caches the parsed schedule (table: change cron from `@every 1h` to `@every 24h`, observe `isBackupDue` answer flip with the same fixed `LastSuccess`).
- `MinGap` returns the parsed min gap and reflects `UpdateConfig`.
- Drop or rewrite `TestNextBackupTime` (which tested today vs tomorrow anchoring) and `TestIsBackupDue` (which tested the 23:59 / 00:01 edge cases) to match the new semantics. Existing battery/SSID/idle/manual-trigger tests stay as-is.

For tests that need to control "now", inject a clock or use fixed `lastSuccess` values relative to `time.Now()` — current tests already use the latter pattern.

### `internal/state/state_test.go`

- Remove tests for `HasBackedUpToday` / `HasBackedUpTodayAfter` along with the methods.

### `internal/logging/logging_test.go`

- Update existing `CleanupOldLogs` tests for the new `(maxFiles int)` signature.
- Add `RetentionFor` table tests: 24h → 25, 1h → 168, 30m → 336, 15m → 500 (clamped at the cap).

### Manual smoke

- Build, set `cron: "@every 1h"` in `.dev-data/config.yaml`, run, observe a backup at startup if `LastSuccess` is older than 1h, and a second backup ~1h later (advance by sleeping a laptop or by manually editing `state.yaml`'s `last_success`).
- Reload config (edit `cron: "*/30 * * * *"`, save) and confirm `NextBackupTime` reflects the new schedule.
- Set an invalid `cron: "*/5 * * * *"` and confirm the app logs a clear validation error and surfaces the "Configuration required" tray state (whatever the existing handling is for invalid configs).

## Files touched

- `go.mod` / `go.sum` — add `github.com/robfig/cron/v3`.
- `internal/config/config.go` — add `Cron` field, add `ParseSchedule` + `minGapOf`, update `Validate`, emit the `schedule.time` deprecation warning from `LoadFromFile`.
- `internal/config/template.go` — new schedule block.
- `internal/config/config_test.go` — new validation + `ParseSchedule` tests.
- `internal/scheduler/scheduler.go` — schedule + minGap cache, rewritten `isBackupDue` and `NextBackupTime`, new `MinGap()` accessor, drop `parseTime`.
- `internal/scheduler/scheduler_test.go` — replace daily-anchor tests with cron tests.
- `internal/state/state.go` — drop `HasBackedUpToday*`.
- `internal/state/state_test.go` — drop matching tests.
- `internal/logging/logging.go` — change `CleanupOldLogs` signature to take a `maxFiles int`, add `RetentionFor(time.Duration) int`.
- `internal/logging/logging_test.go` — update existing tests for the new signature, add table tests for `RetentionFor`.
- `internal/app/app.go` — call `logging.CleanupOldLogs(logging.RetentionFor(a.scheduler.MinGap()))` at every existing `CleanupOldLogs` call site.
- `README.md` — rewrite schedule docs, document upgrade behavior.

## Risks and rollback

- **Risk:** users relying on the precise 01:00 wall-clock anchor get a slightly drifting cadence under the new default. *Mitigation:* document in release notes; the rolling cadence is what most users actually want, and users who want the exact old behavior can set `cron: "0 1 * * *"`.
- **Risk:** new dependency (`robfig/cron/v3`). *Mitigation:* widely used (16k+ stars), MIT, no transitive deps that aren't already in the module graph.
- **Risk:** retention bump for frequent-backup users surprises someone watching disk usage. *Mitigation:* 500-log cap + each log is small.
- **Rollback:** revert the PR. The `cron` field would simply be ignored by the previous binary; users on legacy `time` keep working.
