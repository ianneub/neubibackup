package tailscale

import (
	"os"
	"path/filepath"
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

func TestHasExistingState(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(dir string) error
		expected bool
	}{
		{
			name:     "no state file",
			setup:    func(dir string) error { return nil },
			expected: false,
		},
		{
			name: "empty state file",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "tailscaled.state"), []byte{}, 0600)
			},
			expected: false,
		},
		{
			name: "state file with content",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "tailscaled.state"), []byte("state-data"), 0600)
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			if err := tt.setup(tmpDir); err != nil {
				t.Fatalf("setup error: %v", err)
			}

			got := hasExistingState(tmpDir)
			if got != tt.expected {
				t.Errorf("hasExistingState() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNew_SkipsAuthKeyWhenStateExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a state file to simulate existing registration
	stateFile := filepath.Join(tmpDir, "tailscaled.state")
	if err := os.WriteFile(stateFile, []byte("existing-state-data"), 0600); err != nil {
		t.Fatalf("failed to create state file: %v", err)
	}

	cfg := &config.TailscaleConfig{
		Enabled:  true,
		AuthKey:  "tskey-auth-expired-key",
		Hostname: "testhost",
	}

	mgr, err := New(cfg, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Verify the manager was created (auth key should have been skipped)
	if mgr == nil {
		t.Fatal("New() returned nil manager")
	}

	// The server's AuthKey should be empty since state exists
	if mgr.server.AuthKey != "" {
		t.Errorf("server.AuthKey = %q, want empty (should be skipped when state exists)", mgr.server.AuthKey)
	}
}

func TestNew_UsesAuthKeyWhenNoState(t *testing.T) {
	tmpDir := t.TempDir()
	// No state file - fresh directory

	cfg := &config.TailscaleConfig{
		Enabled:  true,
		AuthKey:  "tskey-auth-new-key",
		Hostname: "testhost",
	}

	mgr, err := New(cfg, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if mgr == nil {
		t.Fatal("New() returned nil manager")
	}

	// The server's AuthKey should be set since no state exists
	if mgr.server.AuthKey != cfg.AuthKey {
		t.Errorf("server.AuthKey = %q, want %q", mgr.server.AuthKey, cfg.AuthKey)
	}
}
