package backup

import (
	"errors"
	"sync"
	"testing"

	"neubibackup/internal/config"
)

// mockNotifier is a test implementation of Notifier that records calls.
type mockNotifier struct {
	mu             sync.Mutex
	startCalls     int
	successCalls   []string
	cancelledCalls int
	failureCalls   []struct {
		errMsg string
		logs   string
	}
	startErr     error
	successErr   error
	failureErr   error
	cancelledErr error
}

func (m *mockNotifier) NotifyStart() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalls++
	return m.startErr
}

func (m *mockNotifier) NotifySuccess(message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.successCalls = append(m.successCalls, message)
	return m.successErr
}

func (m *mockNotifier) NotifyFailure(errMsg string, logs string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failureCalls = append(m.failureCalls, struct {
		errMsg string
		logs   string
	}{errMsg, logs})
	return m.failureErr
}

func (m *mockNotifier) NotifyCancelled() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelledCalls++
	return m.cancelledErr
}

func TestNullNotifier(t *testing.T) {
	n := &NullNotifier{}

	// All methods should return nil
	if err := n.NotifyStart(); err != nil {
		t.Errorf("NotifyStart() returned error: %v", err)
	}
	if err := n.NotifySuccess("test"); err != nil {
		t.Errorf("NotifySuccess() returned error: %v", err)
	}
	if err := n.NotifyFailure("error", "logs"); err != nil {
		t.Errorf("NotifyFailure() returned error: %v", err)
	}
	if err := n.NotifyCancelled(); err != nil {
		t.Errorf("NotifyCancelled() returned error: %v", err)
	}
}

func TestCompositeNotifier_NoServicesConfigured(t *testing.T) {
	n := NewCompositeNotifier(NotifierConfig{})

	// All methods should succeed silently with no services
	if err := n.NotifyStart(); err != nil {
		t.Errorf("NotifyStart() returned error: %v", err)
	}
	if err := n.NotifySuccess("test"); err != nil {
		t.Errorf("NotifySuccess() returned error: %v", err)
	}
	if err := n.NotifyFailure("error", "logs"); err != nil {
		t.Errorf("NotifyFailure() returned error: %v", err)
	}
	if err := n.NotifyCancelled(); err != nil {
		t.Errorf("NotifyCancelled() returned error: %v", err)
	}

	if n.HasHealthchecks() {
		t.Error("HasHealthchecks() should be false")
	}
	if n.HasPushover() {
		t.Error("HasPushover() should be false")
	}
}

func TestCompositeNotifier_HealthchecksOnly(t *testing.T) {
	cfg := NotifierConfig{
		Healthchecks: config.HealthchecksConfig{
			Enabled:           true,
			PingURL:           "https://hc-ping.com/test-uuid",
			SendLogsOnFailure: true,
		},
	}

	n := NewCompositeNotifier(cfg)

	if !n.HasHealthchecks() {
		t.Error("HasHealthchecks() should be true")
	}
	if n.HasPushover() {
		t.Error("HasPushover() should be false")
	}
}

func TestCompositeNotifier_PushoverOnly(t *testing.T) {
	cfg := NotifierConfig{
		Pushover: config.PushoverConfig{
			Enabled:   true,
			APIToken:  "test-token",
			UserKey:   "test-user",
			OnSuccess: true,
			OnFailure: true,
		},
	}

	n := NewCompositeNotifier(cfg)

	if n.HasHealthchecks() {
		t.Error("HasHealthchecks() should be false")
	}
	if !n.HasPushover() {
		t.Error("HasPushover() should be true")
	}
}

func TestCompositeNotifier_BothServicesConfigured(t *testing.T) {
	cfg := NotifierConfig{
		Healthchecks: config.HealthchecksConfig{
			Enabled: true,
			PingURL: "https://hc-ping.com/test-uuid",
		},
		Pushover: config.PushoverConfig{
			Enabled:  true,
			APIToken: "test-token",
			UserKey:  "test-user",
		},
	}

	n := NewCompositeNotifier(cfg)

	if !n.HasHealthchecks() {
		t.Error("HasHealthchecks() should be true")
	}
	if !n.HasPushover() {
		t.Error("HasPushover() should be true")
	}
}

