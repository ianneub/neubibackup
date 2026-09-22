//go:build linux

package updater

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func stubExec(t *testing.T, fn func(argv0 string, argv []string, envv []string) error) {
	t.Helper()
	orig := execFn
	execFn = fn
	t.Cleanup(func() { execFn = orig })
}

func stubLaunchPath(t *testing.T, path string, err error) {
	t.Helper()
	origPath, origErr := launchPath, launchPathErr
	launchPath, launchPathErr = path, err
	t.Cleanup(func() { launchPath, launchPathErr = origPath, origErr })
}

func TestRestartExecsLaunchPath(t *testing.T) {
	stubLaunchPath(t, "/home/user/.local/bin/neubibackup", nil)
	var gotPath string
	var gotArgs []string
	stubExec(t, func(argv0 string, argv []string, envv []string) error {
		gotPath, gotArgs = argv0, argv
		return nil
	})

	if err := Restart(); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if gotPath != "/home/user/.local/bin/neubibackup" {
		t.Errorf("exec path = %q, want the launch path", gotPath)
	}
	if !slices.Equal(gotArgs, os.Args) {
		t.Errorf("exec args = %v, want %v", gotArgs, os.Args)
	}
}

func TestRestartReturnsExecError(t *testing.T) {
	stubExec(t, func(string, []string, []string) error {
		return errors.New("permission denied")
	})

	if err := Restart(); err == nil {
		t.Fatal("Restart: expected error, got nil")
	}
}

func TestRestartReportsLaunchPathError(t *testing.T) {
	stubLaunchPath(t, "", errors.New("readlink /proc/self/exe: no such file"))
	stubExec(t, func(string, []string, []string) error {
		t.Error("exec called despite unknown launch path")
		return nil
	})

	if err := Restart(); err == nil {
		t.Fatal("Restart: expected error, got nil")
	}
}

// Regression: after go-selfupdate swaps the binary, os.Executable points at
// the deleted .<name>.old file, so v2.4.0 failed to restart with
// "exec .../.neubibackup.old: no such file or directory". This runs a real
// process, swaps its binary exactly the way go-selfupdate does, and checks
// that Restart execs the new binary at the original path.
func TestRestartAfterSelfUpdateSwap(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	dir := t.TempDir()
	app := filepath.Join(dir, "neubibackup")
	copyExecutable(t, self, app)

	cmd := exec.Command(app, "-test.run=^TestRestartHelperProcess$")
	cmd.Env = append(os.Environ(), "NEUBIBACKUP_RESTART_HELPER="+dir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	lines := readLines(stdout)

	expectLine(t, lines, "ready")

	// go-selfupdate's update.Apply: write .<name>.new, rename the running
	// binary to .<name>.old, rename .new into place, delete .old.
	newPath := filepath.Join(dir, ".neubibackup.new")
	oldPath := filepath.Join(dir, ".neubibackup.old")
	copyExecutable(t, self, newPath)
	if err := os.Rename(app, oldPath); err != nil {
		t.Fatalf("rename to .old: %v", err)
	}
	if err := os.Rename(newPath, app); err != nil {
		t.Fatalf("rename .new into place: %v", err)
	}
	if err := os.Remove(oldPath); err != nil {
		t.Fatalf("remove .old: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "swapped"), nil, 0600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	fmt.Fprintln(stdin, "restart")
	expectLine(t, lines, "restarted from "+app)
}

// TestRestartHelperProcess is the child process for
// TestRestartAfterSelfUpdateSwap; it is skipped in normal test runs.
func TestRestartHelperProcess(t *testing.T) {
	dir := os.Getenv("NEUBIBACKUP_RESTART_HELPER")
	if dir == "" {
		t.Skip("helper process for TestRestartAfterSelfUpdateSwap")
	}
	if _, err := os.Stat(filepath.Join(dir, "swapped")); err == nil {
		// Second life: we are the new binary, exec'd by Restart.
		fmt.Println("restarted from", launchPath)
		os.Exit(0)
	}
	fmt.Println("ready")
	bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Println("restart failed:", Restart())
	os.Exit(1)
}

func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, b, 0755); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func readLines(r io.Reader) <-chan string {
	ch := make(chan string)
	go func() {
		defer close(ch)
		s := bufio.NewScanner(r)
		for s.Scan() {
			ch <- s.Text()
		}
	}()
	return ch
}

func expectLine(t *testing.T, lines <-chan string, want string) {
	t.Helper()
	select {
	case got, ok := <-lines:
		if !ok {
			t.Fatalf("helper exited before printing %q", want)
		}
		if strings.TrimSpace(got) != want {
			t.Fatalf("helper printed %q, want %q", got, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for helper to print %q", want)
	}
}
