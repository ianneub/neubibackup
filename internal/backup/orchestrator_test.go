package backup

import (
	"context"
	"errors"
	"sync"
	"testing"

	"neubibackup/internal/config"
	"neubibackup/internal/restic"
	"neubibackup/internal/state"
)

// mockTailscale is a test implementation of TailscaleProvider.
type mockTailscale struct {
	mu              sync.Mutex
	connectCalls    int
	disconnectCalls int
	proxyAddr       string
	connectErr      error
	disconnectErr   error
}

func (m *mockTailscale) Connect(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectCalls++
	if m.connectErr != nil {
		return "", m.connectErr
	}
	return m.proxyAddr, nil
}

func (m *mockTailscale) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disconnectCalls++
	return m.disconnectErr
}

func (m *mockTailscale) getConnectCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connectCalls
}

func (m *mockTailscale) getDisconnectCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.disconnectCalls
}

func TestNewOrchestrator(t *testing.T) {
	cfg := &config.Config{}
	appState := &state.State{}

	o := NewOrchestrator(cfg, appState)

	if o.cfg != cfg {
		t.Error("Config not set correctly")
	}
	if o.state != appState {
		t.Error("State not set correctly")
	}
	if o.notifier == nil {
		t.Error("Notifier should not be nil (defaults to NullNotifier)")
	}
}

func TestNewOrchestrator_WithOptions(t *testing.T) {
	cfg := &config.Config{}
	appState := &state.State{}
	notifier := &mockNotifier{}
	tailscale := &mockTailscale{proxyAddr: "127.0.0.1:1080"}
	var progressCalled bool
	progressCb := func(p restic.BackupProgress) {
		progressCalled = true
	}

	o := NewOrchestrator(cfg, appState,
		WithNotifier(notifier),
		WithTailscale(tailscale),
		WithProgressCallback(progressCb),
	)

	if o.notifier != notifier {
		t.Error("Notifier not set via option")
	}
	if o.tailscale != tailscale {
		t.Error("Tailscale not set via option")
	}
	if o.onProgress == nil {
		t.Error("Progress callback not set via option")
	}

	// Verify callback works
	o.onProgress(restic.BackupProgress{})
	if !progressCalled {
		t.Error("Progress callback not called")
	}
}

func TestResult(t *testing.T) {
	t.Run("success result", func(t *testing.T) {
		r := Result{Success: true, LogPath: "/path/to/log"}
		if !r.Success {
			t.Error("Success should be true")
		}
		if r.Cancelled {
			t.Error("Cancelled should be false")
		}
		if r.Error != nil {
			t.Error("Error should be nil")
		}
	})

	t.Run("failure result", func(t *testing.T) {
		err := errors.New("backup failed")
		r := Result{Success: false, Error: err, LogPath: "/path/to/log"}
		if r.Success {
			t.Error("Success should be false")
		}
		if r.Error != err {
			t.Error("Error not set correctly")
		}
	})

	t.Run("cancelled result", func(t *testing.T) {
		r := Result{Success: false, Cancelled: true, Error: context.Canceled}
		if r.Success {
			t.Error("Success should be false")
		}
		if !r.Cancelled {
			t.Error("Cancelled should be true")
		}
	})
}

func TestOrchestrator_TailscaleConnectionFailure(t *testing.T) {
	cfg := &config.Config{
		Tailscale: config.TailscaleConfig{
			Enabled: true,
			AuthKey: "test-auth-key", // Must have AuthKey for IsTailscaleEnabled() to return true
		},
		Repository: config.RepositoryConfig{
			Path:     "/tmp/test-repo",
			Password: "test",
		},
		Backup: config.BackupConfig{
			Paths: []string{"/tmp"},
		},
	}
	appState := &state.State{}
	notifier := &mockNotifier{}
	ts := &mockTailscale{
		connectErr: errors.New("connection refused"),
	}

	o := NewOrchestrator(cfg, appState,
		WithNotifier(notifier),
		WithTailscale(ts),
	)

	result := o.Run(context.Background())

	// Should fail due to Tailscale connection failure
	if result.Success {
		t.Error("Result should indicate failure")
	}
	if result.Error == nil {
		t.Error("Result should have an error")
	}
	if !errors.Is(result.Error, ts.connectErr) && result.Error.Error() != "tailscale connection failed: connection refused" {
		t.Errorf("Error = %v, want tailscale connection failure", result.Error)
	}

	// Should have tried to connect
	if ts.getConnectCalls() != 1 {
		t.Errorf("Connect calls = %d, want 1", ts.getConnectCalls())
	}

	// Should not have disconnected (never connected)
	if ts.getDisconnectCalls() != 0 {
		t.Errorf("Disconnect calls = %d, want 0", ts.getDisconnectCalls())
	}

	// Should have notified failure
	notifier.mu.Lock()
	failureCalls := len(notifier.failureCalls)
	notifier.mu.Unlock()
	if failureCalls != 1 {
		t.Errorf("Failure notifications = %d, want 1", failureCalls)
	}

	// Should have recorded failure in state
	if appState.Backup.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", appState.Backup.ConsecutiveFailures)
	}
}

