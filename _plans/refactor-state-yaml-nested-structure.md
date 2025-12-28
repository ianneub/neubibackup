# Plan: Refactor State YAML to Nested Structure

## Goal
Change `state.yaml` from flat keys to nested objects, grouping backup-related and update-related fields.

## Target Structure

**Current (flat):**
```yaml
last_backup_attempt: 2025-12-28T13:14:08Z
last_backup_success: 2025-12-28T13:14:08Z
last_backup_error: ""
consecutive_failures: 0
last_update_check: 2025-12-28T13:01:10Z
last_update_version: 1.3.1
last_update_time: 2025-12-28T13:01:12Z
```

**New (nested):**
```yaml
backup:
  last_attempt: 2025-12-28T13:14:08Z
  last_success: 2025-12-28T13:14:08Z
  last_error: ""
  consecutive_failures: 0
update:
  last_check: 2025-12-28T13:01:10Z
  last_version: 1.3.1
  last_time: 2025-12-28T13:01:12Z
  last_error: ""
  last_error_time: 0001-01-01T00:00:00Z
```

## Files to Modify

### 1. internal/state/state.go
- Add `BackupState` and `UpdateState` nested structs
- Update `State` struct to embed the nested structs + keep legacy fields for migration
- Add `migrate()` method to convert old format to new on load
- Update `LoadFromFile()` to call migrate after unmarshal
- Update `RecordSuccess()`, `RecordFailure()`, `LastSuccessAge()`, `HasBackedUpToday()` to use nested fields

### 2. internal/state/state_test.go
- Update all field references from `s.LastBackupSuccess` to `s.Backup.LastSuccess` etc.
- Add migration tests for old-to-new format conversion

### 3. main.go
- Update direct field access (~9 locations) to use nested structure:
  - `appState.LastUpdateCheck` → `appState.Update.LastCheck`
  - `appState.LastUpdateVersion` → `appState.Update.LastVersion`
  - etc.

### 4. internal/scheduler/scheduler.go
- Update `s.state.LastBackupSuccess` → `s.state.Backup.LastSuccess` (line ~168)

### 5. internal/tray/status.go
- Update `st.LastBackupSuccess` → `st.Backup.LastSuccess`
- Update `st.LastBackupError` → `st.Backup.LastError`
- Update `st.ConsecutiveFailures` → `st.Backup.ConsecutiveFailures`

## Implementation Steps

1. **Define nested structs** in `internal/state/state.go`:
   ```go
   type BackupState struct {
       LastAttempt         time.Time `yaml:"last_attempt"`
       LastSuccess         time.Time `yaml:"last_success"`
       LastError           string    `yaml:"last_error"`
       ConsecutiveFailures int       `yaml:"consecutive_failures"`
   }

   type UpdateState struct {
       LastCheck     time.Time `yaml:"last_check,omitempty"`
       LastVersion   string    `yaml:"last_version,omitempty"`
       LastTime      time.Time `yaml:"last_time,omitempty"`
       LastError     string    `yaml:"last_error,omitempty"`
       LastErrorTime time.Time `yaml:"last_error_time,omitempty"`
   }
   ```

2. **Update State struct** with both new nested fields and legacy fields (for backward compatibility):
   ```go
   type State struct {
       Backup BackupState `yaml:"backup,omitempty"`
       Update UpdateState `yaml:"update,omitempty"`

       // Legacy fields for backward compatibility (omitempty so they won't save)
       LastBackupAttempt   time.Time `yaml:"last_backup_attempt,omitempty"`
       // ... etc
   }
   ```

3. **Add migration logic** to `LoadFromFile()` that detects old format and copies to nested fields

4. **Update all methods** (RecordSuccess, RecordFailure, etc.) to use `s.Backup.*` and `s.Update.*`

5. **Update tests** to use new field paths and add migration tests

6. **Update consumers** (main.go, scheduler, tray) to use nested field access

7. **Run tests** to verify: `go test ./...`

## Backward Compatibility

- Old state.yaml files will be automatically migrated on first load
- Legacy fields have `omitempty` so they won't be saved in new format
- Migration is one-way: once saved in new format, stays in new format
