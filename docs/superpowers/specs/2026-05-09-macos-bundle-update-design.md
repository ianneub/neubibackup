# macOS auto-update: replace the entire .app bundle

**Status:** approved
**Date:** 2026-05-09
**Owner:** Ian Neubert

## Problem

The auto-updater on macOS replaces only the inner Mach-O binary
(`Contents/MacOS/neubibackup`) instead of the entire `.app` bundle.
The bundle's `_CodeSignature/CodeResources` and `Info.plist` are left
behind from the previous release, so after an update the signature
no longer matches the binary. Concretely:

- `codesign --verify --deep --strict /Applications/NeubiBackup.app`
  fails after an auto-update.
- The Designated Requirement embedded in the user's Keychain ACL
  for `use_keychain` entries no longer matches, breaking the silent
  unlock that the stable self-signed cert was meant to preserve
  (see `docs/release-signing.md`).

The cause is `go-selfupdate.UpdateTo`: its `unzip` step searches the
release ZIP for the entry whose name matches the executable regex
and writes only that single file to `cmdPath`. That is correct for
single-binary distributions but wrong for `.app` bundles.

## Goal

After an auto-update on macOS, `codesign --verify --deep --strict`
passes against the installed `.app` bundle and the Keychain ACL
silently matches without re-prompting the user.

Windows behavior is unchanged.

## Non-goals

- Notarization (still out of scope; we use a stable self-signed cert).
- Privileged escalation when `/Applications` is not writable. We
  surface a clear error and stop.
- Switching update transport away from `go-selfupdate` on Windows.
- Removing `go-selfupdate` from the macOS path; we keep using it for
  release detection and asset download.

## High-level approach

Keep `go-selfupdate` for version detection and for fetching the ZIP
asset. On macOS, bypass `Updater.UpdateTo` (which only writes one
file) and replace the entire `.app` directory atomically with a
darwin-only helper. On Windows, the existing path stays as-is.

```text
┌───────────────────────┐
│ orchestrator.Check    │
└──────────┬────────────┘
           │
           ▼
┌───────────────────────┐
│ updater.DownloadAndApply
│   if darwin →─────────┼──▶ applyDarwinBundleUpdate
│   else      →─────────┼──▶ selfupdate.UpdateTo (today's path)
└───────────────────────┘
```

## Components

### `internal/updater/apply_darwin.go` (new, build-tagged darwin)

Exports two helpers used by `Updater.DownloadAndApply` on darwin:

- `applyDarwinBundleUpdate(ctx, source, rel, runningExecPath) error` —
  the orchestrating function. Resolves the live `.app`, downloads
  and verifies the new bundle, performs the atomic swap.
- `cleanupStaleBundles(appPath)` — best-effort removal of
  `.NeubiBackup.app.old` siblings. Called from `main.go` at startup
  to recover from a previous run that exited before cleanup.

Internal helpers, factored out for testability:

- `extractZipToBundle(zipBytes []byte, destDir string) error` —
  validates the archive shape and writes files. Pure I/O over a
  byte slice and a directory; no networking, no `codesign`.
- `swapBundles(appPath, newPath, oldPath string) error` — the
  rename pair with rollback.
- `verifySignature(bundlePath string) error` — wraps `codesign
  --verify --strict`; returns an error if it exits non-zero.

### `internal/updater/updater.go` (modified)

`DownloadAndApply` branches on `runtime.GOOS`. Darwin path calls
`applyDarwinBundleUpdate(ctx, source, latest, exe)` instead of
`updater.UpdateTo`. The function still uses `go-selfupdate`'s
`GitHubSource.DownloadReleaseAsset` to fetch the ZIP — we don't
reimplement the GitHub source layer.

### Startup cleanup hook (darwin-only build-tagged file)

A new file `cleanup_darwin.go` (build-tagged `//go:build darwin`)
exports `CleanupStaleBundles()` which calls
`updater.cleanupStaleBundles(...)`. A matching
`cleanup_other.go` (build-tagged `!darwin`) exports a no-op
`CleanupStaleBundles()`. `main.go` calls `CleanupStaleBundles()`
unconditionally after tray init; the no-op makes Windows
unaffected and keeps `main.go` free of build tags.

## Swap flow

`applyDarwinBundleUpdate(ctx, source, rel, runningExec)`:

1. **Resolve paths.** Use `findAppBundle(runningExec)` (existing
   helper). If empty (running outside a bundle, e.g., dev), return
   an error so the orchestrator records it and stops.
   - `appPath` = the running `.app` directory.
   - `appDir` = `filepath.Dir(appPath)`.
   - `newPath` = `filepath.Join(appDir, ".NeubiBackup.app.new")`.
   - `oldPath` = `filepath.Join(appDir, ".NeubiBackup.app.old")`.

2. **Pre-flight write check.** Try `os.CreateTemp(appDir, ".nbup-*")`,
   then remove it. On EACCES return a clear error: "auto-update
   needs write access to `<appDir>`; download the latest DMG manually."
   The orchestrator already records error messages to `state.yaml`.

3. **Cleanup leftovers.** `os.RemoveAll(newPath)` and
   `os.RemoveAll(oldPath)` to recover from a half-finished run.

4. **Download.** `source.DownloadReleaseAsset(ctx, rel, rel.AssetID)`
   into a `bytes.Buffer`. Releases are <30 MB so in-memory is fine.
   Reject if `rel.AssetName` does not end in `.zip`.