func TestOrchestrator_handleTailscaleFailure(t *testing.T) {
	appState := &state.State{}
	notifier := &mockNotifier{}

	o := &Orchestrator{
		cfg:      &config.Config{},
		state:    appState,
		notifier: notifier,
	}

	testErr := errors.New("tailscale failed")
	o.handleTailscaleFailure(testErr)

	// Should have recorded failure
	if appState.Backup.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", appState.Backup.ConsecutiveFailures)
	}

	// Should have notified
	notifier.mu.Lock()
	failureCalls := len(notifier.failureCalls)
	notifier.mu.Unlock()
	if failureCalls != 1 {
		t.Errorf("Failure notifications = %d, want 1", failureCalls)
	}
}

func TestOrchestrator_handleBackupSuccess(t *testing.T) {
	appState := &state.State{
		Backup: state.BackupState{
			ConsecutiveFailures: 5, // Previous failures
		},
	}
	notifier := &mockNotifier{}

	o := &Orchestrator{
		cfg:      &config.Config{},
		state:    appState,
		notifier: notifier,
	}

	o.handleBackupSuccess()

	// Should have reset consecutive failures
	if appState.Backup.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", appState.Backup.ConsecutiveFailures)
	}

	// Should have recorded success time
	if appState.Backup.LastSuccess.IsZero() {
		t.Error("LastSuccess should be set")
	}

	// Should have notified success
	notifier.mu.Lock()
	successCalls := len(notifier.successCalls)
	notifier.mu.Unlock()
	if successCalls != 1 {
		t.Errorf("Success notifications = %d, want 1", successCalls)
	}
}

func TestOrchestrator_recordFailure(t *testing.T) {
	appState := &state.State{}

	o := &Orchestrator{
		cfg:      &config.Config{},
		state:    appState,
		notifier: &NullNotifier{},
	}

	testErr := errors.New("backup failed")
	o.recordFailure(testErr)

	if appState.Backup.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", appState.Backup.ConsecutiveFailures)
	}
	if appState.Backup.LastError != "backup failed" {
		t.Errorf("LastError = %q, want %q", appState.Backup.LastError, "backup failed")
	}
}

func TestOrchestrator_wrapProgressCallback_nil(t *testing.T) {
	o := &Orchestrator{}

	cb := o.wrapProgressCallback()
	if cb != nil {
		t.Error("wrapProgressCallback should return nil when onProgress is nil")
	}
}

func TestOrchestrator_wrapProgressCallback_nonNil(t *testing.T) {
	var called bool
	var receivedProgress restic.BackupProgress

	o := &Orchestrator{
		onProgress: func(p restic.BackupProgress) {
			called = true
			receivedProgress = p
		},
	}

	cb := o.wrapProgressCallback()
	if cb == nil {
		t.Fatal("wrapProgressCallback should return non-nil when onProgress is set")
	}

	testProgress := restic.BackupProgress{
		PercentDone: 50.5,
		TotalFiles:  100,
	}
	cb(testProgress)

	if !called {
		t.Error("Progress callback was not called")
	}
	if receivedProgress.PercentDone != 50.5 || receivedProgress.TotalFiles != 100 {
		t.Errorf("Received progress = %+v, want %+v", receivedProgress, testProgress)
	}
}

func TestTailscaleProviderInterface(t *testing.T) {
	// Verify mockTailscale implements TailscaleProvider
	var _ TailscaleProvider = &mockTailscale{}
}
