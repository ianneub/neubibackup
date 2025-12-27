// Package pushover provides integration with Pushover notifications.
package pushover

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiURL = "https://api.pushover.net/1/messages.json"

// Client manages Pushover notifications.
type Client struct {
	Token      string // API token
	UserKey    string // User/group key
	HTTPClient *http.Client
}

// New creates a new Pushover client.
func New(token, userKey string) *Client {
	return &Client{
		Token:   token,
		UserKey: userKey,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Priority levels for notifications.
const (
	PriorityLowest    = -2
	PriorityLow       = -1
	PriorityNormal    = 0
	PriorityHigh      = 1
	PriorityEmergency = 2
)

// Message represents a Pushover notification.
type Message struct {
	Title    string
	Body     string
	Priority int
	URL      string
	URLTitle string
}

// Send sends a notification.
func (c *Client) Send(msg Message) error {
	if c.Token == "" || c.UserKey == "" {
		return nil // Silently skip if not configured
	}

	data := url.Values{
		"token":   {c.Token},
		"user":    {c.UserKey},
		"message": {msg.Body},
	}

	if msg.Title != "" {
		data.Set("title", msg.Title)
	}
	if msg.Priority != 0 {
		data.Set("priority", fmt.Sprintf("%d", msg.Priority))
	}
	if msg.URL != "" {
		data.Set("url", msg.URL)
	}
	if msg.URLTitle != "" {
		data.Set("url_title", msg.URLTitle)
	}

	resp, err := c.HTTPClient.PostForm(apiURL, data)
	if err != nil {
		return fmt.Errorf("send pushover: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result struct {
			Errors []string `json:"errors"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && len(result.Errors) > 0 {
			return fmt.Errorf("pushover error: %s", strings.Join(result.Errors, ", "))
		}
		return fmt.Errorf("pushover returned status %d", resp.StatusCode)
	}

	return nil
}

// SendFailure sends a high-priority failure notification.
func (c *Client) SendFailure(message string) error {
	return c.Send(Message{
		Title:    "NeubiBackup Failed",
		Body:     message,
		Priority: PriorityHigh,
	})
}

// SendSuccess sends a normal-priority success notification.
func (c *Client) SendSuccess(message string) error {
	return c.Send(Message{
		Title:    "NeubiBackup Success",
		Body:     message,
		Priority: PriorityNormal,
	})
}
