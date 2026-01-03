package tailscale

import (
	"testing"

	"neubibackup/internal/config"
)

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.TailscaleConfig{
		Enabled:  true,
		AuthKey:  "tskey-auth-test123",
		Hostname: "testhost",
	}

	mgr, err := New(cfg, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if mgr == nil {
		t.Fatal("New() returned nil manager")
	}

	// Verify manager is not started
	if mgr.IsStarted() {
		t.Error("Manager should not be started initially")
	}

	// Verify status
	if mgr.Status() != "disconnected" {
		t.Errorf("Status() = %q, want %q", mgr.Status(), "disconnected")
	}

	// Verify proxy address is empty when not started
	if mgr.ProxyAddr() != "" {
		t.Errorf("ProxyAddr() = %q, want empty", mgr.ProxyAddr())
	}
}

func TestNew_DefaultHostname(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.TailscaleConfig{
		Enabled: true,
		AuthKey: "tskey-auth-test123",
		// Hostname is empty - should default to "neubibackup"
	}

	mgr, err := New(cfg, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// The hostname default is set in the tsnet.Server, not accessible from Manager
	// We just verify the manager was created successfully
	if mgr == nil {
		t.Fatal("New() returned nil manager")
	}
}

func TestNew_CreateStateDir(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := tmpDir + "/nested/tailscale/state"

	cfg := &config.TailscaleConfig{
		Enabled:  true,
		AuthKey:  "tskey-auth-test123",
		Hostname: "testhost",
	}

	mgr, err := New(cfg, stateDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if mgr == nil {
		t.Fatal("New() returned nil manager")
	}
}

func TestClose_NotStarted(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.TailscaleConfig{
		Enabled:  true,
		AuthKey:  "tskey-auth-test123",
		Hostname: "testhost",
	}

	mgr, err := New(cfg, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Close should not error when not started
	if err := mgr.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}

	// Should still be not started
	if mgr.IsStarted() {
		t.Error("IsStarted() should be false after Close()")
	}
}

func TestIsStarted_ThreadSafe(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.TailscaleConfig{
		Enabled:  true,
		AuthKey:  "tskey-auth-test123",
		Hostname: "testhost",
	}

	mgr, err := New(cfg, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Call IsStarted from multiple goroutines to verify thread safety
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			_ = mgr.IsStarted()
			_ = mgr.Status()
			_ = mgr.ProxyAddr()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
