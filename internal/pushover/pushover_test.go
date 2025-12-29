package pushover

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	client := New("token123", "user456")

	if client.Token != "token123" {
		t.Errorf("New().Token = %q, want %q", client.Token, "token123")
	}
	if client.UserKey != "user456" {
		t.Errorf("New().UserKey = %q, want %q", client.UserKey, "user456")
	}
	if client.HTTPClient == nil {
		t.Error("New() should set HTTPClient")
	}
}

func TestClient_Send(t *testing.T) {
	tests := []struct {
		name           string
		msg            Message
		wantToken      string
		wantUser       string
		wantMessage    string
		wantTitle      string
		wantPriority   string
		wantURL        string
		wantURLTitle   string
	}{
		{
			name: "basic message",
			msg: Message{
				Body: "test message",
			},
			wantToken:   "token123",
			wantUser:    "user456",
			wantMessage: "test message",
		},
		{
			name: "message with title",
			msg: Message{
				Title: "Test Title",
				Body:  "test message",
			},
			wantToken:   "token123",
			wantUser:    "user456",
			wantMessage: "test message",
			wantTitle:   "Test Title",
		},
		{
			name: "message with priority",
			msg: Message{
				Body:     "high priority",
				Priority: PriorityHigh,
			},
			wantToken:    "token123",
			wantUser:     "user456",
			wantMessage:  "high priority",
			wantPriority: "1",
		},
		{
			name: "message with URL",
			msg: Message{
				Body:     "check link",
				URL:      "https://example.com",
				URLTitle: "Example",
			},
			wantToken:    "token123",
			wantUser:     "user456",
			wantMessage:  "check link",
			wantURL:      "https://example.com",
			wantURLTitle: "Example",
		},
		{
			name: "full message",
			msg: Message{
				Title:    "Full Test",
				Body:     "complete message",
				Priority: PriorityEmergency,
				URL:      "https://example.com/detail",
				URLTitle: "Details",
			},
			wantToken:    "token123",
			wantUser:     "user456",
			wantMessage:  "complete message",
			wantTitle:    "Full Test",
			wantPriority: "2",
			wantURL:      "https://example.com/detail",
			wantURLTitle: "Details",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedData url.Values

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if err := r.ParseForm(); err != nil {
					t.Errorf("failed to parse form: %v", err)
				}
				receivedData = r.PostForm
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client := New("token123", "user456")
			client.HTTPClient = server.Client()

			// Override the API URL by using a custom transport
			originalTransport := client.HTTPClient.Transport
			client.HTTPClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = strings.TrimPrefix(server.URL, "http://")
				if originalTransport != nil {
					return originalTransport.RoundTrip(req)
				}
				return http.DefaultTransport.RoundTrip(req)
			})

			err := client.Send(tt.msg)

			if err != nil {
				t.Errorf("Send() unexpected error: %v", err)
			}

			if got := receivedData.Get("token"); got != tt.wantToken {
				t.Errorf("token = %q, want %q", got, tt.wantToken)
			}
			if got := receivedData.Get("user"); got != tt.wantUser {
				t.Errorf("user = %q, want %q", got, tt.wantUser)
			}
			if got := receivedData.Get("message"); got != tt.wantMessage {
				t.Errorf("message = %q, want %q", got, tt.wantMessage)
			}
			if tt.wantTitle != "" {
				if got := receivedData.Get("title"); got != tt.wantTitle {
					t.Errorf("title = %q, want %q", got, tt.wantTitle)
				}
			}
			if tt.wantPriority != "" {
				if got := receivedData.Get("priority"); got != tt.wantPriority {
					t.Errorf("priority = %q, want %q", got, tt.wantPriority)
				}
			}
			if tt.wantURL != "" {
				if got := receivedData.Get("url"); got != tt.wantURL {
					t.Errorf("url = %q, want %q", got, tt.wantURL)
				}
			}
			if tt.wantURLTitle != "" {
				if got := receivedData.Get("url_title"); got != tt.wantURLTitle {
					t.Errorf("url_title = %q, want %q", got, tt.wantURLTitle)
				}
			}
		})
	}
}

