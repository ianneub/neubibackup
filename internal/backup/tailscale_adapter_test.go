package backup

import (
	"testing"

	"neubibackup/internal/config"
)

func TestNewTailscaleAdapter(t *testing.T) {
	cfg := &config.TailscaleConfig{
		Enabled:  true,
		AuthKey:  "tskey-auth-xxx",
		Hostname: "backup-client",
	}
	stateDir := "/tmp/tailscale-state"

	adapter := NewTailscaleAdapter(cfg, stateDir)

	if adapter == nil {
		t.Fatal("NewTailscaleAdapter() returned nil")
	}
	if adapter.cfg != cfg {
		t.Error("adapter.cfg not set correctly")
	}
	if adapter.stateDir != stateDir {
		t.Errorf("adapter.stateDir = %q, want %q", adapter.stateDir, stateDir)
	}
	if adapter.manager != nil {
		t.Error("adapter.manager should be nil before Connect()")
	}
}

func TestTailscaleAdapter_DisconnectWithoutConnect(t *testing.T) {
	cfg := &config.TailscaleConfig{
		Enabled: true,
	}

	adapter := NewTailscaleAdapter(cfg, "/tmp/state")

	// Disconnect without Connect should be safe
	err := adapter.Disconnect()
	if err != nil {
		t.Errorf("Disconnect() without prior Connect() returned error: %v", err)
	}
}

func TestTailscaleAdapter_DisconnectMultipleTimes(t *testing.T) {
	cfg := &config.TailscaleConfig{
		Enabled: true,
	}

	adapter := NewTailscaleAdapter(cfg, "/tmp/state")

	// Multiple disconnects should be safe
	for i := 0; i < 3; i++ {
		err := adapter.Disconnect()
		if err != nil {
			t.Errorf("Disconnect() call %d returned error: %v", i+1, err)
		}
	}
}

func TestTailscaleAdapter_ImplementsTailscaleProvider(t *testing.T) {
	cfg := &config.TailscaleConfig{}
	adapter := NewTailscaleAdapter(cfg, "/tmp/state")

	// Verify adapter implements TailscaleProvider interface
	var _ TailscaleProvider = adapter
}
