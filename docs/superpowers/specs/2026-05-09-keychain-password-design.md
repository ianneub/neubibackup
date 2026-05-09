# Cross-Platform Keychain Password Storage

**Date:** 2026-05-09
**Status:** Design

## Goal

Add a fourth restic-repository-password source that stores the password in the
OS-native credential vault (macOS Keychain, Windows Credential Manager) under
neubibackup's own code-signing identity. This gives users a path that is both
**encrypted at rest** and **scoped to neubibackup** — better than the existing
`password_command: security find-generic-password ...` recipe, where the
Keychain ACL is bound to `/usr/bin/security` and any process the user can run
inherits access.

## Non-Goals

- Storing other config secrets (`pushover.api_token`, `tailscale.auth_key`) in
  the keychain. Same package can handle them later; punted for v1 to keep scope
  tight.
- Linux support. The project does not target Linux; the new package returns
  `ErrUnsupported` on non-darwin/non-windows platforms so dev cross-builds stay
  green.
- Migrating existing users. New mode is opt-in via config.
- **Changes to `.github/workflows/release.yml` to switch from ad-hoc
  signing to Developer ID signing + notarization.** Tracked separately and
  out of scope for this work. See "Assumptions" below for what this design
  takes as given.

## Background

Today `internal/config.RepositoryConfig` exposes three password sources, exactly
one of which must be set:

```go
Password        string `yaml:"password"`
PasswordFile    string `yaml:"password_file"`
PasswordCommand string `yaml:"password_command"`
```

`internal/restic/runner.go` translates them to either `--password-file`,
`--password-command`, or `RESTIC_PASSWORD` env on the restic invocation.

A user can already approximate keychain storage on macOS with
`password_command: security find-generic-password -s neubibackup -w`. The
problem: clicking "Always Allow" on the first prompt adds `/usr/bin/security`
to the item's ACL — not neubibackup. Any subsequent process invoking
`/usr/bin/security` reads the password without prompting. The protection is
session-gated and at-rest only, not app-scoped.

To get app-scoped access on macOS, neubibackup must call `Security.framework`
APIs directly so the ACL binds to neubibackup's own designated requirement
(DR). For that DR to be **stable across releases** — so users approve once
and stay approved through future auto-updates — the binary needs to be signed
with a stable identity (Apple Developer ID), not ad-hoc.

Windows Credential Manager has no per-app ACL; the protection there is DPAPI
at rest plus login-session gating, which we get for free by using the Win32
Credential Manager API.

## Assumptions

This design assumes — but does **not** itself implement — that a future
release-workflow change moves macOS builds from ad-hoc signing
(`codesign --sign -`) to **Developer ID Application** signing with the
hardened runtime and notarization. Specifically:

- macOS releases will be signed with a stable certificate identity
  (`Developer ID Application: <Org> (<TeamID>)`), giving every release the
  same designated requirement.
- The `creativeprojects/go-selfupdate` flow will continue to work because
  every published artifact carries a matching, notarized signature.

The keychain feature **functions correctly under ad-hoc signing too** — the
`Security.framework` calls succeed, the ACL is just bound to the current
binary's cdhash. The practical consequence on ad-hoc builds: the user gets a
Keychain prompt the first time *each new build* of neubibackup tries to read
the password, because the cdhash (and therefore the DR) changes every build.
Approving once per dev rebuild or once per ad-hoc release is workable for
testing this feature in development, but it is not the intended end-user
experience. Documentation will note this and recommend that users wait for
the signed-release rollout before opting into `use_keychain: true` if they
care about silent operation. No code branches on signing status.

## Design

### Config schema

Add one field to `RepositoryConfig`:

```yaml
repository:
  path: rest:https://...
  use_keychain: true
```

`use_keychain` is mutually exclusive with `password`, `password_file`, and
`password_command`. `Validate()` requires **exactly one** password source.
Error message names the new option and points at the README section.

`config.RepositoryConfig`:

```go
type RepositoryConfig struct {
    Path            string `yaml:"path"`
    Password        string `yaml:"password"`
    PasswordFile    string `yaml:"password_file"`
    PasswordCommand string `yaml:"password_command"`
    UseKeychain     bool   `yaml:"use_keychain"`
}
```

### `internal/keychain` package

New package with a small platform-agnostic surface:

```go
package keychain

var ErrNotFound = errors.New("keychain entry not found")
var ErrUnsupported = errors.New("keychain not supported on this platform")

// Get retrieves the password for the given account (typically the repo path).
func Get(account string) (string, error)

// Set stores the password for the given account, replacing any existing value.
func Set(account, password string) error

// Delete removes the keychain entry for the given account.
// Returns ErrNotFound if no entry exists.
func Delete(account string) error
```

Service name constant: `"com.neubibackup.repository"`. The account is
`cfg.Repository.Path`, so multi-repo setups (different binaries pointing at
different repos, same OS user) get distinct entries.

#### Files

