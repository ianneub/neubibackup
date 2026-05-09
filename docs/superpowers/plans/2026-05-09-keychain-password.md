# Keychain Password Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a fourth restic-password source `repository.use_keychain: true` that stores the password in the macOS Keychain or Windows Credential Manager under neubibackup's own code-signing identity, plus the `set-password` / `clear-password` CLI subcommands and tray menu entries to manage it.

**Architecture:** New `internal/keychain` package with platform-specific files (cgo on macOS, pure Go via `wincred` on Windows, stub elsewhere). The runner reads the password through a `passwordSource` interface so tests can inject a fake. Two new top-level subcommands route through the same `keychain.Set` / `Delete` API as the tray menu items. Release signing switches from ad-hoc to a stable self-signed cert so the macOS Keychain ACL stays valid across auto-updates.

**Tech Stack:** Go 1.26, `github.com/keybase/go-keychain` (macOS, cgo), `github.com/danieljoos/wincred` (Windows, pure Go), `golang.org/x/term` (already an indirect dep), `osascript` (macOS dialog), PowerShell + WinForms (Windows dialog).

**Spec:** `docs/superpowers/specs/2026-05-09-keychain-password-design.md`

---

## File Structure

**New files:**

- `internal/keychain/keychain.go` — package doc, exported errors, service constant, exported `Get` / `Set` / `Delete` signatures wired to platform implementations.
- `internal/keychain/keychain_darwin.go` — cgo + `keybase/go-keychain` implementation (build tag `darwin`).
- `internal/keychain/keychain_windows.go` — `danieljoos/wincred` implementation (build tag `windows`).
- `internal/keychain/keychain_other.go` — stub returning `ErrUnsupported` (build tag `!darwin && !windows`).
- `internal/keychain/keychain_darwin_test.go` — round-trip test (build tag `darwin`).
- `internal/keychain/keychain_windows_test.go` — round-trip test (build tag `windows`).
- `internal/keychain/keychain_other_test.go` — `ErrUnsupported` assertion (build tag `!darwin && !windows`).
- `internal/keychain/stdinprompt.go` — cross-platform `ReadPasswordStdin` using `golang.org/x/term`.
- `internal/keychain/stdinprompt_test.go` — tests with a fake fd / mock reader.
- `internal/keychain/dialog_darwin.go` — `PromptDialog` via `osascript`.
- `internal/keychain/dialog_windows.go` — `PromptDialog` via PowerShell + WinForms.
- `internal/keychain/dialog_other.go` — `PromptDialog` stub returning `ErrUnsupported`.
- `cmd_password.go` (top-level, sibling of `main.go`) — `set-password` / `clear-password` subcommand entry points.
- `cmd_password_test.go` — subcommand tests with fake keychain backend.
- `docs/release-signing.md` — cert generation, storage, and release-workflow notes.

**Modified files:**

- `internal/config/config.go` — add `UseKeychain bool` to `RepositoryConfig`; update `Validate()` to enforce exactly-one password source.
- `internal/config/config_test.go` — extend validation tests.
- `internal/config/template.go` — add commented-out `use_keychain` example.
- `internal/restic/runner.go` — refactor `buildEnv` to take a `passwordSource` and return `error`; introduce `passwordSource` interface + default keychain-backed implementation; update `runBackupOnce` and `ensureRepositoryExists` to handle the new error.
- `internal/restic/runner_test.go` — update existing `TestBuildEnv` to call new signature; add cases for `use_keychain`.
- `internal/tray/menu.go` — add `OnSetPassword` / `OnClearPassword` callbacks and `UseKeychain` flag to `MenuConfig`; add submenu items.
- `internal/tray/menu_test.go` — extend menu tests.
- `internal/app/app.go` — wire tray callbacks to handlers that call `keychain.PromptDialog` then `keychain.Set` / `Delete`.
- `main.go` — dispatch on first arg to subcommand handler before acquiring single-instance lock or running the tray.
- `README.md` — document `use_keychain`, the subcommands, and the macOS keychain trade-off.
- `.github/workflows/release.yml` — replace ad-hoc signing step with stable self-signed cert step.
- `go.mod` / `go.sum` — new deps via `go mod tidy`.

---

### Task 1: Add dependencies

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add the keychain libraries**

```bash
go get github.com/keybase/go-keychain@latest
go get github.com/danieljoos/wincred@latest
go get golang.org/x/term@latest
```

- [ ] **Step 2: Run go mod tidy**

```bash
go mod tidy
```

- [ ] **Step 3: Verify build still succeeds**

Run: `go build ./...`
Expected: clean build, no errors.

