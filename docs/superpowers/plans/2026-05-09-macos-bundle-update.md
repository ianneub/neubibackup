# macOS Bundle-Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the entire `.app` bundle on macOS auto-update so the bundle's code signature stays valid after every update.

**Architecture:** Bypass `go-selfupdate.UpdateTo` on darwin. New darwin-only file `internal/updater/apply_darwin.go` extracts the release ZIP to a sibling temp dir, verifies the new bundle's signature with `codesign --verify --strict`, then atomically renames the old `.app` aside and the new one into place. Stale `.app.old` siblings are swept on the next launch via the existing `cleanupOldUpdates()` hook in `internal/app`.

**Tech Stack:** Go 1.26, `archive/zip` (stdlib), `os/exec` (for `codesign`), `github.com/creativeprojects/go-selfupdate` (for release detection and asset download only — not for applying).

**Spec:** `docs/superpowers/specs/2026-05-09-macos-bundle-update-design.md`

---

## File Structure

| File | Purpose |
|---|---|
| `internal/updater/apply_darwin.go` (new) | Darwin-only: `applyDarwinBundleUpdate`, `extractZipToBundle`, `swapBundles`, `verifySignature`, `cleanupStaleBundles`. |
| `internal/updater/apply_darwin_test.go` (new) | Tests for all four helpers + integration test (skipped without `codesign`). |
| `internal/updater/updater.go` (modify) | Branch `DownloadAndApply` on `runtime.GOOS`: darwin → `applyDarwinBundleUpdate`, else → existing `selfupdate.UpdateTo`. |
| `internal/app/cleanup_darwin.go` (new) | Darwin override of `cleanupOldUpdates()` calling `updater.CleanupStaleBundles()`. |
| `internal/app/cleanup_other.go` (modify) | Tighten build tag from `!windows` to `!windows && !darwin` so the no-op stays the catch-all. |
| `README.md` (modify) | One-line note in the auto-update section that updates now replace the entire bundle. |

Each task ends with a commit. The branch should be left in a green state after every task (`go test ./...` passes).

---

## Task 1: extractZipToBundle (TDD)

Pure function: take ZIP bytes plus a destination directory; validate the archive is a single `.app` and write its contents to `destDir`. No network, no `codesign`.

**Files:**
- Create: `internal/updater/apply_darwin.go`
- Create: `internal/updater/apply_darwin_test.go`

- [ ] **Step 1.1: Create the test file with the happy-path test**

Write `internal/updater/apply_darwin_test.go`:

```go
//go:build darwin

package updater

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// buildZip creates an in-memory zip whose entries are the (name, mode, body)
// triples passed in. It is the test helper used everywhere in this file —
// keep it cheap and obvious.
func buildZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		hdr.SetMode(e.mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("CreateHeader(%q): %v", e.name, err)
		}
		if _, err := w.Write(e.body); err != nil {
			t.Fatalf("Write(%q): %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	return buf.Bytes()
}

type zipEntry struct {
	name string
	mode os.FileMode
	body []byte
}

func TestExtractZipToBundle_HappyPath(t *testing.T) {
	zipBytes := buildZip(t, []zipEntry{
		{name: "NeubiBackup.app/Contents/Info.plist", mode: 0o644, body: []byte("<plist/>")},
		{name: "NeubiBackup.app/Contents/MacOS/neubibackup", mode: 0o755, body: []byte("\x7fELF-not-really")},
		{name: "NeubiBackup.app/Contents/_CodeSignature/CodeResources", mode: 0o644, body: []byte("<resources/>")},
	})

	dest := filepath.Join(t.TempDir(), ".NeubiBackup.app.new")
	if err := extractZipToBundle(zipBytes, dest); err != nil {
		t.Fatalf("extractZipToBundle: %v", err)
	}

	checks := []struct {
		path     string
		wantMode os.FileMode
		wantBody string
	}{
		{filepath.Join(dest, "Contents/Info.plist"), 0o644, "<plist/>"},
		{filepath.Join(dest, "Contents/MacOS/neubibackup"), 0o755, "\x7fELF-not-really"},
		{filepath.Join(dest, "Contents/_CodeSignature/CodeResources"), 0o644, "<resources/>"},
	}
	for _, c := range checks {
		info, err := os.Stat(c.path)
		if err != nil {
			t.Fatalf("Stat(%q): %v", c.path, err)
		}
		if got := info.Mode().Perm(); got != c.wantMode {
			t.Errorf("%s mode = %o, want %o", c.path, got, c.wantMode)
		}
		body, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", c.path, err)
		}
		if string(body) != c.wantBody {
			t.Errorf("%s body = %q, want %q", c.path, body, c.wantBody)
		}
	}
}
```