// roundTripperFunc is a helper to create a custom RoundTripper
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClient_Send_EmptyCredentials(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		userKey string
	}{
		{
			name:    "empty token",
			token:   "",
			userKey: "user456",
		},
		{
			name:    "empty user key",
			token:   "token123",
			userKey: "",
		},
		{
			name:    "both empty",
			token:   "",
			userKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := New(tt.token, tt.userKey)
			err := client.Send(Message{Body: "test"})

			if err != nil {
				t.Errorf("Send() with empty credentials returned error: %v", err)
			}
		})
	}
}

func TestClient_Send_HTTPError(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   interface{}
		wantErrContain string
	}{
		{
			name:           "400 with error message",
			statusCode:     http.StatusBadRequest,
			responseBody:   map[string]interface{}{"errors": []string{"invalid token", "user not found"}},
			wantErrContain: "invalid token, user not found",
		},
		{
			name:           "500 without error details",
			statusCode:     http.StatusInternalServerError,
			responseBody:   nil,
			wantErrContain: "status 500",
		},
		{
			name:           "401 unauthorized",
			statusCode:     http.StatusUnauthorized,
			responseBody:   map[string]interface{}{"errors": []string{"application token is invalid"}},
			wantErrContain: "application token is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				if tt.responseBody != nil {
					json.NewEncoder(w).Encode(tt.responseBody)
				}
			}))
			defer server.Close()

			client := New("token123", "user456")
			client.HTTPClient = server.Client()
			client.HTTPClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = strings.TrimPrefix(server.URL, "http://")
				return http.DefaultTransport.RoundTrip(req)
			})

			err := client.Send(Message{Body: "test"})

			if err == nil {
				t.Error("expected error, got nil")
			} else if !strings.Contains(err.Error(), tt.wantErrContain) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErrContain)
			}
		})
	}
}

func TestClient_Send_NetworkError(t *testing.T) {
	client := New("token123", "user456")
	client.HTTPClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = "localhost:1" // Invalid port
		return http.DefaultTransport.RoundTrip(req)
	})

	err := client.Send(Message{Body: "test"})

	if err == nil {
		t.Error("expected error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "send pushover") {
		t.Errorf("error = %q, want to contain 'send pushover'", err.Error())
	}
}

func TestClient_SendFailure(t *testing.T) {
	var receivedData url.Values

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		receivedData = r.PostForm
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New("token123", "user456")
	client.HTTPClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(server.URL, "http://")
		return http.DefaultTransport.RoundTrip(req)
	})

	err := client.SendFailure("Backup failed: disk full")

	if err != nil {
		t.Errorf("SendFailure() unexpected error: %v", err)
	}

	if got := receivedData.Get("title"); got != "NeubiBackup Failed" {
		t.Errorf("title = %q, want %q", got, "NeubiBackup Failed")
	}
	if got := receivedData.Get("message"); got != "Backup failed: disk full" {
		t.Errorf("message = %q, want %q", got, "Backup failed: disk full")
	}
	if got := receivedData.Get("priority"); got != "1" {
		t.Errorf("priority = %q, want %q (PriorityHigh)", got, "1")
	}
}

func TestClient_SendSuccess(t *testing.T) {
	var receivedData url.Values

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		receivedData = r.PostForm
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New("token123", "user456")
	client.HTTPClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(server.URL, "http://")
		return http.DefaultTransport.RoundTrip(req)
	})

	err := client.SendSuccess("Backup completed: 1.5 GB")

	if err != nil {
		t.Errorf("SendSuccess() unexpected error: %v", err)
	}

	if got := receivedData.Get("title"); got != "NeubiBackup Success" {
		t.Errorf("title = %q, want %q", got, "NeubiBackup Success")
	}
	if got := receivedData.Get("message"); got != "Backup completed: 1.5 GB" {
		t.Errorf("message = %q, want %q", got, "Backup completed: 1.5 GB")
	}
	// Priority 0 (normal) should not be sent
	if got := receivedData.Get("priority"); got != "" {
		t.Errorf("priority = %q, want empty (PriorityNormal should not be sent)", got)
	}
}

func TestPriorityConstants(t *testing.T) {
	tests := []struct {
		name     string
		priority int
		want     int
	}{
		{"PriorityLowest", PriorityLowest, -2},
		{"PriorityLow", PriorityLow, -1},
		{"PriorityNormal", PriorityNormal, 0},
		{"PriorityHigh", PriorityHigh, 1},
		{"PriorityEmergency", PriorityEmergency, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.priority != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.priority, tt.want)
			}
		})
	}
}