func TestCompositeNotifier_DisabledHealthchecks(t *testing.T) {
	cfg := NotifierConfig{
		Healthchecks: config.HealthchecksConfig{
			Enabled: false, // Explicitly disabled
			PingURL: "https://hc-ping.com/test-uuid",
		},
	}

	n := NewCompositeNotifier(cfg)

	if n.HasHealthchecks() {
		t.Error("HasHealthchecks() should be false when Enabled=false")
	}
}

func TestCompositeNotifier_EmptyPingURL(t *testing.T) {
	cfg := NotifierConfig{
		Healthchecks: config.HealthchecksConfig{
			Enabled: true,
			PingURL: "", // Empty URL
		},
	}

	n := NewCompositeNotifier(cfg)

	if n.HasHealthchecks() {
		t.Error("HasHealthchecks() should be false when PingURL is empty")
	}
}

func TestMockNotifier_RecordsCalls(t *testing.T) {
	m := &mockNotifier{}

	// Test start
	if err := m.NotifyStart(); err != nil {
		t.Errorf("NotifyStart() returned error: %v", err)
	}
	if m.startCalls != 1 {
		t.Errorf("startCalls = %d, want 1", m.startCalls)
	}

	// Test success
	if err := m.NotifySuccess("backup done"); err != nil {
		t.Errorf("NotifySuccess() returned error: %v", err)
	}
	if len(m.successCalls) != 1 || m.successCalls[0] != "backup done" {
		t.Errorf("successCalls = %v, want [backup done]", m.successCalls)
	}

	// Test failure
	if err := m.NotifyFailure("something broke", "log output"); err != nil {
		t.Errorf("NotifyFailure() returned error: %v", err)
	}
	if len(m.failureCalls) != 1 {
		t.Errorf("failureCalls length = %d, want 1", len(m.failureCalls))
	}
	if m.failureCalls[0].errMsg != "something broke" || m.failureCalls[0].logs != "log output" {
		t.Errorf("failureCalls[0] = %v, want {something broke, log output}", m.failureCalls[0])
	}

	// Test cancelled
	if err := m.NotifyCancelled(); err != nil {
		t.Errorf("NotifyCancelled() returned error: %v", err)
	}
	if m.cancelledCalls != 1 {
		t.Errorf("cancelledCalls = %d, want 1", m.cancelledCalls)
	}
}

func TestMockNotifier_ReturnsErrors(t *testing.T) {
	testErr := errors.New("test error")

	t.Run("start error", func(t *testing.T) {
		m := &mockNotifier{startErr: testErr}
		if err := m.NotifyStart(); err != testErr {
			t.Errorf("NotifyStart() = %v, want %v", err, testErr)
		}
	})

	t.Run("success error", func(t *testing.T) {
		m := &mockNotifier{successErr: testErr}
		if err := m.NotifySuccess("msg"); err != testErr {
			t.Errorf("NotifySuccess() = %v, want %v", err, testErr)
		}
	})

	t.Run("failure error", func(t *testing.T) {
		m := &mockNotifier{failureErr: testErr}
		if err := m.NotifyFailure("err", "logs"); err != testErr {
			t.Errorf("NotifyFailure() = %v, want %v", err, testErr)
		}
	})

	t.Run("cancelled error", func(t *testing.T) {
		m := &mockNotifier{cancelledErr: testErr}
		if err := m.NotifyCancelled(); err != testErr {
			t.Errorf("NotifyCancelled() = %v, want %v", err, testErr)
		}
	})
}

func TestNotifierInterface(t *testing.T) {
	// Verify types implement the Notifier interface
	var _ Notifier = &NullNotifier{}
	var _ Notifier = &CompositeNotifier{}
	var _ Notifier = &mockNotifier{}
}