- [ ] **Step 1.2: Run the test and confirm it fails to compile**

Run:
```bash
go test ./internal/updater/ -run TestExtractZipToBundle_HappyPath -v
```

Expected: build error — `undefined: extractZipToBundle`.

- [ ] **Step 1.3: Create `apply_darwin.go` with the minimal implementation**

Write `internal/updater/apply_darwin.go`:

```go
//go:build darwin

package updater

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractZipToBundle writes the contents of a single-.app ZIP into destDir.
// destDir must not exist yet. The ZIP must contain exactly one top-level
// directory whose name ends in ".app"; entries outside that root, or paths
// that try to escape destDir, are rejected.
func extractZipToBundle(zipBytes []byte, destDir string) error {
	r, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	if len(r.File) == 0 {
		return errors.New("zip is empty")
	}

	root, err := singleAppRoot(r.File)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}

	for _, f := range r.File {
		rel := strings.TrimPrefix(f.Name, root+"/")
		if rel == f.Name {
			// Entry is the root directory itself (e.g., "NeubiBackup.app/").
			if strings.TrimSuffix(f.Name, "/") == root {
				continue
			}
			return fmt.Errorf("zip entry %q outside root %q", f.Name, root)
		}
		dest := filepath.Join(destDir, rel)
		if relCheck, err := filepath.Rel(destDir, dest); err != nil || strings.HasPrefix(relCheck, "..") {
			return fmt.Errorf("zip entry %q escapes destination", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, f.Mode().Perm()); err != nil {
				return fmt.Errorf("mkdir %q: %w", dest, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir parent of %q: %w", dest, err)
		}
		if err := writeZipFile(f, dest); err != nil {
			return err
		}
	}
	return nil
}

// singleAppRoot returns the single top-level directory name in the archive
// (without trailing slash). It errors unless every entry shares one root and
// that root ends with ".app".
func singleAppRoot(files []*zip.File) (string, error) {
	roots := map[string]struct{}{}
	for _, f := range files {
		head := f.Name
		if i := strings.IndexByte(head, '/'); i >= 0 {
			head = head[:i]
		}
		if head == "" {
			return "", fmt.Errorf("zip entry %q has empty top segment", f.Name)
		}
		roots[head] = struct{}{}
	}
	if len(roots) != 1 {
		return "", fmt.Errorf("zip must have exactly one top-level directory, got %d", len(roots))
	}
	var root string
	for r := range roots {
		root = r
	}
	if !strings.HasSuffix(root, ".app") {
		return "", fmt.Errorf("zip top-level %q is not a .app bundle", root)
	}
	return root, nil
}

func writeZipFile(f *zip.File, dest string) error {
	src, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %q: %w", f.Name, err)
	}
	defer src.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %q: %w", dest, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, src); err != nil {
		return fmt.Errorf("copy to %q: %w", dest, err)
	}
	return nil
}
```

- [ ] **Step 1.4: Run the test and confirm it passes**

Run:
```bash
go test ./internal/updater/ -run TestExtractZipToBundle_HappyPath -v
```

Expected: PASS.

- [ ] **Step 1.5: Add the negative tests**

Append to `internal/updater/apply_darwin_test.go`:

```go
func TestExtractZipToBundle_ZipSlip(t *testing.T) {
	zipBytes := buildZip(t, []zipEntry{
		{name: "NeubiBackup.app/Contents/Info.plist", mode: 0o644, body: []byte("ok")},
		{name: "NeubiBackup.app/../escape.txt", mode: 0o644, body: []byte("pwned")},
	})
	dest := filepath.Join(t.TempDir(), ".new")
	if err := extractZipToBundle(zipBytes, dest); err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escape.txt")); err == nil {
		t.Fatal("zip-slip wrote a file outside destDir")
	}
}

func TestExtractZipToBundle_WrongRoot(t *testing.T) {
	t.Run("not_app_suffix", func(t *testing.T) {
		zipBytes := buildZip(t, []zipEntry{
			{name: "NeubiBackup/Contents/Info.plist", mode: 0o644, body: []byte("x")},
		})
		if err := extractZipToBundle(zipBytes, filepath.Join(t.TempDir(), ".new")); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
	t.Run("two_roots", func(t *testing.T) {
		zipBytes := buildZip(t, []zipEntry{
			{name: "First.app/Contents/Info.plist", mode: 0o644, body: []byte("a")},
			{name: "Second.app/Contents/Info.plist", mode: 0o644, body: []byte("b")},
		})
		if err := extractZipToBundle(zipBytes, filepath.Join(t.TempDir(), ".new")); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestExtractZipToBundle_Empty(t *testing.T) {
	zipBytes := buildZip(t, nil)
	if err := extractZipToBundle(zipBytes, filepath.Join(t.TempDir(), ".new")); err == nil {
		t.Fatal("expected error, got nil")
	}
}
```

