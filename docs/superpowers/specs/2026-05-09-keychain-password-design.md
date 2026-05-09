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
- Apple Developer ID signing or notarization. The project deliberately stays
  off Apple's paid program. macOS releases will be signed with a stable
  self-signed cert (see "Release signing" below); Gatekeeper still treats
  the app as "from an unidentified developer," same as today's ad-hoc state.

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
with a stable identity. A self-signed code-signing cert reused across every
release satisfies this; Apple Developer ID is *not* required.

Windows Credential Manager has no per-app ACL; the protection there is DPAPI
at rest plus login-session gating, which we get for free by using the Win32
Credential Manager API.

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

### Release signing (macOS)

The macOS release workflow switches from ad-hoc signing
(`codesign --sign -`) to a stable self-signed cert. The cert is used for
every release in perpetuity; it is the linchpin of the keychain ACL story.

**Cert generation (one-time, manual, performed by maintainer):**

A self-signed code-signing cert with these properties:

- Type: X.509 with `extKeyUsage = codeSigning`
- Key: RSA 2048 or ECDSA P-256 (either is fine; ECDSA produces smaller
  signatures)
- Common Name: `NeubiBackup Code Signing` (or similar; cosmetic only — the
  DR uses the cert hash, not the name)
- Validity: 100 years (self-signed has no real expiration constraint;
  long-dated keeps the option of skipping `--timestamp`)
- Exported as a password-protected `.p12`

Generation may use Keychain Access ("Create a Certificate" → Type:
`Code Signing`, Identity Type: `Self Signed Root`) or `openssl` +
`security import`. The implementation plan picks one method and documents
the exact commands.

**Cert storage:**

- Primary copy: GitHub repository secret `MACOS_CERT_P12_BASE64` (base64 of
  the `.p12`) plus `MACOS_CERT_PASSWORD`.
- Backup copy: maintainer's password manager and an offline backup
  (encrypted USB or printout of the base64). The cert *cannot* be
  regenerated identically — losing it forces every existing user to
  re-approve their keychain entry on the next release.
- The cert's SHA-1 hash is committed to the repo as a public reference
  value (so future maintainers can verify which cert produced a given
  signature). Documented in `docs/release-signing.md` (new file).

**Workflow change in `.github/workflows/release.yml`:**

Before the existing "Sign app bundle" step (line 148), add:

```yaml
- name: Import code-signing cert
  env:
    P12_BASE64: ${{ secrets.MACOS_CERT_P12_BASE64 }}
    P12_PASSWORD: ${{ secrets.MACOS_CERT_PASSWORD }}
  run: |
    echo "$P12_BASE64" | base64 --decode > cert.p12
    security create-keychain -p "" build.keychain
    security default-keychain -s build.keychain
    security unlock-keychain -p "" build.keychain
    security import cert.p12 -k build.keychain -P "$P12_PASSWORD" \
      -T /usr/bin/codesign
    security set-key-partition-list -S apple-tool:,apple: -s -k "" build.keychain
    rm cert.p12
```

Replace the "Sign app bundle" step body with:

```bash
codesign --force --deep --sign "NeubiBackup Code Signing" NeubiBackup.app
codesign --verify --deep --strict --verbose=2 NeubiBackup.app
codesign -dv --verbose=4 NeubiBackup.app
```

Notes:

- No `--options runtime` (hardened runtime). It buys nothing without
  notarization and can break Go programs.
- No `--timestamp`. The 100-year cert lifetime makes timestamping
  unnecessary; can be added later if desired.
- `--deep` recursively signs the embedded restic binary. The plan should
  spot-check that `codesign --verify` validates the inner binary.

**Bundle identifier and DR shape:**

The existing `Info.plist` already sets `CFBundleIdentifier` to
`com.neubibackup.app`. The DR after signing will resolve to roughly:

```
identifier "com.neubibackup.app" and certificate root = H"<cert SHA-1>"
```

Both halves are stable across releases as long as the cert is reused. The
keychain feature relies on this shape; if `CFBundleIdentifier` ever
changes, every existing keychain ACL becomes invalid. Treated as a
versioned constant, like a database schema.

**Local dev builds:**

`scripts/build-dev-app.sh` keeps `codesign --sign -` (ad-hoc). Developers
testing the keychain feature locally will see a prompt every rebuild
because the cdhash changes. Acceptable for development; the in-memory
test backend (see "Tests" below) is the primary harness for the keychain
package, so most testing doesn't even touch the real keychain.

**Windows:** unchanged. Wincred has no per-app ACL; signing affects
nothing about credential persistence.

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
  - "Password prompt after rebuilding from source" — local dev builds are
    ad-hoc signed, so the cdhash changes every build. Click "Always Allow"
    once or re-run `set-password`. Does not affect installed releases.
  - "Keychain not available on Linux" — by design.
  - macOS: explicit comparison with `password_command: security ...` and why
    `use_keychain` is preferred.

## Risks / Open Questions

- **Cert loss is unrecoverable.** The self-signed cert's hash is baked into
  every existing user's keychain ACL. If the `.p12` is lost, the next
  release signs with a different cert hash, no existing keychain entry
  matches, and every user gets a one-time prompt to re-approve. There is
  no way to "regenerate the same self-signed cert" — its hash is a function
  of the (unrecoverable) private key. Mitigated by storing the `.p12` in
  the maintainer's password manager and an offline backup in addition to
  the GitHub secret.
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
- **Gatekeeper UX is unchanged.** Self-signing does not get past Gatekeeper's
  "unidentified developer" warning on first launch. Same as today's ad-hoc
  state. Users still right-click → Open the first time. Documented in the
  README's installation section (existing copy already covers this).

## Out of Scope (for v1)

- Per-field secret storage for Pushover, Tailscale, healthchecks URLs.
- Multi-account / multi-user keychain entries beyond the per-repo-path key.
- GUI-based password change without going through the tray (e.g., a settings
  window). The tray prompt is sufficient.
- Linux Secret Service support.
- Apple Developer ID signing, hardened runtime, notarization, stapling. Not
  planned.
