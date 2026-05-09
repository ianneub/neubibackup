# Changelog

All notable changes to NeubiBackup are documented here.

## v2.0.0

### Breaking changes

- **Config schema bumps to version 2.** Existing v1 configs are rejected at startup with a clear migration error.
- **`schedule.time` is removed.** Replace with `schedule.cron`, which accepts either a 5-field cron expression (e.g. `"0 1 * * *"`) or a Go-duration descriptor (e.g. `"@every 1h"`).
- **`config.yaml` is parsed strictly.** Unknown fields produce a YAML error naming the offending key. This is intentional, to catch leftover `schedule.time:` entries during the v1 → v2 migration.

### Migration

In `~/neubibackup/config.yaml`:

1. Set `version: 2` at the top.
2. Remove the `schedule.time:` line.
3. Add `schedule.cron: "<expression>"`. Quick mappings:
   - `time: "01:00"` → `cron: "0 1 * * *"`
   - `time: "02:00"` → `cron: "0 2 * * *"`
   - No preference → `cron: "@every 24h"`

See the README "Migrating from v1 to v2" section for details.

### New

- **Configurable schedule** via cron expressions or `@every` descriptors (hourly, every-6-hours, twice-daily, etc.). Minimum gap between fires is 15 minutes.
- **"Next backup: …"** menu item directly under "Last backup: …" showing when the next scheduled run will fire.
- **Log retention scales with cadence.** Daily backups keep 25 logs (unchanged), hourly keep ~168, capped at 500.

### Internal

- Scheduler now uses `github.com/robfig/cron/v3`. The daily-anchor code path is removed.
- `state.HasBackedUpToday` and `state.HasBackedUpTodayAfter` are removed (they had no remaining callers).
