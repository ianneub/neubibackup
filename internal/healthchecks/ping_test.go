package healthchecks

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		pingURL  string
		wantURL  string
	}{
		{
			name:    "without trailing slash",
			pingURL: "https://hc-ping.com/uuid",
			wantURL: "https://hc-ping.com/uuid",
		},
		{
			name:    "with trailing slash",
			pingURL: "https://hc-ping.com/uuid/",
			wantURL: "https://hc-ping.com/uuid",
		},
		{
			name:    "empty URL",
			pingURL: "",
			wantURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := New(tt.pingURL)
			if client.PingURL != tt.wantURL {
				t.Errorf("New(%q).PingURL = %q, want %q", tt.pingURL, client.PingURL, tt.wantURL)
			}
			if client.HTTPClient == nil {
				t.Error("New() should set HTTPClient")
			}
		})
	}
}

func TestClient_Start(t *testing.T) {
	tests := []struct {
		name           string
		serverStatus   int
		wantErr        bool
		wantErrContain string
	}{
		{
			name:         "success",
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:           "server error",
			serverStatus:   http.StatusInternalServerError,
			wantErr:        true,
			wantErrContain: "status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedPath string
			var receivedMethod string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedPath = r.URL.Path
				receivedMethod = r.Method
				w.WriteHeader(tt.serverStatus)
			}))
			defer server.Close()

			client := New(server.URL)
			err := client.Start()

			if tt.wantErr {
				if err == nil {
					t.Error("Start() expected error, got nil")
				} else if tt.wantErrContain != "" && !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("Start() error = %q, want to contain %q", err.Error(), tt.wantErrContain)
				}
			} else {
				if err != nil {
					t.Errorf("Start() unexpected error: %v", err)
				}
			}

			if receivedPath != "/start" {
				t.Errorf("Start() sent request to %q, want %q", receivedPath, "/start")
			}
			if receivedMethod != http.MethodGet {
				t.Errorf("Start() used method %q, want %q", receivedMethod, http.MethodGet)
			}
		})
	}
}

func TestClient_Success(t *testing.T) {
	var receivedPath string
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	err := client.Success()

	if err != nil {
		t.Errorf("Success() unexpected error: %v", err)
	}

	if receivedPath != "/" {
		t.Errorf("Success() sent request to %q, want %q", receivedPath, "/")
	}
	if receivedMethod != http.MethodGet {
		t.Errorf("Success() used method %q, want %q", receivedMethod, http.MethodGet)
	}
}

func TestClient_Fail(t *testing.T) {
	tests := []struct {
		name       string
		logs       string
		wantMethod string
		wantBody   string
	}{
		{
			name:       "with logs",
			logs:       "error: backup failed\ndetails here",
			wantMethod: http.MethodPost,
			wantBody:   "error: backup failed\ndetails here",
		},
		{
			name:       "empty logs",
			logs:       "",
			wantMethod: http.MethodGet,
			wantBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedPath string
			var receivedMethod string
			var receivedBody string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedPath = r.URL.Path
				receivedMethod = r.Method
				if r.Body != nil {
					body, _ := io.ReadAll(r.Body)
					receivedBody = string(body)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client := New(server.URL)
			err := client.Fail(tt.logs)

			if err != nil {
				t.Errorf("Fail() unexpected error: %v", err)
			}

			if receivedPath != "/fail" {
				t.Errorf("Fail() sent request to %q, want %q", receivedPath, "/fail")
			}
			if receivedMethod != tt.wantMethod {
				t.Errorf("Fail() used method %q, want %q", receivedMethod, tt.wantMethod)
			}
			if receivedBody != tt.wantBody {
				t.Errorf("Fail() sent body %q, want %q", receivedBody, tt.wantBody)
			}
		})
	}
}

func TestClient_Log(t *testing.T) {
	var receivedPath string
	var receivedMethod string
	var receivedBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL)
	message := "backup progress: 50%"
	err := client.Log(message)

	if err != nil {
		t.Errorf("Log() unexpected error: %v", err)
	}

	if receivedPath != "/log" {
		t.Errorf("Log() sent request to %q, want %q", receivedPath, "/log")
	}
	if receivedMethod != http.MethodPost {
		t.Errorf("Log() used method %q, want %q", receivedMethod, http.MethodPost)
	}
	if receivedBody != message {
		t.Errorf("Log() sent body %q, want %q", receivedBody, message)
	}
}

func TestClient_EmptyPingURL(t *testing.T) {
	client := New("")

	tests := []struct {
		name string
		fn   func() error
	}{
		{"Start", client.Start},
		{"Success", client.Success},
		{"Fail", func() error { return client.Fail("logs") }},
		{"Log", func() error { return client.Log("message") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err != nil {
				t.Errorf("%s() with empty PingURL returned error: %v", tt.name, err)
			}
		})
	}
}

func TestClient_HTTPErrors(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		wantErr        bool
		wantErrContain string
	}{
		{
			name:       "200 OK",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "201 Created",
			statusCode: http.StatusCreated,
			wantErr:    false,
		},
		{
			name:           "400 Bad Request",
			statusCode:     http.StatusBadRequest,
			wantErr:        true,
			wantErrContain: "status 400",
		},
		{
			name:           "404 Not Found",
			statusCode:     http.StatusNotFound,
			wantErr:        true,
			wantErrContain: "status 404",
		},
		{
			name:           "500 Internal Server Error",
			statusCode:     http.StatusInternalServerError,
			wantErr:        true,
			wantErrContain: "status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := New(server.URL)
			err := client.Success()

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErrContain)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestClient_NetworkError(t *testing.T) {
	client := New("http://localhost:1") // Invalid port, connection refused
	err := client.Start()

	if err == nil {
		t.Error("expected error for network failure, got nil")
	}

	if !strings.Contains(err.Error(), "ping healthchecks") {
		t.Errorf("error = %q, want to contain 'ping healthchecks'", err.Error())
	}
}