- [ ] **Step 1.6: Run all extract tests and confirm green**

Run:
```bash
go test ./internal/updater/ -run TestExtractZipToBundle -v
```

Expected: 4 tests PASS.

- [ ] **Step 1.7: Commit**

```bash
git add internal/updater/apply_darwin.go internal/updater/apply_darwin_test.go
git commit -m "feat(updater): add darwin zip-to-bundle extractor

Pure function that writes a single-.app zip into a destination dir,
with zip-slip guard and a strict single-root check. First piece of
the macOS bundle-replacement update flow."
```

---

## Task 2: swapBundles (TDD)

Atomic rename pair with rollback if the second rename fails.

**Files:**
- Modify: `internal/updater/apply_darwin.go`
- Modify: `internal/updater/apply_darwin_test.go`

- [ ] **Step 2.1: Write the failing tests**

Append to `internal/updater/apply_darwin_test.go`:

```go
func TestSwapBundles_HappyPath(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "App.app")
	newPath := filepath.Join(dir, ".App.app.new")
	oldPath := filepath.Join(dir, ".App.app.old")

	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents/marker"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(newPath, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newPath, "Contents/marker"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := swapBundles(app, newPath, oldPath); err != nil {
		t.Fatalf("swapBundles: %v", err)
	}

	if body, _ := os.ReadFile(filepath.Join(app, "Contents/marker")); string(body) != "v2" {
		t.Errorf("appPath marker = %q, want v2", body)
	}
	if body, _ := os.ReadFile(filepath.Join(oldPath, "Contents/marker")); string(body) != "v1" {
		t.Errorf("oldPath marker = %q, want v1", body)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("newPath still exists after swap")
	}
}

func TestSwapBundles_RollbackOnSecondRenameFailure(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "App.app")
	missingNew := filepath.Join(dir, "does-not-exist")
	oldPath := filepath.Join(dir, ".App.app.old")

	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents/marker"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := swapBundles(app, missingNew, oldPath)
	if err == nil {
		t.Fatal("expected error from missing newPath, got nil")
	}

	if body, _ := os.ReadFile(filepath.Join(app, "Contents/marker")); string(body) != "v1" {
		t.Errorf("appPath marker = %q after rollback, want v1", body)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("oldPath still exists after rollback")
	}
}
```

- [ ] **Step 2.2: Run and confirm they fail to compile**

Run:
```bash
go test ./internal/updater/ -run TestSwapBundles -v
```

Expected: build error — `undefined: swapBundles`.

- [ ] **Step 2.3: Add `swapBundles` to `apply_darwin.go`**

Append to `internal/updater/apply_darwin.go`:

```go
// swapBundles atomically replaces appPath with newPath. The previous bundle
// is moved to oldPath. If the second rename fails, the first rename is
// rolled back so the live bundle is left intact.
func swapBundles(appPath, newPath, oldPath string) error {
	if err := os.Rename(appPath, oldPath); err != nil {
		return fmt.Errorf("rename %q -> %q: %w", appPath, oldPath, err)
	}
	if err := os.Rename(newPath, appPath); err != nil {
		// Try to restore the previous bundle.
		if rbErr := os.Rename(oldPath, appPath); rbErr != nil {
			return fmt.Errorf(
				"rename %q -> %q failed: %v; rollback also failed: %v",
				newPath, appPath, err, rbErr,
			)
		}
		return fmt.Errorf("rename %q -> %q: %w (rolled back)", newPath, appPath, err)
	}
	return nil
}
```

- [ ] **Step 2.4: Run and confirm green**

Run:
```bash
go test ./internal/updater/ -run TestSwapBundles -v
```

Expected: 2 tests PASS.

- [ ] **Step 2.5: Commit**

```bash
git add internal/updater/apply_darwin.go internal/updater/apply_darwin_test.go
git commit -m "feat(updater): add swapBundles with rollback for darwin

Atomic rename pair that moves the live .app aside before sliding the
new one into place; rolls back the first rename if the second fails."
```

