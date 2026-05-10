//go:build darwin

package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"os/exec"
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

	if err := verifySignature(context.Background(), app); err != nil {
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

	if err := verifySignature(context.Background(), app); err == nil {
		t.Fatal("expected verifySignature to reject tampered bundle, got nil")
	}
}

func TestVerifySignature_RejectsUnsignedBundle(t *testing.T) {
	if !codesignAvailable(t) {
		t.Skip("codesign not available")
	}
	app := buildFakeApp(t, t.TempDir(), "Fake.app", "fake")
	if err := verifySignature(context.Background(), app); err == nil {
		t.Fatal("expected error for unsigned bundle, got nil")
	}
}

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
	if err := verifySignature(context.Background(), v1); err != nil {
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