(If you're on Linux without Apple SDK headers, the macOS-tagged file doesn't exist yet, so the build is fine. Don't worry about cross-compilation here yet.)

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add deps for OS keychain support

- github.com/keybase/go-keychain (macOS, cgo)
- github.com/danieljoos/wincred (Windows)
- golang.org/x/term (stdin no-echo password read)"
```

---

### Task 2: Build the `internal/keychain` package skeleton + stub backend

**Files:**
- Create: `internal/keychain/keychain.go`
- Create: `internal/keychain/keychain_other.go`
- Create: `internal/keychain/keychain_other_test.go`

- [ ] **Step 1: Write the package skeleton**

Create `internal/keychain/keychain.go`:

```go
// Package keychain provides cross-platform access to the OS-native credential
// vault (macOS Keychain on darwin, Windows Credential Manager on windows).
//
// Stored items use a fixed service name and an account string supplied by the
// caller (typically a restic repository path), so multiple repositories on the
// same machine get distinct entries.
//
// On platforms other than darwin and windows, all functions return
// ErrUnsupported.
package keychain

import "errors"

// ServiceName is the service identifier used for every item this package
// stores. Do not change without considering migration: existing entries on
// users' machines are keyed on this value.
const ServiceName = "com.neubibackup.repository"

// ErrNotFound is returned when no entry exists for the requested account.
var ErrNotFound = errors.New("keychain: entry not found")

// ErrUnsupported is returned on platforms where the native keychain backend
// is not implemented (anything other than darwin/windows).
var ErrUnsupported = errors.New("keychain: not supported on this platform")
```

- [ ] **Step 2: Write the stub backend**

Create `internal/keychain/keychain_other.go`:

```go
//go:build !darwin && !windows

package keychain

// Get returns ErrUnsupported on non-darwin/non-windows platforms.
func Get(account string) (string, error) {
	return "", ErrUnsupported
}

// Set returns ErrUnsupported on non-darwin/non-windows platforms.
func Set(account, password string) error {
	return ErrUnsupported
}

// Delete returns ErrUnsupported on non-darwin/non-windows platforms.
func Delete(account string) error {
	return ErrUnsupported
}
```

- [ ] **Step 3: Write the stub test**

Create `internal/keychain/keychain_other_test.go`:

```go
//go:build !darwin && !windows

package keychain

import (
	"errors"
	"testing"
)

func TestStubReturnsUnsupported(t *testing.T) {
	if _, err := Get("acct"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Get: got %v, want ErrUnsupported", err)
	}
	if err := Set("acct", "pw"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Set: got %v, want ErrUnsupported", err)
	}
	if err := Delete("acct"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Delete: got %v, want ErrUnsupported", err)
	}
}
```

- [ ] **Step 4: Verify build + test on a Linux-style cross-compile to confirm the stub compiles**

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./internal/keychain/
```

Expected: clean build (the stub is what's selected on linux).

If Go is not installed locally, use Docker per CLAUDE.md:

```bash
docker run --rm -v "$PWD:/workspace" -w /workspace \
  -e GOOS=linux -e GOARCH=amd64 -e CGO_ENABLED=0 \
  golang:1.26.2 go test ./internal/keychain/
```

Expected: `PASS` for `TestStubReturnsUnsupported`.

- [ ] **Step 5: Commit**

```bash
git add internal/keychain/
git commit -m "feat(keychain): add cross-platform keychain package skeleton

Defines exported Get/Set/Delete API plus ErrNotFound/ErrUnsupported.
Provides a stub backend for non-darwin/non-windows platforms so dev
cross-builds (CGO_ENABLED=0 on Linux) stay green."
```

---

### Task 3: macOS keychain backend

**Files:**
- Create: `internal/keychain/keychain_darwin.go`
- Create: `internal/keychain/keychain_darwin_test.go`

- [ ] **Step 1: Write the failing macOS round-trip test**

Create `internal/keychain/keychain_darwin_test.go`:

```go
//go:build darwin

package keychain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

// uniqueAccount returns a random account name so concurrent test runs and
// re-runs after crashes do not collide on a leftover keychain entry.
func uniqueAccount(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return "test-" + hex.EncodeToString(b)
}

func TestRoundTrip(t *testing.T) {
	acct := uniqueAccount(t)
	t.Cleanup(func() { _ = Delete(acct) })

	const pw = "hunter2-correct-horse-battery-staple"

	if err := Set(acct, pw); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := Get(acct)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != pw {
		t.Errorf("Get: %q, want %q", got, pw)
	}

	if err := Delete(acct); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := Get(acct); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: %v, want ErrNotFound", err)
	}
}

func TestSetReplacesExisting(t *testing.T) {
	acct := uniqueAccount(t)
	t.Cleanup(func() { _ = Delete(acct) })

	if err := Set(acct, "first"); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if err := Set(acct, "second"); err != nil {
		t.Fatalf("second Set: %v", err)
	}
	got, err := Get(acct)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "second" {
		t.Errorf("Get: %q, want %q", got, "second")
	}
}

func TestDeleteMissing(t *testing.T) {
	acct := uniqueAccount(t)
	if err := Delete(acct); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing: %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails to compile (no implementation yet)**

Run: `go test ./internal/keychain/ -run TestRoundTrip -v`
Expected: build error — `undefined: Set` etc., because no darwin file exists yet.

- [ ] **Step 3: Write the macOS implementation**

Create `internal/keychain/keychain_darwin.go`:

```go
//go:build darwin

package keychain

import (
	"errors"

	gokeychain "github.com/keybase/go-keychain"
)

// Get retrieves the password for the given account. Returns ErrNotFound if
// no entry exists for (ServiceName, account).
func Get(account string) (string, error) {
	q := gokeychain.NewItem()
	q.SetSecClass(gokeychain.SecClassGenericPassword)
	q.SetService(ServiceName)
	q.SetAccount(account)
	q.SetMatchLimit(gokeychain.MatchLimitOne)
	q.SetReturnData(true)

	results, err := gokeychain.QueryItem(q)
	if err != nil {
		if errors.Is(err, gokeychain.ErrorItemNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	if len(results) == 0 {
		return "", ErrNotFound
	}
	return string(results[0].Data), nil
}

// Set stores or replaces the password for the given account. The Keychain
// ACL is created with the calling process's designated requirement; for
// stable cross-release ACLs, the binary must be signed with a stable code
// signing identity (see docs/release-signing.md).
func Set(account, password string) error {
	// Best-effort delete first so the new entry's ACL is owned by us, not
	// inherited from a prior process. ErrNotFound is fine.
	if err := Delete(account); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	item := gokeychain.NewItem()
	item.SetSecClass(gokeychain.SecClassGenericPassword)
	item.SetService(ServiceName)
	item.SetAccount(account)
	item.SetData([]byte(password))
	item.SetSynchronizable(gokeychain.SynchronizableNo)
	item.SetAccessible(gokeychain.AccessibleWhenUnlocked)

	if err := gokeychain.AddItem(item); err != nil {
		return err
	}
	return nil
}

// Delete removes the keychain entry for the given account. Returns
// ErrNotFound if no entry exists.
func Delete(account string) error {
	q := gokeychain.NewItem()
	q.SetSecClass(gokeychain.SecClassGenericPassword)
	q.SetService(ServiceName)
	q.SetAccount(account)
	if err := gokeychain.DeleteItem(q); err != nil {
		if errors.Is(err, gokeychain.ErrorItemNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run the tests on macOS**

Run: `go test ./internal/keychain/ -v`
Expected: `PASS` for all three tests.

If you see `Failed to access keychain` or a UI prompt, your tests are running outside a logged-in user session. They must run as your user with the login keychain unlocked.

- [ ] **Step 5: Commit**

```bash
git add internal/keychain/keychain_darwin.go internal/keychain/keychain_darwin_test.go
git commit -m "feat(keychain): macOS Keychain backend via Security.framework

Uses keybase/go-keychain (cgo wrapper around Security.framework) so the
ACL on the keychain item binds to neubibackup's own code-signing
identity. Stable across releases when the binary is signed with the
self-signed cert documented in docs/release-signing.md."
```

---

### Task 4: Windows keychain backend

**Files:**
- Create: `internal/keychain/keychain_windows.go`
- Create: `internal/keychain/keychain_windows_test.go`

- [ ] **Step 1: Write the failing Windows round-trip test**

Create `internal/keychain/keychain_windows_test.go`:

```go
//go:build windows

package keychain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

func uniqueAccount(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return "test-" + hex.EncodeToString(b)
}

func TestRoundTrip(t *testing.T) {
	acct := uniqueAccount(t)
	t.Cleanup(func() { _ = Delete(acct) })

	const pw = "hunter2-correct-horse-battery-staple"

	if err := Set(acct, pw); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := Get(acct)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != pw {
		t.Errorf("Get: %q, want %q", got, pw)
	}

	if err := Delete(acct); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := Get(acct); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: %v, want ErrNotFound", err)
	}
}

func TestSetReplacesExisting(t *testing.T) {
	acct := uniqueAccount(t)
	t.Cleanup(func() { _ = Delete(acct) })

	if err := Set(acct, "first"); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if err := Set(acct, "second"); err != nil {
		t.Fatalf("second Set: %v", err)
	}
	got, err := Get(acct)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "second" {
		t.Errorf("Get: %q, want %q", got, "second")
	}
}

func TestDeleteMissing(t *testing.T) {
	acct := uniqueAccount(t)
	if err := Delete(acct); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing: %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails to compile**

Run on Windows: `go test ./internal/keychain/ -v`
Expected: build error — `undefined: Set` etc.

- [ ] **Step 3: Write the Windows implementation**

Create `internal/keychain/keychain_windows.go`:

```go
//go:build windows

package keychain

import (
	"errors"
	"syscall"

	"github.com/danieljoos/wincred"
)

// targetName builds the credential target name. Format:
//   "<service>:<account>"
// so multiple repositories get distinct entries.
func targetName(account string) string {
	return ServiceName + ":" + account
}

// Get retrieves the password for the given account. Returns ErrNotFound if
// no credential exists.
func Get(account string) (string, error) {
	cred, err := wincred.GetGenericCredential(targetName(account))
	if err != nil {
		// wincred returns syscall.Errno(0x80070490) (ERROR_NOT_FOUND)
		// when the credential is missing.
		var errno syscall.Errno
		if errors.As(err, &errno) && uint32(errno) == 0x80070490 {
			return "", ErrNotFound
		}
		return "", err
	}
	return string(cred.CredentialBlob), nil
}

// Set stores or replaces the password for the given account.
func Set(account, password string) error {
	cred := wincred.NewGenericCredential(targetName(account))
	cred.CredentialBlob = []byte(password)
	cred.UserName = account
	cred.Persist = wincred.PersistLocalMachine
	return cred.Write()
}

// Delete removes the credential for the given account. Returns ErrNotFound
// if it doesn't exist.
func Delete(account string) error {
	cred, err := wincred.GetGenericCredential(targetName(account))
	if err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) && uint32(errno) == 0x80070490 {
			return ErrNotFound
		}
		return err
	}
	return cred.Delete()
}
```

- [ ] **Step 4: Run the tests on Windows**

Run on Windows: `go test ./internal/keychain/ -v`
Expected: `PASS` for all three tests.

- [ ] **Step 5: Commit**

```bash
git add internal/keychain/keychain_windows.go internal/keychain/keychain_windows_test.go
git commit -m "feat(keychain): Windows Credential Manager backend via wincred

Pure-Go wrapper around the Win32 Credential Manager API. Credentials
are persisted at LocalMachine scope; protection is DPAPI at rest plus
login-session gating (no per-app ACL — that's a Windows OS limitation,
not specific to this implementation)."
```

---

### Task 5: Cross-platform stdin password reader

**Files:**
- Create: `internal/keychain/stdinprompt.go`
- Create: `internal/keychain/stdinprompt_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/keychain/stdinprompt_test.go`:

```go
package keychain

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadPasswordStdinEcho(t *testing.T) {
	// When stdin is not a terminal (typical test environment), the function
	// reads a line of plain text. We exercise that path by passing an
	// io.Reader.
	in := strings.NewReader("hunter2\n")
	var promptOut bytes.Buffer

	pw, err := readPasswordFrom(in, &promptOut, "Enter password: ")
	if err != nil {
		t.Fatalf("readPasswordFrom: %v", err)
	}
	if pw != "hunter2" {
		t.Errorf("password = %q, want %q", pw, "hunter2")
	}
	if !strings.Contains(promptOut.String(), "Enter password:") {
		t.Errorf("prompt not written; got %q", promptOut.String())
	}
}

func TestReadPasswordStdinTrimsCRLF(t *testing.T) {
	in := strings.NewReader("secret\r\n")
	var promptOut bytes.Buffer

	pw, err := readPasswordFrom(in, &promptOut, "Pw: ")
	if err != nil {
		t.Fatalf("readPasswordFrom: %v", err)
	}
	if pw != "secret" {
		t.Errorf("password = %q, want %q", pw, "secret")
	}
}

func TestReadPasswordStdinEmpty(t *testing.T) {
	in := strings.NewReader("\n")
	var promptOut bytes.Buffer

	pw, err := readPasswordFrom(in, &promptOut, "Pw: ")
	if err != nil {
		t.Fatalf("readPasswordFrom: %v", err)
	}
	if pw != "" {
		t.Errorf("password = %q, want empty", pw)
	}
}
```

- [ ] **Step 2: Run the test, confirm it fails to compile**

Run: `go test ./internal/keychain/ -run TestReadPasswordStdin -v`
Expected: build error — `undefined: readPasswordFrom`.

- [ ] **Step 3: Write the implementation**

Create `internal/keychain/stdinprompt.go`:

```go
package keychain

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadPasswordStdin prompts the user on os.Stderr and reads a password
// from os.Stdin. When stdin is a terminal, input is read with echo
// disabled. When stdin is a pipe (e.g., scripted use), a newline-terminated
// line is read with normal buffering.
func ReadPasswordStdin(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(fd)
		// Always emit a newline after a no-echo read so the next line of
		// terminal output isn't on the same row as the (invisible) input.
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	return readPasswordFrom(os.Stdin, os.Stderr, prompt)
}

// readPasswordFrom is the non-terminal code path, factored out so tests
// can supply their own reader/writer.
func readPasswordFrom(in io.Reader, promptOut io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(promptOut, prompt); err != nil {
		return "", err
	}
	r := bufio.NewReader(in)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/keychain/ -run TestReadPasswordStdin -v`
Expected: `PASS` for all three.

- [ ] **Step 5: Commit**

```bash
git add internal/keychain/stdinprompt.go internal/keychain/stdinprompt_test.go
git commit -m "feat(keychain): add ReadPasswordStdin for CLI subcommands"
```

---

### Task 6: GUI password dialogs (macOS + Windows + stub)

**Files:**
- Create: `internal/keychain/dialog_darwin.go`
- Create: `internal/keychain/dialog_windows.go`
- Create: `internal/keychain/dialog_other.go`

This task ships all three at once because they share the function signature defined by the build-tagged files. There are no automated tests — the dialogs require a real desktop session. Manual smoke test instructions are included.

- [ ] **Step 1: Write the macOS dialog**

Create `internal/keychain/dialog_darwin.go`:

```go
//go:build darwin

package keychain

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrPromptCancelled is returned when the user dismisses the dialog
// without entering a password (Cancel button or empty input).
var ErrPromptCancelled = errors.New("keychain: prompt cancelled")

// PromptDialog shows a native password dialog and returns what the user
// typed. Returns ErrPromptCancelled if the user cancels or supplies an
// empty value.
//
// title: dialog window title (kept short)
// message: prompt text shown above the input field
func PromptDialog(title, message string) (string, error) {
	// AppleScript escapes: replace " with \" inside our literals.
	escTitle := strings.ReplaceAll(title, `"`, `\"`)
	escMsg := strings.ReplaceAll(message, `"`, `\"`)

	script := fmt.Sprintf(
		`display dialog "%s" with title "%s" default answer "" with hidden answer buttons {"Cancel","OK"} default button "OK"`,
		escMsg, escTitle,
	)

	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		// User clicking Cancel → osascript exits non-zero with
		// "User canceled. (-128)" on stderr. Treat any failure as cancel
		// (we don't want to leak osascript internals to the user).
		return "", ErrPromptCancelled
	}

	// osascript output looks like:
	//   button returned:OK, text returned:thepassword
	s := strings.TrimRight(string(out), "\r\n")
	const marker = "text returned:"
	idx := strings.Index(s, marker)
	if idx < 0 {
		return "", ErrPromptCancelled
	}
	pw := s[idx+len(marker):]
	if pw == "" {
		return "", ErrPromptCancelled
	}
	return pw, nil
}
```

- [ ] **Step 2: Write the Windows dialog**

Create `internal/keychain/dialog_windows.go`:

```go
//go:build windows

package keychain

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
)

// ErrPromptCancelled is returned when the user dismisses the dialog
// without entering a password (Cancel button or empty input).
var ErrPromptCancelled = errors.New("keychain: prompt cancelled")

// promptScript renders a small WinForms password dialog. Output is the
// plain-text password on stdout, or empty on cancel.
const promptScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

$form = New-Object System.Windows.Forms.Form
$form.Text = $env:NB_PROMPT_TITLE
$form.Size = New-Object System.Drawing.Size(420,170)
$form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$form.TopMost = $true

$label = New-Object System.Windows.Forms.Label
$label.Location = New-Object System.Drawing.Point(12,15)
$label.Size = New-Object System.Drawing.Size(390,30)
$label.Text = $env:NB_PROMPT_MESSAGE
$form.Controls.Add($label)

$txt = New-Object System.Windows.Forms.TextBox
$txt.Location = New-Object System.Drawing.Point(12,50)
$txt.Size = New-Object System.Drawing.Size(390,20)
$txt.UseSystemPasswordChar = $true
$form.Controls.Add($txt)

$ok = New-Object System.Windows.Forms.Button
$ok.Location = New-Object System.Drawing.Point(245,90)
$ok.Size = New-Object System.Drawing.Size(75,25)
$ok.Text = 'OK'
$ok.DialogResult = [System.Windows.Forms.DialogResult]::OK
$form.AcceptButton = $ok
$form.Controls.Add($ok)

$cancel = New-Object System.Windows.Forms.Button
$cancel.Location = New-Object System.Drawing.Point(327,90)
$cancel.Size = New-Object System.Drawing.Size(75,25)
$cancel.Text = 'Cancel'
$cancel.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
$form.CancelButton = $cancel
$form.Controls.Add($cancel)

$form.Add_Shown({ $txt.Focus() | Out-Null })
$result = $form.ShowDialog()
if ($result -eq [System.Windows.Forms.DialogResult]::OK) {
    [Console]::Out.Write($txt.Text)
    exit 0
}
exit 1
`

// PromptDialog shows a native password dialog and returns the entered
// password. Returns ErrPromptCancelled on cancel or empty input.
func PromptDialog(title, message string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
		"-WindowStyle", "Hidden", "-Command", promptScript)
	cmd.Env = append(cmd.Env,
		"NB_PROMPT_TITLE="+title,
		"NB_PROMPT_MESSAGE="+message,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	out, err := cmd.Output()
	if err != nil {
		// Non-zero exit (Cancel button pressed) or any other failure.
		return "", ErrPromptCancelled
	}
	pw := strings.TrimRight(string(out), "\r\n")
	if pw == "" {
		return "", ErrPromptCancelled
	}
	return pw, nil
}
```

- [ ] **Step 3: Write the stub**

Create `internal/keychain/dialog_other.go`:

```go
//go:build !darwin && !windows

package keychain

import "errors"

// ErrPromptCancelled is returned when the user dismisses the dialog. On
// stub platforms the dialog is unavailable, so PromptDialog always
// returns ErrUnsupported instead.
var ErrPromptCancelled = errors.New("keychain: prompt cancelled")

// PromptDialog is unavailable on this platform.
func PromptDialog(title, message string) (string, error) {
	return "", ErrUnsupported
}
```

- [ ] **Step 4: Verify the package builds on each platform**

Run on macOS: `go build ./internal/keychain/`
Expected: clean.

Run on a stub-target (Linux Docker per CLAUDE.md):
```bash
docker run --rm -v "$PWD:/workspace" -w /workspace \
  -e GOOS=linux -e GOARCH=amd64 -e CGO_ENABLED=0 \
  golang:1.26.2 go build ./internal/keychain/
```
Expected: clean.

- [ ] **Step 5: Manual smoke test (macOS)**

Write a tiny throwaway in a scratch file `cmd/dialogtest/main.go`:

```go
//go:build darwin

package main

import (
	"fmt"
	"neubibackup/internal/keychain"
)

func main() {
	pw, err := keychain.PromptDialog("NeubiBackup", "Enter your repository password:")
	fmt.Printf("err=%v len=%d\n", err, len(pw))
}
```

Run: `go run ./cmd/dialogtest/`
Expected: a system dialog appears, accepts hidden input, prints the length on OK and prints `err=keychain: prompt cancelled len=0` on Cancel.

Delete `cmd/dialogtest/` after verifying. (Don't commit it.)

```bash
rm -rf cmd/dialogtest
```

- [ ] **Step 6: Commit**

```bash
git add internal/keychain/dialog_darwin.go internal/keychain/dialog_windows.go internal/keychain/dialog_other.go
git commit -m "feat(keychain): native password prompt dialogs

macOS uses osascript display dialog with hidden answer; Windows uses a
PowerShell-driven WinForms dialog with UseSystemPasswordChar. Both
return ErrPromptCancelled on Cancel or empty input."
```

---

### Task 7: Add `UseKeychain` to config + validation

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing validation tests**

In `internal/config/config_test.go`, find the existing `TestConfigValidation` block (search for `func TestConfigValidation`). Append the following cases inside its `tests := []struct{...}` slice, just before the closing `}`:

```go
{
    name: "valid config with use_keychain",
    cfg: config.Config{
        Version: 2,
        Repository: config.RepositoryConfig{
            Path:        "/backup/repo",
            UseKeychain: true,
        },
        Backup: config.BackupConfig{
            Paths: []string{"/home"},
        },
        Schedule: config.ScheduleConfig{
            Cron: "@every 24h",
        },
    },
    wantErr: false,
},
{
    name: "rejects use_keychain plus password",
    cfg: config.Config{
        Version: 2,
        Repository: config.RepositoryConfig{
            Path:        "/backup/repo",
            Password:    "secret",
            UseKeychain: true,
        },
        Backup: config.BackupConfig{
            Paths: []string{"/home"},
        },
        Schedule: config.ScheduleConfig{
            Cron: "@every 24h",
        },
    },
    wantErr: true,
},
{
    name: "rejects use_keychain plus password_file",
    cfg: config.Config{
        Version: 2,
        Repository: config.RepositoryConfig{
            Path:         "/backup/repo",
            PasswordFile: "/tmp/p",
            UseKeychain:  true,
        },
        Backup: config.BackupConfig{
            Paths: []string{"/home"},
        },
        Schedule: config.ScheduleConfig{
            Cron: "@every 24h",
        },
    },
    wantErr: true,
},
{
    name: "rejects password plus password_file",
    cfg: config.Config{
        Version: 2,
        Repository: config.RepositoryConfig{
            Path:         "/backup/repo",
            Password:     "x",
            PasswordFile: "/tmp/p",
        },
        Backup: config.BackupConfig{
            Paths: []string{"/home"},
        },
        Schedule: config.ScheduleConfig{
            Cron: "@every 24h",
        },
    },
    wantErr: true,
},
```

- [ ] **Step 2: Run the test to confirm failures**

Run: `go test ./internal/config/ -run TestConfigValidation -v`
Expected: the new "valid config with use_keychain" case fails because the field doesn't exist yet (compile error).

- [ ] **Step 3: Add the field**

In `internal/config/config.go`, modify `RepositoryConfig`:

```go
// RepositoryConfig defines the restic repository settings.
type RepositoryConfig struct {
	Path            string `yaml:"path"`             // Repository path or URL
	Password        string `yaml:"password"`         // Password directly (less secure)
	PasswordFile    string `yaml:"password_file"`    // Path to password file
	PasswordCommand string `yaml:"password_command"` // Command to get password
	UseKeychain     bool   `yaml:"use_keychain"`     // Read/write password from OS keychain
}
```

- [ ] **Step 4: Tighten `Validate()` to enforce exactly-one source**

In `internal/config/config.go`, replace the existing password check inside `Validate()`:

```go
if c.Repository.Password == "" && c.Repository.PasswordFile == "" && c.Repository.PasswordCommand == "" {
    return fmt.Errorf("repository.password, repository.password_file, or repository.password_command is required")
}
```

with:

```go
sources := 0
if c.Repository.Password != "" {
    sources++
}
if c.Repository.PasswordFile != "" {
    sources++
}
if c.Repository.PasswordCommand != "" {
    sources++
}
if c.Repository.UseKeychain {
    sources++
}
switch {
case sources == 0:
    return fmt.Errorf("one of repository.password, repository.password_file, repository.password_command, or repository.use_keychain is required")
case sources > 1:
    return fmt.Errorf("exactly one of repository.password, repository.password_file, repository.password_command, or repository.use_keychain may be set")
}
```

- [ ] **Step 5: Run the validation tests**

Run: `go test ./internal/config/ -run TestConfigValidation -v`
Expected: all sub-cases `PASS`, including the new ones.

- [ ] **Step 6: Run the full config package tests to make sure nothing else broke**

Run: `go test ./internal/config/ -v`
Expected: all `PASS`.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add use_keychain password source

Adds repository.use_keychain bool. Validate() now requires exactly one
of password, password_file, password_command, or use_keychain (was: at
least one of the first three)."
```

---

### Task 8: Refactor `buildEnv` to take a passwordSource and return error

**Files:**
- Modify: `internal/restic/runner.go`
- Modify: `internal/restic/runner_test.go`

- [ ] **Step 1: Update the existing `TestBuildEnv` to expect the new signature**

In `internal/restic/runner_test.go`, find `TestBuildEnv`. Replace the call inside the `t.Run` block:

```go
env := buildEnv(tt.cfg, tt.proxyAddr)
```

with:

```go
env, err := buildEnv(tt.cfg, tt.proxyAddr, fakePasswordSource{})
if err != nil {
    t.Fatalf("buildEnv: %v", err)
}
```

Then, at the bottom of `runner_test.go` (outside any `func`), add:

```go
// fakePasswordSource is used by tests that don't exercise the
// use_keychain code path. It panics if called.
type fakePasswordSource struct{}

func (fakePasswordSource) Get(account string) (string, error) {
    panic("fakePasswordSource.Get called unexpectedly")
}
```

- [ ] **Step 2: Run the test to confirm it fails to compile**

Run: `go test ./internal/restic/ -run TestBuildEnv -v`
Expected: build error — `buildEnv` signature mismatch.

- [ ] **Step 3: Refactor `buildEnv`**

In `internal/restic/runner.go`, add an import for the new keychain package at the top:

```go
import (
    // ...existing imports...
    "neubibackup/internal/keychain"
)
```

Define the interface and default backend right above the `buildEnv` function:

```go
// passwordSource abstracts where we get the repository password from
// when use_keychain is true. The default reads from the OS keychain;
// tests inject a fake.
type passwordSource interface {
    Get(account string) (string, error)
}

type keychainSource struct{}

func (keychainSource) Get(account string) (string, error) {
    return keychain.Get(account)
}

// defaultPasswordSource is overridable by tests via package init or by
// future plumbing if a non-keychain backend is added.
var defaultPasswordSource passwordSource = keychainSource{}
```

Replace the existing `buildEnv` function with:

```go
func buildEnv(cfg *config.Config, proxyAddr string, src passwordSource) ([]string, error) {
    var env []string

    env = append(env, "RESTIC_REPOSITORY="+cfg.Repository.Path)

    switch {
    case cfg.Repository.UseKeychain:
        pw, err := src.Get(cfg.Repository.Path)
        if err != nil {
            return nil, fmt.Errorf("%w: keychain: %v", ErrPasswordFailed, err)
        }
        env = append(env, "RESTIC_PASSWORD="+pw)
    case cfg.Repository.Password != "":
        env = append(env, "RESTIC_PASSWORD="+cfg.Repository.Password)
    }

    if proxyAddr != "" {
        env = append(env, "HTTP_PROXY=socks5://"+proxyAddr)
        env = append(env, "HTTPS_PROXY=socks5://"+proxyAddr)
    }

    return env, nil
}
```

- [ ] **Step 4: Update `buildEnv` callers**

Find every call to `buildEnv` in `internal/restic/runner.go` (there are three: in `runBackupOnce`, `ensureRepositoryExists` (twice — once for `snapshots`, once for `init`)). Replace each:

```go
cmd.Env = append(os.Environ(), buildEnv(cfg, proxyAddr)...)
```

with:

```go
envExtra, err := buildEnv(cfg, proxyAddr, defaultPasswordSource)
if err != nil {
    return err
}
cmd.Env = append(os.Environ(), envExtra...)
```

In `runBackupOnce` the surrounding function already returns `error`, so this just works. In `ensureRepositoryExists` the same is true. The `init` block also already returns `error`. Verify all three sites compile.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/restic/ -run TestBuildEnv -v`
Expected: all sub-cases `PASS` (the existing assertions still hold; we just routed them through a fake source).

- [ ] **Step 6: Run the full restic package tests**

Run: `go test ./internal/restic/ -v`
Expected: all `PASS`.

- [ ] **Step 7: Commit**

```bash
git add internal/restic/runner.go internal/restic/runner_test.go
git commit -m "refactor(restic): introduce passwordSource interface in buildEnv

Threads a passwordSource through buildEnv so the use_keychain path can
look up the password without coupling the runner to the keychain
package directly. buildEnv now returns an error so a keychain miss
surfaces as ErrPasswordFailed (no retry). No behavior change for
existing password sources."
```

---

### Task 9: Add `use_keychain` test cases to runner

**Files:**
- Modify: `internal/restic/runner_test.go`

- [ ] **Step 1: Add a stubbed source and use_keychain test cases**

At the bottom of `runner_test.go`, replace the existing `fakePasswordSource` (added in Task 8) with a more flexible one:

```go
// fakePasswordSource is a configurable source for runner tests.
type fakePasswordSource struct {
    pw  string
    err error
}

func (f fakePasswordSource) Get(account string) (string, error) {
    if f.err != nil {
        return "", f.err
    }
    return f.pw, nil
}
```

- [ ] **Step 2: Add a dedicated test for `use_keychain` env behavior**

In `internal/restic/runner_test.go`, add a new test function:

```go
func TestBuildEnvUseKeychain(t *testing.T) {
    cfg := &config.Config{
        Repository: config.RepositoryConfig{
            Path:        "/backup/repo",
            UseKeychain: true,
        },
    }

    env, err := buildEnv(cfg, "", fakePasswordSource{pw: "from-keychain"})
    if err != nil {
        t.Fatalf("buildEnv: %v", err)
    }

    var gotRepo, gotPw string
    var sawPw bool
    for _, e := range env {
        if strings.HasPrefix(e, "RESTIC_REPOSITORY=") {
            gotRepo = strings.TrimPrefix(e, "RESTIC_REPOSITORY=")
        }
        if strings.HasPrefix(e, "RESTIC_PASSWORD=") {
            gotPw = strings.TrimPrefix(e, "RESTIC_PASSWORD=")
            sawPw = true
        }
    }
    if gotRepo != "/backup/repo" {
        t.Errorf("RESTIC_REPOSITORY = %q, want /backup/repo", gotRepo)
    }
    if !sawPw {
        t.Error("RESTIC_PASSWORD missing")
    }
    if gotPw != "from-keychain" {
        t.Errorf("RESTIC_PASSWORD = %q, want from-keychain", gotPw)
    }
}

func TestBuildEnvKeychainMissError(t *testing.T) {
    cfg := &config.Config{
        Repository: config.RepositoryConfig{
            Path:        "/backup/repo",
            UseKeychain: true,
        },
    }

    _, err := buildEnv(cfg, "", fakePasswordSource{err: errors.New("not found")})
    if err == nil {
        t.Fatal("buildEnv: nil error, want ErrPasswordFailed")
    }
    if !errors.Is(err, ErrPasswordFailed) {
        t.Errorf("buildEnv error = %v, want errors.Is ErrPasswordFailed", err)
    }
}
```

You'll need `"errors"` and `"strings"` in the test file's imports if they aren't already there. Add them to the import block.

- [ ] **Step 3: Add a `buildBackupArgs` test case for `use_keychain`**

Find `TestBuildBackupArgs` in `runner_test.go`. Append this case to the `tests` slice:

```go
{
    name: "with use_keychain",
    cfg: &config.Config{
        Repository: config.RepositoryConfig{
            Path:        "/backup/repo",
            UseKeychain: true,
        },
        Backup: config.BackupConfig{
            Paths: []string{"/home"},
        },
    },
    contains: []string{
        "-r", "/backup/repo",
    },
    excludes: []string{
        "--password-file",
        "--password-command",
    },
},
```

This guarantees no `--password-file` / `--password-command` flag leaks through when `use_keychain` is set.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/restic/ -v`
Expected: `PASS` including the three new tests.

- [ ] **Step 5: Commit**

```bash
git add internal/restic/runner_test.go
git commit -m "test(restic): cover use_keychain in buildEnv and buildBackupArgs

Verifies use_keychain sources RESTIC_PASSWORD via the injected
passwordSource, surfaces ErrPasswordFailed on keychain miss, and does
not pass --password-file or --password-command flags to restic."
```

---

### Task 10: `set-password` and `clear-password` subcommands

**Files:**
- Create: `cmd_password.go`
- Create: `cmd_password_test.go`
- Modify: `main.go`

- [ ] **Step 1: Write the failing subcommand tests**

Create `cmd_password_test.go`:

```go
package main

import (
	"errors"
	"strings"
	"testing"

	"neubibackup/internal/config"
	"neubibackup/internal/keychain"
)

// fakeKeychain implements the keychainBackend interface used by the
// subcommand handlers.
type fakeKeychain struct {
	store map[string]string
	getErr error
	setErr error
	delErr error
}

func newFakeKeychain() *fakeKeychain {
	return &fakeKeychain{store: map[string]string{}}
}

func (f *fakeKeychain) Get(account string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	v, ok := f.store[account]
	if !ok {
		return "", keychain.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeychain) Set(account, password string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.store[account] = password
	return nil
}

func (f *fakeKeychain) Delete(account string) error {
	if f.delErr != nil {
		return f.delErr
	}
	if _, ok := f.store[account]; !ok {
		return keychain.ErrNotFound
	}
	delete(f.store, account)
	return nil
}

func TestSetPasswordStoresInKeychain(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{Path: "/backup/repo"},
	}
	kc := newFakeKeychain()
	var stderr strings.Builder

	read := func(prompt string) (string, error) {
		if !strings.Contains(prompt, "password") {
			t.Errorf("prompt = %q, expected 'password' substring", prompt)
		}
		return "supersecret", nil
	}

	rc := runSetPassword(cfg, kc, read, &stderr)
	if rc != 0 {
		t.Fatalf("runSetPassword rc = %d, want 0; stderr=%q", rc, stderr.String())
	}
	if got := kc.store["/backup/repo"]; got != "supersecret" {
		t.Errorf("stored = %q, want supersecret", got)
	}
}

func TestSetPasswordEmptyRejected(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{Path: "/backup/repo"},
	}
	kc := newFakeKeychain()
	var stderr strings.Builder

	read := func(prompt string) (string, error) { return "", nil }

	rc := runSetPassword(cfg, kc, read, &stderr)
	if rc == 0 {
		t.Errorf("runSetPassword rc = 0, want non-zero on empty input")
	}
	if !strings.Contains(stderr.String(), "empty") {
		t.Errorf("stderr = %q, want mention of 'empty'", stderr.String())
	}
	if len(kc.store) != 0 {
		t.Errorf("store unexpectedly written: %v", kc.store)
	}
}

func TestSetPasswordRequiresRepoPath(t *testing.T) {
	cfg := &config.Config{}
	kc := newFakeKeychain()
	var stderr strings.Builder

	read := func(prompt string) (string, error) { return "ignored", nil }

	rc := runSetPassword(cfg, kc, read, &stderr)
	if rc == 0 {
		t.Error("runSetPassword rc = 0, want non-zero when repository.path is empty")
	}
	if !strings.Contains(stderr.String(), "repository.path") {
		t.Errorf("stderr = %q, want mention of 'repository.path'", stderr.String())
	}
}

func TestClearPasswordRemovesEntry(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{Path: "/backup/repo"},
	}
	kc := newFakeKeychain()
	kc.store["/backup/repo"] = "old"
	var stderr strings.Builder

	rc := runClearPassword(cfg, kc, &stderr)
	if rc != 0 {
		t.Errorf("runClearPassword rc = %d, want 0", rc)
	}
	if _, ok := kc.store["/backup/repo"]; ok {
		t.Error("entry not deleted")
	}
}

func TestClearPasswordIsIdempotent(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{Path: "/backup/repo"},
	}
	kc := newFakeKeychain()
	var stderr strings.Builder

	rc := runClearPassword(cfg, kc, &stderr)
	if rc != 0 {
		t.Errorf("runClearPassword rc = %d, want 0 even when entry missing", rc)
	}
}

func TestClearPasswordSurfacesUnexpectedError(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{Path: "/backup/repo"},
	}
	kc := newFakeKeychain()
	kc.delErr = errors.New("boom")
	var stderr strings.Builder

	rc := runClearPassword(cfg, kc, &stderr)
	if rc == 0 {
		t.Error("runClearPassword rc = 0, want non-zero on backend error")
	}
}
```

- [ ] **Step 2: Run the test to confirm compile failure**

Run: `go test ./... -run TestSetPassword -v`
Expected: build error — `runSetPassword` undefined, etc.

- [ ] **Step 3: Implement the subcommand handlers**

Create `cmd_password.go`:

```go
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"neubibackup/internal/config"
	"neubibackup/internal/keychain"
)