5. **Extract.** `extractZipToBundle(zipBytes, newPath)`:
   - Open `archive/zip` over the bytes.
   - Require all entries to share a single top-level segment ending
     in `.app`. Reject otherwise.
   - For each entry, compute `rel = strings.TrimPrefix(entry.Name,
     "<top>")`, then `dest = filepath.Join(newPath, rel)`. Verify
     `filepath.Rel(newPath, dest)` does not start with `..`
     (zip-slip guard).
   - Recreate directories as needed. Write files with
     `entry.Mode().Perm()` so the executable bit on
     `Contents/MacOS/neubibackup` survives.

6. **Verify signature.** `verifySignature(newPath)` runs
   `codesign --verify --strict <newPath>`. Non-zero exit →
   `os.RemoveAll(newPath)` and abort with the codesign error.

7. **Atomic swap.** `swapBundles(appPath, newPath, oldPath)`:
   - `os.Rename(appPath, oldPath)`. If error, return it
     (live bundle untouched).
   - `os.Rename(newPath, appPath)`. If error, attempt
     `os.Rename(oldPath, appPath)` to roll back; return original
     error either way.

8. **Best-effort cleanup.** `os.RemoveAll(oldPath)`. Failures are
   logged and ignored — the running process may hold open files;
   `cleanupStaleBundles` will finish the job on next launch.

9. **Return nil.** The orchestrator then calls `Restart()`
   (unchanged: `open -n <appPath>` and `os.Exit(0)`).

## Failure modes

| Stage | Failure | Live bundle |
|---|---|---|
| Steps 1–6 | any error | untouched |
| Step 7 first rename | EACCES, busy | untouched |
| Step 7 second rename | unexpected | rolled back; if rollback fails, broken — error surfaced and reinstall via DMG required |
| Step 8 cleanup | fs busy | harmless leftover, cleaned next launch |

## Testing

All tests in `internal/updater/apply_darwin_test.go`,
build-tagged `//go:build darwin`. Use `t.TempDir()` only; no real
`/Applications` writes.

### Pure-logic tests

- `TestExtractZipToBundle_HappyPath` — in-memory zip with
  `NeubiBackup.app/Contents/MacOS/neubibackup` (mode 0755),
  `Contents/Info.plist`, `Contents/_CodeSignature/CodeResources` →
  extract to temp dir, assert all three exist with right contents
  and modes (executable bit preserved on the binary).
- `TestExtractZipToBundle_ZipSlip` — entry name `../escape.txt` →
  must error, no file under or beside the target.
- `TestExtractZipToBundle_WrongRoot` — top-level entry not ending
  in `.app`, or multiple top-level entries → must error.
- `TestExtractZipToBundle_Empty` — empty zip → must error.
- `TestSwapBundles_HappyPath` — fake source/dest dirs in temp;
  after `swapBundles`, assert old at `.old`, new at `appPath`.
- `TestSwapBundles_RollbackOnSecondRename` — inject a non-existent
  newPath (or a hook in a tiny indirection) so the second rename
  fails; assert rollback rename ran and the original bundle is
  back at `appPath`.

### Integration test (skipped if `codesign` unavailable)

- `TestApplyDarwinBundleUpdate_RealCodesign`:
  - Build a tiny fake `.app`: directory with `Contents/MacOS/foo`
    (shell script `#!/bin/sh\nexit 0`, chmod 0755) and a minimal
    `Info.plist`. Ad-hoc sign with `codesign --sign - <path>`.
  - Zip it. Pass through an injected source returning those bytes
    to a thin wrapper that mirrors `applyDarwinBundleUpdate` against
    a temp `appPath`.
  - Assert the swap succeeded and
    `codesign --verify --strict <appPath>` passes.
- `TestApplyDarwinBundleUpdate_VerifyRejectsTamperedBundle`:
  - Same as above, but flip one byte in
    `Contents/MacOS/foo` *after* signing, before zipping. Assert
    the function returns an error and the live bundle at
    `appPath` is untouched.
- Skip these tests if `codesign` is missing on `$PATH` or if
  `SKIP_CODESIGN_TESTS=1` is set, so non-darwin CI matrices
  remain green.

### Existing tests

`restart_darwin_test.go`, `updater_test.go`, and
`orchestrator_test.go` are unchanged.

### Manual smoke test (documented, not automated)

On a real Mac:

1. Install v(N) to `/Applications` from the DMG.
2. Configure a `use_keychain` repository and run a backup so the
   keychain ACL is created.
3. Publish v(N+1).
4. Let auto-update run (or use the menu item).
5. After restart, confirm:
   - `codesign --verify --deep --strict /Applications/NeubiBackup.app`
     exits 0.
   - The next backup runs without a keychain prompt.

## Open decisions (resolved)

- **Restic binary**: embedded via `//go:embed`, not a separate file
  in `Contents/Resources`. The swap doesn't need to handle it
  specially.
- **DMG vs ZIP**: auto-update uses the ZIP asset
  (`neubibackup_darwin_<arch>.zip`); release workflow already
  publishes both. No change.
- **Stale-bundle cleanup**: runs at startup from `main.go` (darwin
  only), best-effort.
- **Logging**: `slog` like the rest of the package; log download
  size, file count, swap timings.
- **No privileged escalation**: if write to `appDir` fails, surface
  a clear error and stop. Sudo/osascript prompts are scope creep.

## Out of scope

- Verifying the signing identity matches the previous release's
  identity. The current cert is stable by design (see
  `docs/release-signing.md`); if it ever rotates, that release's
  notes already need to warn users.
- Changing the release artifact format.
- Linux: the project has no Linux build today.
