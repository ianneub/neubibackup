package backup

import (
	"context"
	"fmt"

	"neubibackup/internal/config"
	"neubibackup/internal/tailscale"
)

// TailscaleAdapter adapts tailscale.Manager to the TailscaleProvider interface.
// It handles the full lifecycle of Tailscale connection.
type TailscaleAdapter struct {
	cfg      *config.TailscaleConfig
	stateDir string
	manager  *tailscale.Manager
}

// NewTailscaleAdapter creates a new TailscaleAdapter.
// The stateDir is where Tailscale will store its persistent state.
func NewTailscaleAdapter(cfg *config.TailscaleConfig, stateDir string) *TailscaleAdapter {
	return &TailscaleAdapter{
		cfg:      cfg,
		stateDir: stateDir,
	}
}

// Connect creates a new Tailscale manager and establishes a connection.
func (a *TailscaleAdapter) Connect(ctx context.Context) (string, error) {
	manager, err := tailscale.New(a.cfg, a.stateDir)
	if err != nil {
		return "", fmt.Errorf("create tailscale manager: %w", err)
	}

	if err := manager.Start(ctx); err != nil {
		return "", fmt.Errorf("start tailscale: %w", err)
	}

	a.manager = manager
	return manager.ProxyAddr(), nil
}

// Disconnect closes the Tailscale connection.
func (a *TailscaleAdapter) Disconnect() error {
	if a.manager == nil {
		return nil
	}
	err := a.manager.Close()
	a.manager = nil
	return err
}