// keychainBackend is the slice of the keychain package consumed by the
// password subcommands. Letting tests inject a fake keeps unit tests off
// the real Keychain.
type keychainBackend interface {
	Get(account string) (string, error)
	Set(account, password string) error
	Delete(account string) error
}

// realKeychain calls into the real keychain package.
type realKeychain struct{}

func (realKeychain) Get(a string) (string, error)  { return keychain.Get(a) }
func (realKeychain) Set(a, p string) error         { return keychain.Set(a, p) }
func (realKeychain) Delete(a string) error         { return keychain.Delete(a) }

// passwordReader is the function signature used by runSetPassword to
// obtain the password (separate from the keychain backend so tests don't
// have to drive a real terminal).
type passwordReader func(prompt string) (string, error)

// dispatchPasswordCmd handles top-level password-related subcommands.
// It is called by main() before the singleinstance lock or tray bootstrap.
// Returns (handled, exitCode). When handled=false the caller should
// continue with the normal flow.
func dispatchPasswordCmd(args []string) (bool, int) {
	if len(args) < 2 {
		return false, 0
	}
	switch args[1] {
	case "set-password":
		cfg, rc := loadCfgForSubcommand(os.Stderr)
		if rc != 0 {
			return true, rc
		}
		return true, runSetPassword(cfg, realKeychain{}, keychain.ReadPasswordStdin, os.Stderr)
	case "clear-password":
		cfg, rc := loadCfgForSubcommand(os.Stderr)
		if rc != 0 {
			return true, rc
		}
		return true, runClearPassword(cfg, realKeychain{}, os.Stderr)
	}
	return false, 0
}