- `keychain.go` — shared types, exported errors, service constant.
- `keychain_darwin.go` — build tag `darwin`. Uses `github.com/keybase/go-keychain`
  (cgo wrapper over `Security.framework`). Creates generic password items with
  default ACL (binds to current binary's code-signing identity).
- `keychain_windows.go` — build tag `windows`. Uses `github.com/danieljoos/wincred`
  (pure Go, no cgo) to write `CRED_TYPE_GENERIC` credentials.
- `keychain_other.go` — build tag `!darwin && !windows`. All three functions
  return `ErrUnsupported`. This keeps the Linux Docker cross-build path
  (`CGO_ENABLED=0` per `CLAUDE.md`) green.
- `keychain_test.go` — platform-conditional tests (build-tagged on
  darwin/windows) that round-trip Set→Get→Delete→Get(=ErrNotFound).

#### macOS implementation notes

Use `keychain.NewGenericPassword`, `keychain.AddItem`, `keychain.QueryItem`,
`keychain.DeleteItem`. Don't set `SetAccessGroup` — leave it at default so the
ACL binds to the current process's designated requirement. Use
`SecAccessibleWhenUnlocked` so the entry is available only while the keychain
is unlocked (matches login session).

When `Set` is called and an entry already exists, use
`keychain.UpdateItem` if the library exposes it; otherwise fall back to
delete-then-add inside the same function, propagating the original entry's
contents only if the add fails (so a half-failed update doesn't silently lose
the user's password).

#### Windows implementation notes

Use `wincred.GenericCredential` with `TargetName =
"com.neubibackup.repository:" + account`, `Persist = LocalMachine`,
`CredentialBlob = []byte(password)`. `Write()`, `GetGenericCredential()`,
`Delete()`.

### Runner integration

In `internal/restic/runner.go`'s `buildEnv()`:

```go
switch {
case cfg.Repository.UseKeychain:
    pw, err := keychain.Get(cfg.Repository.Path)
    if err != nil {
        // bubble up; runner.ensureRepositoryExists will treat it as ErrPasswordFailed
        return nil, err
    }
    env = append(env, "RESTIC_PASSWORD="+pw)
case cfg.Repository.Password != "":
    env = append(env, "RESTIC_PASSWORD="+cfg.Repository.Password)
}
```

`buildEnv` will need to grow an error return (it currently returns
`[]string`). Callers in `runBackupOnce` and `ensureRepositoryExists` handle
the error: a keychain miss surfaces as `ErrPasswordFailed`, which is already
wired to skip retries and notify the user.

`buildRepoArgs()` is unchanged — `--password-file` / `--password-command` are
not added when `use_keychain` is true.

For testability, introduce a small interface in `internal/restic`:

```go
type passwordSource interface {
    Get(account string) (string, error)
}
```

Default to a `keychain.Get`-backed implementation; tests inject a fake.

### CLI subcommands

Two new subcommands, parsed in `main.go` ahead of the tray bootstrap:

- `neubibackup set-password` — reads a password from stdin with no echo
  (`golang.org/x/term.ReadPassword`), confirms it (re-prompts), calls
  `keychain.Set(cfg.Repository.Path, pw)`. Exits 0 on success, non-zero with a
  clear error on failure (e.g., config not loaded, repo path empty, keychain
  call failed).
- `neubibackup clear-password` — calls `keychain.Delete(cfg.Repository.Path)`.
  `ErrNotFound` is treated as success (idempotent). Other errors exit non-zero.

Both subcommands require a configured `repository.path` so the account name is
known. They do **not** require `use_keychain: true` — the user can stage the
password before flipping the config flag.

### Tray menu

Under the existing tray menu, add a "Repository password" submenu (or two
top-level items, depending on what `getlantern/systray` makes simplest):

- "Set repository password…"
- "Clear repository password"

Both are greyed out when `cfg.Repository.UseKeychain == false`. They invoke
the same code path as the CLI subcommands, but capture the password via a
platform-native dialog:

- macOS: `osascript -e 'display dialog "..." default answer "" with hidden answer'`
  — output parsed for `text returned:`.
- Windows: spawn `powershell.exe` with `-Command "Read-Host -AsSecureString | ConvertFrom-SecureString -AsPlainText"`,
  read stdout. (`-AsPlainText` requires PowerShell 7+; for Windows PowerShell
  5.1 fall back to a tiny `[Runtime.InteropServices.Marshal]::PtrToStringAuto`
  one-liner. Detail to nail down in the plan.)

If the dialog is cancelled or returns empty, the tray shows a transient
notification ("Password not changed") and the existing keychain entry is left
untouched.

After Set, the tray refreshes its status: if `use_keychain: true` and
`keychain.Get` succeeds, the "missing password" badge clears.

### Status / error surfacing

`internal/state` is unchanged. The runner already converts password failures
into `ErrPasswordFailed`, which the existing healthchecks/Pushover/tray paths
handle. New angle: on startup, if `use_keychain: true` and `keychain.Get`
returns `ErrNotFound`, log a warning and surface the condition through the
existing tray status mechanism (same channel that reports a missing/invalid
config) so the user knows to run `set-password`. Implementation: a one-shot
check after config load, before the scheduler starts. Exact tray surface
(menu-item label vs. icon state) follows whatever pattern the tray package
already uses for similar startup errors.

### Validation rules

`Config.Validate()`:

1. Count password sources: `Password != ""`, `PasswordFile != ""`,
   `PasswordCommand != ""`, `UseKeychain == true`. Exactly one must be set.
2. If `use_keychain: true`, `repository.path` must be set (already required,
   but the error message should mention the keychain dependency).

`IsConfigured()` is unchanged — repo path is still the gate.

## Tests

Required per `CLAUDE.md`. Concretely:

### `internal/keychain`

- `keychain_darwin_test.go` (build-tagged darwin): round-trip Set/Get/Delete.
  Uses a randomized account name to avoid collisions across test runs.
  Cleans up in `t.Cleanup`.
- `keychain_windows_test.go` (build-tagged windows): same shape.
- `keychain_other_test.go`: asserts `ErrUnsupported` on stub builds.

### `internal/config`

Extend `config_test.go`:

- `Validate()` rejects zero password sources.
- `Validate()` rejects two password sources (covering each pair).
- `Validate()` accepts `use_keychain: true` alone.
- YAML round-trip test for the new field.

### `internal/restic`

- `runner_test.go`: extend `buildEnv` table to assert `RESTIC_PASSWORD` is set
  when `use_keychain: true` and a fake `passwordSource` returns "secret123".
- `runner_test.go`: assert `ErrPasswordFailed` propagates when the fake source
  returns `keychain.ErrNotFound`.
- `runner_test.go`: assert no `--password-command` / `--password-file` flag is
  added in keychain mode (extend the existing `buildRepoArgs` table).

### Subcommand tests

- New `cmd_password_test.go` (or extension of `main_test.go`): test
  `set-password` and `clear-password` against a stubbed keychain backend.
  Use the existing `NEUBIBACKUP_APP_DIR` `TestMain` pattern (per
  `CLAUDE.md`).

## Documentation

Update `README.md`:

- Features list: "Native keychain integration on macOS and Windows."
- Configuration section: document `use_keychain: true`, link to the
  `set-password` subcommand.
- Tray menu section: list the two new entries.
- Troubleshooting:
  - "macOS prompts on every update" — note this is expected on ad-hoc
    releases (current state) and resolves once the project ships
    Developer-ID-signed releases. Recommend `password_command` in the
    interim if silent operation matters.
  - "Password prompt after rebuilding from source" — code-signature change,
    click "Always Allow" once or re-run `set-password`.
  - "Keychain not available on Linux" — by design.
  - macOS: explicit comparison with `password_command: security ...` and why
    `use_keychain` is preferred (assuming a signed release).

## Risks / Open Questions

- **Pre-signing UX.** Until the release workflow switches to Developer ID
  signing, every ad-hoc release has a different cdhash and therefore a
  different DR. Users opting into `use_keychain: true` on ad-hoc releases will
  see a Keychain prompt at every auto-update. README will warn about this
  and recommend either waiting for the signed-release rollout or sticking
  with `password_command` in the meantime. The feature is shipped now so the
  signed-release rollout has nothing to integrate later — it just becomes
  better.
- **One-time prompt at the signing cutover.** Users who set the password on
  the last ad-hoc release will get one final prompt on the first signed
  release (cdhash → Team-ID-based DR transition). After they click
  "Always Allow" once on the signed binary, future signed releases are
  silent. Documented in the release notes for whichever version flips the
  signing.
- **`keybase/go-keychain` maintenance.** Active enough for our needs (used by
  Keybase, 1Password CLI, others). If it stagnates, the wrapper surface is
  small (~3 calls); we can replace with a thin direct cgo binding.
- **PowerShell version skew on Windows.** `-AsPlainText` is PS 7+. Plan must
  pick a snippet that works on Windows PowerShell 5.1 (default on Windows 10/11)
  too.
- **CGO and cross-builds.** `internal/keychain` is the second cgo-only package
  on macOS (after tray). The Linux/CGO_ENABLED=0 docker path is unaffected
  thanks to the `keychain_other.go` stub. CI already builds on macOS/Windows
  runners; no new CI surface.

## Out of Scope (for v1)

- Per-field secret storage for Pushover, Tailscale, healthchecks URLs.
- Multi-account / multi-user keychain entries beyond the per-repo-path key.
- GUI-based password change without going through the tray (e.g., a settings
  window). The tray prompt is sufficient.
- Linux Secret Service support.
- **Release-workflow changes for Developer ID signing + notarization.** The
  feature ships first; the signing workflow lands separately when the Apple
  developer-account work is finished.
