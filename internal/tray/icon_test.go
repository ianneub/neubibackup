package tray

import (
	"testing"
)

func TestDetermineIconState(t *testing.T) {
	tests := []struct {
		name                string
		isRunning           bool
		isConfigured        bool
		consecutiveFailures int
		hasSuccessfulBackup bool
		want                IconState
	}{
		{
			name:         "running takes highest priority",
			isRunning:    true,
			isConfigured: true,
			want:         IconStateRunning,
		},
		{
			name:                "running even with failures",
			isRunning:           true,
			isConfigured:        true,
			consecutiveFailures: 5,
			want:                IconStateRunning,
		},
		{
			name:         "running even when not configured",
			isRunning:    true,
			isConfigured: false,
			want:         IconStateRunning,
		},
		{
			name:         "not configured shows error",
			isRunning:    false,
			isConfigured: false,
			want:         IconStateError,
		},
		{
			name:                "not configured takes priority over success",
			isRunning:           false,
			isConfigured:        false,
			hasSuccessfulBackup: true,
			want:                IconStateError,
		},
		{
			name:                "failures show error",
			isRunning:           false,
			isConfigured:        true,
			consecutiveFailures: 1,
			want:                IconStateError,
		},
		{
			name:                "failures take priority over success",
			isRunning:           false,
			isConfigured:        true,
			consecutiveFailures: 3,
			hasSuccessfulBackup: true,
			want:                IconStateError,
		},
		{
			name:                "success after no failures",
			isRunning:           false,
			isConfigured:        true,
			consecutiveFailures: 0,
			hasSuccessfulBackup: true,
			want:                IconStateSuccess,
		},
		{
			name:                "idle when configured but never backed up",
			isRunning:           false,
			isConfigured:        true,
			consecutiveFailures: 0,
			hasSuccessfulBackup: false,
			want:                IconStateIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineIconState(tt.isRunning, tt.isConfigured, tt.consecutiveFailures, tt.hasSuccessfulBackup)
			if got != tt.want {
				t.Errorf("DetermineIconState() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetIconBytes(t *testing.T) {
	tests := []struct {
		name  string
		state IconState
	}{
		{"idle", IconStateIdle},
		{"success", IconStateSuccess},
		{"error", IconStateError},
		{"running", IconStateRunning},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetIconBytes(tt.state)
			if got == nil {
				t.Errorf("GetIconBytes(%v) returned nil", tt.state)
			}
			if len(got) == 0 {
				t.Errorf("GetIconBytes(%v) returned empty slice", tt.state)
			}
		})
	}
}

func TestGetIconBytes_InvalidState(t *testing.T) {
	// Invalid state should default to idle icon
	got := GetIconBytes(IconState(999))
	if got == nil {
		t.Error("GetIconBytes(invalid) returned nil, expected idle icon")
	}
	// Should return the same as idle
	idle := GetIconBytes(IconStateIdle)
	if len(got) != len(idle) {
		t.Error("GetIconBytes(invalid) did not return idle icon")
	}
}
