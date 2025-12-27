// Package healthchecks provides integration with healthchecks.io.
package healthchecks

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client manages healthchecks.io pings.
type Client struct {
	// PingURL is the full ping URL (e.g., https://hc-ping.com/uuid)
	PingURL    string
	HTTPClient *http.Client
}

// New creates a new healthchecks client.
func New(pingURL string) *Client {
	return &Client{
		PingURL: strings.TrimSuffix(pingURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Start signals that a job has started.
// Call this at the beginning of a backup.
func (c *Client) Start() error {
	return c.ping("/start", nil)
}

// Success signals that a job completed successfully.
func (c *Client) Success() error {
	return c.ping("", nil)
}

// Fail signals that a job failed.
// Optionally include log output in the request body.
func (c *Client) Fail(logs string) error {
	var body io.Reader
	if logs != "" {
		body = strings.NewReader(logs)
	}
	return c.ping("/fail", body)
}

// Log sends a log message without changing the check status.
func (c *Client) Log(message string) error {
	return c.ping("/log", strings.NewReader(message))
}

func (c *Client) ping(suffix string, body io.Reader) error {
	if c.PingURL == "" {
		return nil // Silently skip if not configured
	}

	url := c.PingURL + suffix

	var req *http.Request
	var err error

	if body != nil {
		req, err = http.NewRequest(http.MethodPost, url, body)
	} else {
		req, err = http.NewRequest(http.MethodGet, url, nil)
	}
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("ping healthchecks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("healthchecks returned status %d", resp.StatusCode)
	}

	return nil
}