func loadCfgForSubcommand(stderr io.Writer) (*config.Config, int) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "Error loading config: %v\n", err)
		return nil, 1
	}
	return cfg, 0
}

// runSetPassword prompts for a password and stores it in the keychain
// against the configured repository.path. Returns the process exit code.
func runSetPassword(cfg *config.Config, kc keychainBackend, read passwordReader, stderr io.Writer) int {
	if cfg.Repository.Path == "" {
		fmt.Fprintln(stderr, "Error: repository.path is not set in config.yaml")
		return 1
	}

	pw, err := read("Repository password: ")
	if err != nil {
		fmt.Fprintf(stderr, "Error reading password: %v\n", err)
		return 1
	}
	if pw == "" {
		fmt.Fprintln(stderr, "Error: empty password not stored")
		return 1
	}

	if err := kc.Set(cfg.Repository.Path, pw); err != nil {
		fmt.Fprintf(stderr, "Error writing to keychain: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "Password stored for %s\n", cfg.Repository.Path)
	return 0
}

// runClearPassword removes the keychain entry for the configured
// repository.path. Missing entries are treated as success.
func runClearPassword(cfg *config.Config, kc keychainBackend, stderr io.Writer) int {
	if cfg.Repository.Path == "" {
		fmt.Fprintln(stderr, "Error: repository.path is not set in config.yaml")
		return 1
	}

	err := kc.Delete(cfg.Repository.Path)
	switch {
	case err == nil:
		fmt.Fprintf(stderr, "Password cleared for %s\n", cfg.Repository.Path)
		return 0
	case errors.Is(err, keychain.ErrNotFound):
		fmt.Fprintf(stderr, "No password stored for %s\n", cfg.Repository.Path)
		return 0
	default:
		fmt.Fprintf(stderr, "Error clearing keychain: %v\n", err)
		return 1
	}
}
```

- [ ] **Step 4: Wire `dispatchPasswordCmd` into `main()`**

In `main.go`, modify `main()`:

```go
func main() {
	if handled, rc := dispatchPasswordCmd(os.Args); handled {
		os.Exit(rc)
	}

	// Acquire single instance lock before starting
	var err error
	instanceLock, err = singleinstance.Acquire()
	// ...rest unchanged...
}
```

- [ ] **Step 5: Run the new tests**

Run: `go test ./... -run "TestSetPassword|TestClearPassword" -v`
Expected: all `PASS`.

- [ ] **Step 6: Run the full test suite**

Run: `go test ./...`
Expected: all `PASS`.

- [ ] **Step 7: Manual smoke test**

On macOS:
```bash
go build -o neubibackup .
./neubibackup set-password   # type a password, press enter
./neubibackup clear-password # confirms removal
./neubibackup clear-password # second run is a no-op success
```

Don't commit the `neubibackup` binary.

```bash
rm -f neubibackup
```

- [ ] **Step 8: Commit**

```bash
git add cmd_password.go cmd_password_test.go main.go
git commit -m "feat: add set-password and clear-password subcommands

Two new top-level subcommands let users store / remove the restic
repository password in the OS keychain without going through the tray.
Dispatched before the single-instance lock so they work alongside a
running tray instance."
```

---

### Task 11: Tray menu entries for password management

**Files:**
- Modify: `internal/tray/menu.go`
- Modify: `internal/tray/menu_test.go`
- Modify: `internal/app/app.go`

- [ ] **Step 1: Update `MenuConfig` and add menu items**

In `internal/tray/menu.go`, add the following fields to the `MenuConfig` struct (alphabetical position is fine; place them with the other action callbacks):

```go
// UseKeychain reports whether the active config has use_keychain: true.
// When false, the password menu items are disabled.
UseKeychain func() bool

// OnSetPassword is invoked when the user clicks "Set repository
// password…". Implementations should pop the password dialog and write
// the result to the keychain.
OnSetPassword func()

// OnClearPassword is invoked when the user clicks "Clear repository
// password".
OnClearPassword func()
```

Add the menu-item references to the `Menu` struct:

```go
mSetPassword   *systray.MenuItem
mClearPassword *systray.MenuItem
```

In `setup()`, after the `mOpenAppLog` line and before its `systray.AddSeparator()`, insert:

```go
// Password management (only meaningful when use_keychain is enabled)
m.mSetPassword = systray.AddMenuItem("Set repository password…", "Store the restic repository password in the OS keychain")
m.mClearPassword = systray.AddMenuItem("Clear repository password", "Remove the stored repository password from the OS keychain")
m.applyPasswordMenuState()
```

Also add this method on `*Menu`:

```go
// applyPasswordMenuState enables/disables the password menu items based
// on whether the config has use_keychain: true.
func (m *Menu) applyPasswordMenuState() {
    enabled := m.cfg.UseKeychain != nil && m.cfg.UseKeychain()
    if m.mSetPassword != nil {
        if enabled {
            m.mSetPassword.Enable()
        } else {
            m.mSetPassword.Disable()
        }
    }
    if m.mClearPassword != nil {
        if enabled {
            m.mClearPassword.Enable()
        } else {
            m.mClearPassword.Disable()
        }
    }
}
```

In `RefreshOnConfigChange()`, append a call to refresh password state:

```go
func (m *Menu) RefreshOnConfigChange() {
    if m.cfg.IsConfigured() {
        if m.mBackupNow != nil {
            m.mBackupNow.Enable()
        }
    }
    m.applyPasswordMenuState()
    m.UpdateStatus()
}
```

Wire click handling in `eventLoop`. Change the `eventLoop` signature so it accepts the new items, then add cases. Replace:

```go
go m.eventLoop(mOpenConfig, mOpenLogs, mOpenAppLog, mAbout, mQuit)
```

with the same call (no extra args needed, because we use the receiver-stored items). Inside `eventLoop`, add two new cases alongside the others (anywhere before `case <-mQuit.ClickedCh`):

```go
case <-m.mSetPassword.ClickedCh:
    if m.cfg.OnSetPassword != nil {
        m.cfg.OnSetPassword()
    }

case <-m.mClearPassword.ClickedCh:
    if m.cfg.OnClearPassword != nil {
        m.cfg.OnClearPassword()
    }
```

- [ ] **Step 2: Add menu state tests**

In `internal/tray/menu_test.go`, add a test for `applyPasswordMenuState`. The existing tests use a mocked menu setup; follow that pattern. If menu_test.go already has helpers like `newTestMenu`, reuse them. Append:

```go
func TestApplyPasswordMenuStateEnabled(t *testing.T) {
    var enabled bool
    m := &Menu{
        cfg: MenuConfig{
            UseKeychain: func() bool { return enabled },
        },
        // mSetPassword / mClearPassword left nil — applyPasswordMenuState
        // must not panic on nil items.
    }

    enabled = false
    m.applyPasswordMenuState() // must not panic on nil items

    enabled = true
    m.applyPasswordMenuState() // must not panic on nil items
}

func TestMenuConfigUseKeychainNilSafe(t *testing.T) {
    m := &Menu{cfg: MenuConfig{UseKeychain: nil}}
    m.applyPasswordMenuState() // must not panic when getter is nil
}
```

These tests don't exercise the systray library (which can't be driven headlessly); they only verify the nil-safety of the helper. The full path is exercised by the manual smoke test below.

- [ ] **Step 3: Wire the callbacks in `internal/app/app.go`**

Find the `tray.NewMenu(tray.MenuConfig{...})` block (around line 174) and add:

```go
UseKeychain: func() bool {
    return a.cfg != nil && a.cfg.Repository.UseKeychain
},
OnSetPassword:   a.handleSetPassword,
OnClearPassword: a.handleClearPassword,
```

Add the handlers as methods on `*App` somewhere near the other `handle*` methods (e.g., after `handleUpdateClick`):

```go
// handleSetPassword pops a password dialog and stores the entered value
// in the OS keychain against the configured repository path.
func (a *App) handleSetPassword() {
    if a.cfg == nil || a.cfg.Repository.Path == "" {
        slog.Warn("Set password requested but no repository configured")
        return
    }
    pw, err := keychain.PromptDialog("NeubiBackup", "Enter the restic repository password:")
    if err != nil {
        slog.Info("Set password cancelled or failed", "error", err)
        return
    }
    if err := keychain.Set(a.cfg.Repository.Path, pw); err != nil {
        slog.Error("Failed to store password in keychain", "error", err)
        return
    }
    slog.Info("Repository password stored in keychain")
}

// handleClearPassword removes the stored password for the configured
// repository path. Missing entries are treated as success.
func (a *App) handleClearPassword() {
    if a.cfg == nil || a.cfg.Repository.Path == "" {
        slog.Warn("Clear password requested but no repository configured")
        return
    }
    err := keychain.Delete(a.cfg.Repository.Path)
    switch {
    case err == nil:
        slog.Info("Repository password cleared from keychain")
    case errors.Is(err, keychain.ErrNotFound):
        slog.Info("No keychain entry to clear for repository")
    default:
        slog.Error("Failed to delete password from keychain", "error", err)
    }
}
```

Add the new imports to `app.go`:

```go
import (
    // ...existing imports...
    "errors"
    "neubibackup/internal/keychain"
)
```

- [ ] **Step 4: Build and run tests**

Run: `go build ./...`
Run: `go test ./...`
Expected: all `PASS`.

- [ ] **Step 5: Manual smoke test on macOS**

Build a dev `.app`:
```bash
./scripts/build-dev-app.sh
```

Edit `.dev-data/config.yaml` (created on first run) so it has `use_keychain: true` and a real `repository.path`. Run:

```bash
./NeubiBackup.app/Contents/MacOS/neubibackup
```

Click the tray icon. The menu should now show "Set repository password…" and "Clear repository password" (enabled). Click each and verify the password dialog appears and the corresponding `slog` line is written to `.dev-data/app.log`.

- [ ] **Step 6: Commit**

```bash
git add internal/tray/menu.go internal/tray/menu_test.go internal/app/app.go
git commit -m "feat(tray): add 'Set/Clear repository password' menu items

Menu items are enabled only when use_keychain: true. App handlers pop a
native dialog (osascript on macOS, WinForms via PowerShell on Windows)
and route the result through keychain.Set / Delete. Disabled items are
shown but greyed when use_keychain is off, mirroring the rest of the
menu's pattern."
```

---

### Task 12: Documentation — README, config template, troubleshooting

**Files:**
- Modify: `README.md`
- Modify: `internal/config/template.go`

- [ ] **Step 1: Update the config template**

In `internal/config/template.go`, find the `repository:` section in `DefaultConfigTemplate`. After the `password_command` example block (and before the `# What to backup` comment), insert:

```text
  # Or use the OS keychain (macOS Keychain / Windows Credential Manager):
  # Recommended for desktop installs. Set use_keychain: true and run
  # `neubibackup set-password` (CLI) or use the tray menu's
  # "Set repository password..." item to store the password.
  # use_keychain: true
```

- [ ] **Step 2: Update README.md — Features section**

Find the Features section in `README.md` and add a bullet:

```markdown
- Native OS keychain integration on macOS and Windows for storing the restic repository password securely
```

- [ ] **Step 3: Update README.md — Configuration section**

Find the section that describes the existing password options. Add a new subsection (use the document's existing heading style):

````markdown
### Storing the password in the OS keychain

Set `use_keychain: true` in `repository`, leaving the other password fields empty:

```yaml
repository:
  path: "rest:https://user:pass@backup.example.com/repo"
  use_keychain: true
```

Then store the password once. From a terminal:

```bash
neubibackup set-password
```

Or open the tray menu and click **Set repository password…**.

The password is stored in the macOS Keychain (under service `com.neubibackup.repository`) or Windows Credential Manager (target `com.neubibackup.repository:<repo path>`). NeubiBackup reads it on every backup; you don't put it in `config.yaml` or any file.

To remove the stored password, run `neubibackup clear-password` or use the **Clear repository password** tray item.

#### Why prefer this over `password_command: "security ..."` on macOS?

`password_command: security find-generic-password -s neubibackup -w` works, but the keychain ACL on the entry is bound to `/usr/bin/security`. Once you click "Always Allow" the first time, *any* process on your machine that runs `security` reads the password without prompting.

With `use_keychain: true`, NeubiBackup creates and reads the keychain entry through `Security.framework` directly, so the ACL binds to NeubiBackup's own code-signing identity. Other tools (including `/usr/bin/security`) get a fresh prompt. Releases are signed with a stable cert (see `docs/release-signing.md`) so the ACL stays valid across auto-updates.
````

- [ ] **Step 4: Update README.md — Tray Menu Options**

Find the Tray Menu Options section and add bullets describing the two new items:

```markdown
- **Set repository password…** — open a system dialog to store the restic password in the OS keychain. Only enabled when `use_keychain: true`.
- **Clear repository password** — remove the stored keychain entry. Only enabled when `use_keychain: true`.
```

- [ ] **Step 5: Update README.md — Troubleshooting**

Find the Troubleshooting section. Add:

```markdown
### macOS prompts for keychain access after rebuilding from source

Local `go build` produces ad-hoc-signed binaries whose code-signing hash differs from the released app. The Keychain ACL is bound to that hash, so a fresh build looks like a different program. Either click **Always Allow** once for that build, or re-run `neubibackup set-password`. Released versions don't have this problem because they all share a stable signing cert.

### Keychain integration is not available on Linux

`use_keychain` is implemented on macOS and Windows only. On Linux, NeubiBackup falls back to `password`, `password_file`, or `password_command`.
```

- [ ] **Step 6: Verify config template parses**

Run: `go test ./internal/config/ -v`
Expected: all template tests `PASS` (the existing `template_test.go` parses the template).

- [ ] **Step 7: Commit**

```bash
git add README.md internal/config/template.go
git commit -m "docs: document use_keychain, set-password, clear-password

Adds the new password source to the config template, README features /
configuration / tray-menu / troubleshooting sections, plus a comparison
to the password_command + security recipe on macOS."
```

---

### Task 13: Generate the self-signed cert and document the procedure

**Files:**
- Create: `docs/release-signing.md`

This task produces an artifact (the `.p12` cert) outside the repo, plus a doc inside it. The maintainer runs the cert-generation commands once on their own machine.

- [ ] **Step 1: Generate the self-signed code-signing cert**

Run on the maintainer's macOS machine:

```bash
mkdir -p /tmp/nb-signing && cd /tmp/nb-signing

# 1. Generate a private key.
openssl genrsa -out neubibackup.key 2048

# 2. Generate a self-signed code-signing cert valid for 100 years.
cat > openssl.cnf <<'EOF'
[ req ]
distinguished_name = dn
prompt = no
x509_extensions = v3_codesign

[ dn ]
CN = NeubiBackup Code Signing
O  = NeubiBackup
C  = US

[ v3_codesign ]
basicConstraints = critical, CA:false
keyUsage = critical, digitalSignature
extendedKeyUsage = critical, codeSigning
EOF

openssl req -new -x509 \
  -key neubibackup.key \
  -days 36500 \
  -config openssl.cnf \
  -out neubibackup.crt

# 3. Bundle into a .p12 (set a strong export password; remember it).
openssl pkcs12 -export \
  -inkey neubibackup.key \
  -in neubibackup.crt \
  -out neubibackup.p12 \
  -name "NeubiBackup Code Signing"

# 4. Capture the cert SHA-1 (the value baked into every user's Keychain ACL).
CERT_SHA1=$(openssl x509 -in neubibackup.crt -noout -fingerprint -sha1 | sed 's/.*=//; s/://g')
echo "Cert SHA-1: $CERT_SHA1"

# 5. Encode the .p12 for the GitHub secret.
base64 -i neubibackup.p12 > neubibackup.p12.base64
```

Save:
- `neubibackup.p12` and the export password → 1Password / maintainer's password manager.
- `neubibackup.p12.base64` contents → GitHub repo secret `MACOS_CERT_P12_BASE64`.
- The export password → GitHub repo secret `MACOS_CERT_PASSWORD`.
- The SHA-1 from step 4 → write it into `docs/release-signing.md` (next step) and the GitHub secret list as a sanity-check reference.
- An offline backup copy of `neubibackup.p12` (encrypted USB or printed base64) somewhere durable.

After saving, securely delete the `/tmp/nb-signing/` directory:

```bash
rm -rf /tmp/nb-signing
```

- [ ] **Step 2: Write the release-signing doc**

Create `docs/release-signing.md`:

```markdown
# Release signing

NeubiBackup releases are signed with a long-lived **self-signed**
code-signing cert (not Apple Developer ID). Every release uses the same
cert so the macOS Keychain ACL on user-stored repository passwords stays
valid across auto-updates.

This document is for maintainers. If you only build the project locally,
your `go build` / `scripts/build-dev-app.sh` workflow continues to use
ad-hoc signing — nothing here applies.

## What signing buys us

- macOS releases have a stable [Designated Requirement][dr]
  (`identifier "com.neubibackup.app" and certificate root = H"<sha1>"`).
- The `use_keychain` feature (see `README.md`) creates Keychain entries
  whose ACL is bound to that DR. The same cert across releases means
  the ACL keeps matching when users auto-update.
- Lose the cert and every existing user's keychain entry stops being
  silently usable on the next release: they get one prompt, click
  Always Allow, done. But re-using the same cert avoids that prompt
  entirely.

[dr]: https://developer.apple.com/library/archive/documentation/Security/Conceptual/CodeSigningGuide/RequirementLang/RequirementLang.html

## What signing does **not** buy

- Gatekeeper acceptance. macOS still says "from an unidentified
  developer" on first launch — same as today's ad-hoc state. Users
  right-click → Open the first time.
- Notarization (requires Apple Developer ID, which we deliberately do
  not use).

## Cert facts

- **Common Name:** `NeubiBackup Code Signing`
- **Type:** RSA 2048, self-signed root, code-signing EKU
- **Validity:** 100 years from the date of generation
- **SHA-1 fingerprint:** `<paste-the-sha1-from-step-1-here>`

If you ever rebuild the cert, replace the SHA-1 above and warn users:
the next release will trigger a one-time keychain prompt.

## How releases use it

`.github/workflows/release.yml` imports the cert from a temporary
keychain in CI and signs the `.app` bundle with it. See the
`Import code-signing cert` and `Sign app bundle` steps.

## Required GitHub secrets

- `MACOS_CERT_P12_BASE64` — base64-encoded `.p12` (the cert + private
  key bundle).
- `MACOS_CERT_PASSWORD` — the export password set when the `.p12` was
  produced.

## Backup and rotation

The `.p12` exists in three places:

1. GitHub repo secret (used by CI).
2. Maintainer's password manager.
3. Offline backup (encrypted USB / printed base64).

Losing all three means rotating to a new self-signed cert, which
invalidates every user's existing keychain entry. Don't lose all three.

## Regeneration procedure

If you must rotate: regenerate per the commands in this repo's
implementation plan
(`docs/superpowers/plans/2026-05-09-keychain-password.md`, Task 13),
update both GitHub secrets, update the SHA-1 in this file, and call out
the cert change in the next release's notes.
```

After writing, replace `<paste-the-sha1-from-step-1-here>` with the value `CERT_SHA1` printed in Step 1.

- [ ] **Step 3: Verify the secrets are populated**

In a browser (or via `gh secret list`), confirm the repo has both
`MACOS_CERT_P12_BASE64` and `MACOS_CERT_PASSWORD`. Don't proceed to
Task 14 until they are present, otherwise the workflow change will fail
on the next tag.

```bash
gh secret list -R <owner>/<repo>
```

Expected output includes both secret names.

- [ ] **Step 4: Commit the doc**

```bash
git add docs/release-signing.md
git commit -m "docs: add release-signing procedure for self-signed cert

Documents the self-signed code-signing cert reused across all macOS
releases, why it exists (stable Keychain ACL across auto-updates),
where the .p12 lives, and how to rotate if it's ever lost."
```

---

### Task 14: Update the release workflow to sign with the stable cert

**Files:**
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Import the cert into a build keychain**

In `.github/workflows/release.yml`, find the existing "Sign app bundle" step (around line 148). Insert a new step **before** it:

```yaml
      - name: Import code-signing cert
        env:
          P12_BASE64: ${{ secrets.MACOS_CERT_P12_BASE64 }}
          P12_PASSWORD: ${{ secrets.MACOS_CERT_PASSWORD }}
        run: |
          if [ -z "$P12_BASE64" ] || [ -z "$P12_PASSWORD" ]; then
            echo "ERROR: MACOS_CERT_P12_BASE64 and MACOS_CERT_PASSWORD secrets are required."
            exit 1
          fi

          # Write the .p12 to a temporary file.
          echo "$P12_BASE64" | base64 --decode > /tmp/codesign.p12

          # Create a dedicated build keychain so we don't pollute the
          # runner's defaults.
          KCHAIN_PW="ci-keychain-$RANDOM"
          security create-keychain -p "$KCHAIN_PW" build.keychain
          security default-keychain -s build.keychain
          security unlock-keychain -p "$KCHAIN_PW" build.keychain
          security set-keychain-settings -lut 21600 build.keychain

          # Import the cert.
          security import /tmp/codesign.p12 \
            -k build.keychain \
            -P "$P12_PASSWORD" \
            -T /usr/bin/codesign

          # Allow codesign to use the imported key without an additional
          # prompt.
          security set-key-partition-list \
            -S apple-tool:,apple: \
            -s -k "$KCHAIN_PW" build.keychain

          # Wipe the .p12 from disk; it's now resident in the build
          # keychain only.
          rm -f /tmp/codesign.p12

          # Show what got imported (cert hash should match the value in
          # docs/release-signing.md).
          security find-identity -v -p codesigning build.keychain
```

- [ ] **Step 2: Replace the ad-hoc sign step**

Replace the existing "Sign app bundle" step body:

```yaml
      - name: Sign app bundle
        run: |
          # Ad-hoc sign the entire app bundle (sign after copying binary, not before)
          codesign --force --deep --sign - NeubiBackup.app
          echo "App bundle signed successfully"
          codesign -dv --verbose=4 NeubiBackup.app

          # Verify the signature
          codesign --verify --verbose NeubiBackup.app
```

with:

```yaml
      - name: Sign app bundle
        run: |
          # Sign with the stable self-signed cert (see docs/release-signing.md).
          # --deep recursively signs the embedded restic binary first,
          # then the bundle's main executable, then the bundle.
          codesign --force --deep \
            --sign "NeubiBackup Code Signing" \
            NeubiBackup.app

          echo "App bundle signed successfully"
          codesign -dv --verbose=4 NeubiBackup.app

          # Verify the full signature chain.
          codesign --verify --deep --strict --verbose=2 NeubiBackup.app
```

- [ ] **Step 3: Sanity-check the workflow YAML locally**

Run a YAML lint to make sure indentation didn't drift:

```bash
python3 -c "import sys, yaml; yaml.safe_load(open('.github/workflows/release.yml'))"
```

Expected: no output (clean parse). If it errors, fix the indentation.

If `python3 -c "import yaml"` fails because PyYAML isn't installed, skip and rely on GitHub Actions' validation when you push.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: sign macOS releases with stable self-signed cert

Replaces ad-hoc signing (codesign --sign -) with a stable code-signing
identity loaded from the MACOS_CERT_P12_BASE64 / MACOS_CERT_PASSWORD
GitHub secrets. The cert's SHA-1 becomes part of every Keychain ACL
created by the use_keychain feature, so reusing the same cert across
releases keeps user-stored passwords silently usable through
auto-updates. See docs/release-signing.md."
```

- [ ] **Step 5: Test the change before merging to main**

Push the branch and create a release-test tag from it:

```bash
git push -u origin <branch-name>
git tag v0.0.0-keychain-test
git push origin v0.0.0-keychain-test
```

Watch the release workflow on GitHub Actions. Expected result:
- "Import code-signing cert" step prints `1 valid identities found` referencing `NeubiBackup Code Signing`.
- "Sign app bundle" step exits 0 and the verify step passes.
- The release artifacts are produced as before.

If anything fails, fix the workflow, push, and retag (delete the prior test tag first):

```bash
git push origin :refs/tags/v0.0.0-keychain-test
git tag -d v0.0.0-keychain-test
```

When the test passes, delete the test tag from GitHub (it will create a release entry, which can be deleted manually too):

```bash
git push origin :refs/tags/v0.0.0-keychain-test
```

---

## Self-Review

Spec coverage check (skim the spec, point at tasks):

- [x] `internal/keychain` package skeleton + ErrNotFound/ErrUnsupported/ServiceName → Task 2
- [x] macOS implementation via keybase/go-keychain → Task 3
- [x] Windows implementation via wincred → Task 4
- [x] Linux/other stub returning ErrUnsupported → Task 2
- [x] Keychain round-trip tests, platform-conditional → Tasks 2/3/4
- [x] Cross-platform stdin password reader → Task 5
- [x] macOS osascript dialog → Task 6
- [x] Windows PowerShell + WinForms dialog → Task 6
- [x] `repository.use_keychain` config field → Task 7
- [x] Validate() requires exactly one password source → Task 7
- [x] passwordSource interface + buildEnv refactor with error return → Task 8
- [x] Keychain miss surfaces as ErrPasswordFailed (no retry) → Task 8 (`fmt.Errorf("%w: keychain: %v", ErrPasswordFailed, err)`)
- [x] Tests verifying RESTIC_PASSWORD set under use_keychain, no `--password-command` flag → Task 9
- [x] CLI subcommands set-password / clear-password → Task 10
- [x] Subcommand dispatch ahead of singleinstance lock → Task 10
- [x] Tray submenu items, disabled when use_keychain false → Task 11
- [x] App handlers for tray callbacks → Task 11
- [x] README + config template updates → Task 12
- [x] Self-signed cert generation procedure → Task 13
- [x] docs/release-signing.md → Task 13
- [x] release.yml workflow change → Task 14

Type / signature consistency:

- `keychain.Get/Set/Delete` signatures identical across `keychain_darwin.go`, `keychain_windows.go`, `keychain_other.go` ✓
- `keychain.PromptDialog(title, message string) (string, error)` identical across `dialog_*.go` ✓
- `passwordSource.Get(account string) (string, error)` matches across runner.go, runner_test.go, and what `keychain.Get` exposes ✓
- `keychainBackend` interface in `cmd_password.go` matches what `realKeychain` and `fakeKeychain` provide ✓
- Service name string `"com.neubibackup.repository"` consistent between code (`ServiceName` constant) and docs ✓
- Bundle identifier `com.neubibackup.app` matches what the workflow already sets in `Info.plist` ✓
- `MACOS_CERT_P12_BASE64` / `MACOS_CERT_PASSWORD` secret names consistent between Task 13 and Task 14 ✓

Placeholder scan: no "TBD", no "implement later", no "similar to Task N", no vague "handle errors". Every step that requires code has the code inline.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-09-keychain-password.md`. Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