---

## Task 3: verifySignature (TDD with skip)

Wrap `codesign --verify --strict` so a malformed bundle is rejected before we touch the live one.

**Files:**
- Modify: `internal/updater/apply_darwin.go`
- Modify: `internal/updater/apply_darwin_test.go`

- [ ] **Step 3.1: Write the test**

Append to `internal/updater/apply_darwin_test.go`:

```go
import "os/exec"

// codesignAvailable reports whether real codesign tests should run.
// Disabled by SKIP_CODESIGN_TESTS=1 (CI-only matrices that lack codesign).
func codesignAvailable(t *testing.T) bool {
	t.Helper()
	if os.Getenv("SKIP_CODESIGN_TESTS") == "1" {
		return false
	}
	if _, err := exec.LookPath("codesign"); err != nil {
		return false
	}
	return true
}

// signAdHoc signs the given path with the ad-hoc signer ("-").
func signAdHoc(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("codesign", "--force", "--sign", "-", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ad-hoc sign %q: %v: %s", path, err, out)
	}
}

// buildFakeApp creates the minimum on-disk shape that codesign accepts as a
// bundle: a directory named *.app with Contents/MacOS/<exe> and a
// Contents/Info.plist whose CFBundleExecutable matches the exe basename.
func buildFakeApp(t *testing.T, dir, name, exe string) string {
	t.Helper()
	app := filepath.Join(dir, name)
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(macos, exe)
	if err := os.WriteFile(exePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleExecutable</key><string>` + exe + `</string>
  <key>CFBundleIdentifier</key><string>com.example.fake</string>
  <key>CFBundleName</key><string>Fake</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>CFBundleShortVersionString</key><string>1.0</string>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestVerifySignature_AcceptsAdHocSignedBundle(t *testing.T) {
	if !codesignAvailable(t) {
		t.Skip("codesign not available")
	}
	app := buildFakeApp(t, t.TempDir(), "Fake.app", "fake")
	signAdHoc(t, app)

	if err := verifySignature(app); err != nil {
		t.Fatalf("verifySignature on signed bundle: %v", err)
	}
}

func TestVerifySignature_RejectsTamperedBundle(t *testing.T) {
	if !codesignAvailable(t) {
		t.Skip("codesign not available")
	}
	app := buildFakeApp(t, t.TempDir(), "Fake.app", "fake")
	signAdHoc(t, app)

	// Tamper with the Mach-O after signing.
	exePath := filepath.Join(app, "Contents", "MacOS", "fake")
	body, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, []byte("# tampered\n")...)
	if err := os.WriteFile(exePath, body, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := verifySignature(app); err == nil {
		t.Fatal("expected verifySignature to reject tampered bundle, got nil")
	}
}

func TestVerifySignature_RejectsUnsignedBundle(t *testing.T) {
	if !codesignAvailable(t) {
		t.Skip("codesign not available")
	}
	app := buildFakeApp(t, t.TempDir(), "Fake.app", "fake")
	if err := verifySignature(app); err == nil {
		t.Fatal("expected error for unsigned bundle, got nil")
	}
}
```

Note: that `import "os/exec"` line at the top of the snippet must be merged into the existing import block of `apply_darwin_test.go`, not added as a duplicate top-level import.

- [ ] **Step 3.2: Run and confirm they fail to compile**

Run:
```bash
go test ./internal/updater/ -run TestVerifySignature -v
```

Expected: build error — `undefined: verifySignature`.

- [ ] **Step 3.3: Add `verifySignature` to `apply_darwin.go`**

Append to `internal/updater/apply_darwin.go`:

```go
import "os/exec" // merge into existing import block

// verifySignature runs `codesign --verify --strict` against bundlePath and
// returns the codesign output as part of the error if the verification fails.
func verifySignature(bundlePath string) error {
	cmd := exec.Command("codesign", "--verify", "--strict", bundlePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign verify failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

(`os/exec` import: merge into the existing import block at the top of the file.)

- [ ] **Step 3.4: Run and confirm green**

Run:
```bash
go test ./internal/updater/ -run TestVerifySignature -v
```

Expected: 3 tests PASS (or SKIP if codesign is unavailable).

- [ ] **Step 3.5: Commit**

```bash
git add internal/updater/apply_darwin.go internal/updater/apply_darwin_test.go
git commit -m "feat(updater): add verifySignature wrapping codesign --verify --strict

