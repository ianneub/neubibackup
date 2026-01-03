// Package tailscale provides Tailscale network integration via tsnet.
package tailscale

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"neubibackup/internal/config"

	"tailscale.com/tsnet"
)

// Manager handles the Tailscale connection lifecycle.
type Manager struct {
	server    *tsnet.Server
	proxy     *Proxy
	cfg       *config.TailscaleConfig
	stateDir  string
	mu        sync.RWMutex
	started   bool
	proxyAddr string
}

// New creates a new Tailscale manager.
// The stateDir is where Tailscale will store its persistent state.
func New(cfg *config.TailscaleConfig, stateDir string) (*Manager, error) {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, fmt.Errorf("create tailscale state dir: %w", err)
	}

	hostname := cfg.Hostname
	if hostname == "" {
		hostname = "neubibackup"
	}

	srv := &tsnet.Server{
		Dir:       stateDir,
		Hostname:  hostname,
		AuthKey:   cfg.AuthKey,
		Ephemeral: false, // Always non-ephemeral for stable device registration
		Logf: func(format string, args ...any) {
			slog.Debug(fmt.Sprintf(format, args...), "component", "tailscale")
		},
	}

	return &Manager{
		server:   srv,
		cfg:      cfg,
		stateDir: stateDir,
	}, nil
}

// Start initializes the Tailscale connection and starts the SOCKS5 proxy.
// This will block until the connection is established or fails.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	slog.Info("Starting Tailscale connection...")

	// Start with timeout
	startCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// The Start() call may block while authenticating
	if err := m.server.Start(); err != nil {
		return fmt.Errorf("start tailscale: %w", err)
	}

	// Wait for connection to be ready
	status, err := m.server.Up(startCtx)
	if err != nil {
		m.server.Close()
		return fmt.Errorf("tailscale up: %w", err)
	}

	var ip string
	if len(status.TailscaleIPs) > 0 {
		ip = status.TailscaleIPs[0].String()
	}
	slog.Info("Tailscale connected", "hostname", m.server.Hostname, "ip", ip)

	// Start SOCKS5 proxy
	m.proxy = NewProxy(m.server)
	proxyAddr, err := m.proxy.Start()
	if err != nil {
		m.server.Close()
		return fmt.Errorf("start socks5 proxy: %w", err)
	}

	m.proxyAddr = proxyAddr
	m.started = true

	slog.Info("Tailscale SOCKS5 proxy listening", "address", proxyAddr)

	return nil
}

// Close shuts down the Tailscale connection and proxy.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	slog.Info("Shutting down Tailscale...")

	// Close proxy first
	if m.proxy != nil {
		if err := m.proxy.Close(); err != nil {
			slog.Warn("Error closing proxy", "error", err)
		}
		m.proxy = nil
	}

	// Close tsnet server
	err := m.server.Close()
	m.started = false
	m.proxyAddr = ""

	return err
}

// ProxyAddr returns the SOCKS5 proxy address (e.g., "127.0.0.1:12345").
// Returns empty string if not started.
func (m *Manager) ProxyAddr() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.proxyAddr
}

// IsStarted returns true if Tailscale is connected.
func (m *Manager) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// Status returns the current Tailscale connection status.
func (m *Manager) Status() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return "disconnected"
	}

	return "connected"
}

// Dial creates a connection through the Tailscale network.
func (m *Manager) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return nil, fmt.Errorf("tailscale not started")
	}

	return m.server.Dial(ctx, network, addr)
}
