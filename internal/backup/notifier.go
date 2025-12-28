// Package backup provides backup orchestration with notification support.
package backup

import (
	"neubibackup/internal/config"
	"neubibackup/internal/healthchecks"
	"neubibackup/internal/pushover"
)

// Notifier defines the interface for backup notifications.
// Implementations handle notification delivery to various services.
type Notifier interface {
	// NotifyStart signals that a backup has started.
	NotifyStart() error

	// NotifySuccess signals that a backup completed successfully.
	NotifySuccess(message string) error

	// NotifyFailure signals that a backup failed.
	// The logs parameter contains the backup log output for services that support it.
	NotifyFailure(errMsg string, logs string) error
}

// CompositeNotifier combines multiple notification services into a single Notifier.
// It sends notifications to all configured services, logging warnings for individual failures
// but not failing the overall notification.
type CompositeNotifier struct {
	healthchecks *healthchecks.Client
	pushover     *pushover.Client
	pushoverCfg  config.PushoverConfig
	hcCfg        config.HealthchecksConfig
}

// NotifierConfig contains the configuration for creating a CompositeNotifier.
type NotifierConfig struct {
	Healthchecks config.HealthchecksConfig
	Pushover     config.PushoverConfig
}

// NewCompositeNotifier creates a new CompositeNotifier from the given configuration.
// It creates clients for all enabled notification services.
func NewCompositeNotifier(cfg NotifierConfig) *CompositeNotifier {
	n := &CompositeNotifier{
		hcCfg:       cfg.Healthchecks,
		pushoverCfg: cfg.Pushover,
	}

	// Create healthchecks client if configured
	if cfg.Healthchecks.Enabled && cfg.Healthchecks.PingURL != "" {
		n.healthchecks = healthchecks.New(cfg.Healthchecks.PingURL)
	}

	// Create pushover client if configured
	if cfg.Pushover.Enabled {
		n.pushover = pushover.New(cfg.Pushover.APIToken, cfg.Pushover.UserKey)
	}

	return n
}

// NotifyStart signals that a backup has started.
// Sends start ping to healthchecks.io if configured.
func (n *CompositeNotifier) NotifyStart() error {
	if n.healthchecks != nil {
		return n.healthchecks.Start()
	}
	return nil
}

// NotifySuccess signals that a backup completed successfully.
// Sends success pings to healthchecks.io and pushover if configured.
func (n *CompositeNotifier) NotifySuccess(message string) error {
	var firstErr error

	// Healthchecks success ping
	if n.healthchecks != nil {
		if err := n.healthchecks.Success(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Pushover success notification (if configured for success)
	if n.pushover != nil && n.pushoverCfg.OnSuccess {
		if err := n.pushover.SendSuccess(message); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// NotifyFailure signals that a backup failed.
// Sends failure pings to healthchecks.io (with logs if configured) and pushover.
func (n *CompositeNotifier) NotifyFailure(errMsg string, logs string) error {
	var firstErr error

	// Healthchecks fail ping
	if n.healthchecks != nil {
		var logsToSend string
		if n.hcCfg.SendLogsOnFailure {
			logsToSend = logs
		}
		if err := n.healthchecks.Fail(logsToSend); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Pushover failure notification
	if n.pushover != nil && n.pushoverCfg.OnFailure {
		if err := n.pushover.SendFailure(errMsg); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// HasHealthchecks returns true if healthchecks.io is configured.
func (n *CompositeNotifier) HasHealthchecks() bool {
	return n.healthchecks != nil
}

// HasPushover returns true if Pushover is configured.
func (n *CompositeNotifier) HasPushover() bool {
	return n.pushover != nil
}

// NullNotifier is a Notifier implementation that does nothing.
// Useful for testing or when notifications are disabled.
type NullNotifier struct{}

// NotifyStart does nothing and returns nil.
func (n *NullNotifier) NotifyStart() error {
	return nil
}

// NotifySuccess does nothing and returns nil.
func (n *NullNotifier) NotifySuccess(_ string) error {
	return nil
}

// NotifyFailure does nothing and returns nil.
func (n *NullNotifier) NotifyFailure(_, _ string) error {
	return nil
}