Used to reject a tampered or malformed update before we swap it into
place. Tests build a tiny fake bundle, ad-hoc sign it, and exercise
both happy and tampered paths; they skip when codesign is missing."
```

---

## Task 4: applyDarwinBundleUpdate (TDD, integration)

Compose the previous three helpers plus a download closure. This is the function the rest of the updater calls.

**Files:**
- Modify: `internal/updater/apply_darwin.go`
- Modify: `internal/updater/apply_darwin_test.go`

- [ ] **Step 4.1: Write the integration tests**

Append to `internal/updater/apply_darwin_test.go`:

```go
import "context" // merge into existing import block

// zipDir walks src and produces a zip whose entries are rooted at the
// basename of src (so the archive has a single top-level directory).
func zipDir(t *testing.T, src string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	root := filepath.Base(src)

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		name := root
		if rel != "." {
			name = filepath.Join(root, rel)
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(name)
		hdr.Method = zip.Deflate
		if info.IsDir() {
			hdr.Name += "/"
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(body)
		return err
	})
	if err != nil {
		t.Fatalf("zipDir: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	return buf.Bytes()
}

func TestApplyDarwinBundleUpdate_HappyPath(t *testing.T) {
	if !codesignAvailable(t) {
		t.Skip("codesign not available")
	}
	dir := t.TempDir()

	// Build & sign the "installed" bundle (v1) and place it in dir.
	v1 := buildFakeApp(t, dir, "App.app", "fake")
	if err := os.WriteFile(filepath.Join(v1, "Contents", "MacOS", "fake"), []byte("#!/bin/sh\necho v1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	signAdHoc(t, v1)

	// Build, sign, and zip the "update" bundle (v2) in a side directory.
	stage := t.TempDir()
	v2 := buildFakeApp(t, stage, "App.app", "fake")
	if err := os.WriteFile(filepath.Join(v2, "Contents", "MacOS", "fake"), []byte("#!/bin/sh\necho v2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	signAdHoc(t, v2)
	zipBytes := zipDir(t, v2)

	// runningExec mimics what os.Executable returns inside a .app bundle.
	runningExec := filepath.Join(v1, "Contents", "MacOS", "fake")
	fetch := func(_ context.Context) ([]byte, error) { return zipBytes, nil }

	if err := applyDarwinBundleUpdate(context.Background(), fetch, runningExec); err != nil {
		t.Fatalf("applyDarwinBundleUpdate: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(v1, "Contents", "MacOS", "fake"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(body, []byte("echo v2")) {
		t.Errorf("installed binary still v1: %q", body)
	}
	if err := verifySignature(v1); err != nil {
		t.Errorf("post-swap verifySignature: %v", err)
	}
}

func TestApplyDarwinBundleUpdate_RejectsTamperedZip(t *testing.T) {
	if !codesignAvailable(t) {
		t.Skip("codesign not available")
	}
	dir := t.TempDir()

	v1 := buildFakeApp(t, dir, "App.app", "fake")
	signAdHoc(t, v1)
	originalBody, err := os.ReadFile(filepath.Join(v1, "Contents", "MacOS", "fake"))
	if err != nil {
		t.Fatal(err)
	}

	stage := t.TempDir()
	v2 := buildFakeApp(t, stage, "App.app", "fake")
	signAdHoc(t, v2)
	// Tamper *after* signing, before zipping — bundle's signature is now invalid.
	tamperPath := filepath.Join(v2, "Contents", "MacOS", "fake")
	tampered, _ := os.ReadFile(tamperPath)
	tampered = append(tampered, []byte("# tampered\n")...)
	if err := os.WriteFile(tamperPath, tampered, 0o755); err != nil {
		t.Fatal(err)
	}
	zipBytes := zipDir(t, v2)

	runningExec := filepath.Join(v1, "Contents", "MacOS", "fake")
	fetch := func(_ context.Context) ([]byte, error) { return zipBytes, nil }

	if err := applyDarwinBundleUpdate(context.Background(), fetch, runningExec); err == nil {
		t.Fatal("expected error from tampered zip, got nil")
	}

	postSwapBody, err := os.ReadFile(filepath.Join(v1, "Contents", "MacOS", "fake"))
	if err != nil {
		t.Fatalf("ReadFile after rejection: %v", err)
	}
	if !bytes.Equal(originalBody, postSwapBody) {
		t.Error("live bundle was modified despite verification failure")
	}
}

func TestApplyDarwinBundleUpdate_NotInBundleErrors(t *testing.T) {
	fetch := func(_ context.Context) ([]byte, error) {
		t.Fatal("fetch should not be called when running outside a bundle")
		return nil, nil
	}
	err := applyDarwinBundleUpdate(context.Background(), fetch, "/usr/local/bin/neubibackup")
	if err == nil {
		t.Fatal("expected error when not inside an .app bundle, got nil")
	}
}
```

- [ ] **Step 4.2: Run and confirm they fail to compile**

Run:
```bash
go test ./internal/updater/ -run TestApplyDarwinBundleUpdate -v
```

Expected: build error — `undefined: applyDarwinBundleUpdate`.

- [ ] **Step 4.3: Add `applyDarwinBundleUpdate` to `apply_darwin.go`**

Append to `internal/updater/apply_darwin.go`:

```go
import (
	"context"
	"log/slog"
)
// merge "context" and "log/slog" into the existing import block

// FetchZipFunc downloads the update ZIP. The function is injected so tests
// can supply bytes directly without hitting the network.
type FetchZipFunc func(ctx context.Context) ([]byte, error)

// applyDarwinBundleUpdate replaces the running .app bundle with the bundle
// contained in the ZIP returned by fetch. The function never touches the
// live bundle until the new one is fully extracted and codesign --verify
// --strict has accepted it.
func applyDarwinBundleUpdate(ctx context.Context, fetch FetchZipFunc, runningExec string) error {
	appPath := findAppBundle(runningExec)
	if appPath == "" {
		return fmt.Errorf("auto-update requires running from a .app bundle (exec=%q)", runningExec)
	}
	appDir := filepath.Dir(appPath)
	base := filepath.Base(appPath)
	newPath := filepath.Join(appDir, "."+base+".new")
	oldPath := filepath.Join(appDir, "."+base+".old")

	if err := preflightWritable(appDir); err != nil {
		return err
	}

	// Recover from a half-finished previous run before doing any work.
	_ = os.RemoveAll(newPath)
	_ = os.RemoveAll(oldPath)

	slog.Info("auto-update: downloading bundle zip")
	zipBytes, err := fetch(ctx)
	if err != nil {
		return fmt.Errorf("download update zip: %w", err)
	}
	slog.Info("auto-update: downloaded bundle zip", "bytes", len(zipBytes))

	if err := extractZipToBundle(zipBytes, newPath); err != nil {
		_ = os.RemoveAll(newPath)
		return fmt.Errorf("extract update: %w", err)
	}

	if err := verifySignature(newPath); err != nil {
		_ = os.RemoveAll(newPath)
		return fmt.Errorf("verify update signature: %w", err)
	}

	if err := swapBundles(appPath, newPath, oldPath); err != nil {
		// swapBundles handles its own rollback; clean up the staging dir.
		_ = os.RemoveAll(newPath)
		return err
	}

	// Best-effort cleanup; cleanupStaleBundles will sweep on next launch.
	if err := os.RemoveAll(oldPath); err != nil {
		slog.Warn("auto-update: could not remove old bundle (will retry on next launch)", "path", oldPath, "error", err)
	}
	slog.Info("auto-update: bundle replaced successfully", "path", appPath)
	return nil
}

// preflightWritable verifies we can create a file in dir. Catches /Applications
// permission issues before we destroy anything.
func preflightWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".nbup-*")
	if err != nil {
		return fmt.Errorf("auto-update needs write access to %q: %w", dir, err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}
```

- [ ] **Step 4.4: Run and confirm green**

Run:
```bash
go test ./internal/updater/ -run TestApplyDarwinBundleUpdate -v
```

Expected: 3 tests PASS (the codesign-dependent ones SKIP cleanly when codesign is absent).

Then run the full updater test suite to confirm nothing else broke:
```bash
go test ./internal/updater/ -v
```

Expected: all PASS / SKIP (no FAIL).

- [ ] **Step 4.5: Commit**

```bash
git add internal/updater/apply_darwin.go internal/updater/apply_darwin_test.go
git commit -m "feat(updater): add applyDarwinBundleUpdate composing extract+verify+swap

Top-level darwin helper that downloads the update zip via an injected
fetcher, extracts and codesign-verifies a sibling temp bundle, then
atomic-renames it into place. Live bundle is never touched on failure."
```

---

## Task 5: Wire `Updater.DownloadAndApply` to the darwin path

Replace `selfupdate.UpdateTo` on darwin with `applyDarwinBundleUpdate` and a closure that downloads the asset via the existing `selfupdate.GitHubSource`.

**Files:**
- Modify: `internal/updater/updater.go`

- [ ] **Step 5.1: Read the current `DownloadAndApply` to confirm the diff target**

Run:
```bash
grep -n "func (u \*Updater) DownloadAndApply" internal/updater/updater.go
```

Expected: prints one line. The function continues until the closing `}` of `return nil` — that whole function body is what Step 5.2 replaces.

- [ ] **Step 5.2: Replace the `DownloadAndApply` function**

Replace the entire existing `DownloadAndApply` function in `internal/updater/updater.go` (from `// DownloadAndApply downloads the latest update and applies it.` through the function's closing `}`) with:

```go
// DownloadAndApply downloads the latest update and applies it.
// The application should restart after this completes successfully.
func (u *Updater) DownloadAndApply(ctx context.Context) error {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return fmt.Errorf("creating GitHub source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return fmt.Errorf("creating updater: %w", err)
	}

	latest, found, err := updater.DetectLatest(ctx, selfupdate.NewRepositorySlug(u.repoOwner, u.repoName))
	if err != nil {
		return fmt.Errorf("detecting latest version: %w", err)
	}

	if !found {
		return fmt.Errorf("no release found")
	}

	if !latest.GreaterThan(u.currentVersion) {
		return fmt.Errorf("already up to date")
	}

	slog.Info("Updating", "from", u.currentVersion, "to", latest.Version())

	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	if runtime.GOOS == "darwin" {
		slog.Info("Updating macOS app bundle", "path", exe)
		fetch := func(ctx context.Context) ([]byte, error) {
			rc, err := source.DownloadReleaseAsset(ctx, latest, latest.AssetID)
			if err != nil {
				return nil, fmt.Errorf("download release asset: %w", err)
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
		if err := applyDarwinBundleUpdate(ctx, fetch, exe); err != nil {
			return fmt.Errorf("applying darwin update: %w", err)
		}
	} else {
		if err := updater.UpdateTo(ctx, latest, exe); err != nil {
			return fmt.Errorf("applying update: %w", err)
		}
	}

	slog.Info("Update completed successfully", "version", latest.Version())
	return nil
}
```

Then add `"io"` to the import block at the top of `internal/updater/updater.go` if not already present.

- [ ] **Step 5.3: Build and run all unit tests**

Run:
```bash
go build ./... && go test ./internal/updater/...
```

Expected: builds clean (darwin host); all updater tests PASS.

- [ ] **Step 5.4: Commit**

```bash
git add internal/updater/updater.go
git commit -m "feat(updater): route darwin updates through full-bundle replacement

DownloadAndApply now branches on runtime.GOOS — darwin uses the new
applyDarwinBundleUpdate (zip → sibling temp dir → codesign verify →
atomic swap). All other platforms keep selfupdate.UpdateTo."
```

---

## Task 6: Stale-bundle sweeper at startup

Hook `cleanupStaleBundles` into the existing `cleanupOldUpdates()` startup path so a leftover `.NeubiBackup.app.old` (e.g., from a process that exited before best-effort cleanup) is removed silently on the next launch.

**Files:**
- Create: `internal/app/cleanup_darwin.go`
- Modify: `internal/app/cleanup_other.go`
- Modify: `internal/updater/apply_darwin.go`
- Modify: `internal/updater/apply_darwin_test.go`

- [ ] **Step 6.1: Write the test for `CleanupStaleBundles`**

Append to `internal/updater/apply_darwin_test.go`:

```go
func TestCleanupStaleBundles_RemovesOldSiblings(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "App.app")
	old1 := filepath.Join(dir, ".App.app.old")
	old2 := filepath.Join(dir, ".App.app.new") // half-finished extract

	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(old1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(old2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old1, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	CleanupStaleBundles(filepath.Join(app, "Contents", "MacOS", "fake"))

	if _, err := os.Stat(old1); !os.IsNotExist(err) {
		t.Errorf("%s still exists", old1)
	}
	if _, err := os.Stat(old2); !os.IsNotExist(err) {
		t.Errorf("%s still exists", old2)
	}
	if _, err := os.Stat(app); err != nil {
		t.Errorf("live %s removed: %v", app, err)
	}
}

func TestCleanupStaleBundles_NoOpOutsideBundle(t *testing.T) {
	// Should not panic or error on a non-bundle path.
	CleanupStaleBundles("/usr/local/bin/neubibackup")
}
```

- [ ] **Step 6.2: Run and confirm it fails to compile**

Run:
```bash
go test ./internal/updater/ -run TestCleanupStaleBundles -v
```

Expected: build error — `undefined: CleanupStaleBundles`.

- [ ] **Step 6.3: Implement `CleanupStaleBundles` in `apply_darwin.go`**

Append to `internal/updater/apply_darwin.go`:

```go
// CleanupStaleBundles removes any .<bundle>.{old,new} siblings of the running
// .app bundle. Best-effort; called from app startup.
func CleanupStaleBundles(runningExec string) {
	appPath := findAppBundle(runningExec)
	if appPath == "" {
		return
	}
	appDir := filepath.Dir(appPath)
	base := filepath.Base(appPath)
	for _, suffix := range []string{".old", ".new"} {
		path := filepath.Join(appDir, "."+base+suffix)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			slog.Warn("could not remove stale bundle", "path", path, "error", err)
		} else {
			slog.Info("removed stale bundle", "path", path)
		}
	}
}
```

- [ ] **Step 6.4: Run and confirm green**

Run:
```bash
go test ./internal/updater/ -run TestCleanupStaleBundles -v
```

Expected: 2 tests PASS.

- [ ] **Step 6.5: Tighten `cleanup_other.go` build tag and add darwin override**

Edit the first line of `internal/app/cleanup_other.go` from:
```go
//go:build !windows
```
to:
```go
//go:build !windows && !darwin
```

Then create `internal/app/cleanup_darwin.go`:
```go
//go:build darwin

package app

import (
	"os"

	"neubibackup/internal/updater"
)

// cleanupOldUpdates sweeps stale .app.{old,new} siblings left over from a
// previous self-update that exited before its best-effort cleanup ran.
func cleanupOldUpdates() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	updater.CleanupStaleBundles(exe)
}

// cleanupOldAutostartShortcut is a no-op on macOS.
func cleanupOldAutostartShortcut() {}
```

- [ ] **Step 6.6: Build the project for darwin and run all tests**

Run:
```bash
go build ./... && go test ./...
```

Expected: clean build; all tests PASS or SKIP.

- [ ] **Step 6.7: Commit**

```bash
git add internal/updater/apply_darwin.go internal/updater/apply_darwin_test.go internal/app/cleanup_darwin.go internal/app/cleanup_other.go
git commit -m "feat(app): sweep stale macOS update bundles at startup

If a previous self-update exits before its best-effort cleanup
removes .NeubiBackup.app.old (or a half-finished .new), wipe it on
the next launch so the next update has a clean slate."
```

---

## Task 7: README note

Document the new behavior in one short paragraph so users can grep for it.

**Files:**
- Modify: `README.md`

- [ ] **Step 7.1: Locate the auto-update / Updates section**

Run:
```bash
grep -n -i "auto.*update\|automatic update" README.md | head -10
```

Expected: prints one or more line numbers — pick the section that talks about the in-app auto-updater (not the menu item glossary).

- [ ] **Step 7.2: Insert a sentence right after the relevant paragraph**

Add this sentence at the end of the auto-update paragraph (the `Edit` tool needs the existing paragraph as `old_string`; replace verbatim):

```
On macOS, automatic updates now replace the entire `NeubiBackup.app`
bundle (not just the inner binary). This keeps the bundle's code
signature valid after every update so the Keychain ACL behind
`use_keychain` stays usable without re-prompting.
```

If the file doesn't already have such a paragraph, add a one-paragraph subsection titled `### macOS update behavior` containing the sentence above.

- [ ] **Step 7.3: Final smoke build**

Run:
```bash
go build ./... && go test ./...
```

Expected: clean.

- [ ] **Step 7.4: Commit**

```bash
git add README.md
git commit -m "docs(readme): note that macOS auto-updates now replace the full .app

Explain why the change matters (keeps the codesigning chain intact and
the use_keychain ACL silently usable)."
```

---

## Verification checklist (run before declaring done)

- [ ] `go test ./...` — green on darwin host.
- [ ] `go build ./...` — green.
- [ ] Cross-build sanity: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...` builds without referencing darwin-only symbols.
- [ ] `git log --oneline -10` shows seven separate commits, one per task.

## Manual smoke test (post-merge, on a real Mac — not part of the plan steps)

1. Install the previous tagged release into `/Applications` from the DMG.
2. Run a backup against a `use_keychain` repo so the Keychain ACL is created.
3. Tag a new release, let auto-update run.
4. After restart, run `codesign --verify --deep --strict /Applications/NeubiBackup.app` — must exit 0.
5. Run another backup — must complete without a Keychain prompt.
