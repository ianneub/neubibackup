//go:build darwin

package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// extractZipToBundle writes the contents of a single-.app ZIP into destDir.
// destDir must not exist (or must be empty) when called. The ZIP must contain
// exactly one top-level directory whose name ends in ".app"; entries outside
// that root, or paths that try to escape destDir, are rejected.
//
// On error, destDir may be partially populated. Callers are responsible for
// removing it (e.g., os.RemoveAll) before retrying.
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

	_, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy to %q: %w", dest, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %q: %w", dest, closeErr)
	}
	return nil
}

// verifySignature runs `codesign --verify --deep --strict` against bundlePath
// and returns the codesign output as part of the error if the verification
// fails. The context is used to cancel codesign if the caller times out or
// aborts.
func verifySignature(ctx context.Context, bundlePath string) error {
	cmd := exec.CommandContext(ctx, "codesign", "--verify", "--deep", "--strict", bundlePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign verify failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

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

	if err := verifySignature(ctx, newPath); err != nil {
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
