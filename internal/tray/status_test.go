package tray

import (
	"testing"
	"time"

	"neubibackup/internal/restic"
	"neubibackup/internal/state"
)

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		name      string
		state     *state.State
		isRunning bool
		want      string
	}{
		{
			name:      "backup running",
			state:     &state.State{},
			isRunning: true,
			want:      "Backup running...",
		},
		{
			name:      "never backed up",
			state:     &state.State{},
			isRunning: false,
			want:      "Not yet backed up",
		},
		{
			name: "never backed up with error",
			state: &state.State{
				LastBackupError: "connection failed",
			},
			isRunning: false,
			want:      "Last backup failed",
		},
		{
			name: "backed up recently",
			state: &state.State{
				LastBackupSuccess: time.Now().Add(-30 * time.Second),
			},
			isRunning: false,
			want:      "Last backup: just now",
		},
		{
			name: "backed up 5 minutes ago",
			state: &state.State{
				LastBackupSuccess: time.Now().Add(-5 * time.Minute),
			},
			isRunning: false,
			want:      "Last backup: 5 minutes ago",
		},
		{
			name: "backed up 2 hours ago",
			state: &state.State{
				LastBackupSuccess: time.Now().Add(-2 * time.Hour),
			},
			isRunning: false,
			want:      "Last backup: 2 hours ago",
		},
		{
			name: "backed up 3 days ago",
			state: &state.State{
				LastBackupSuccess: time.Now().Add(-3 * 24 * time.Hour),
			},
			isRunning: false,
			want:      "Last backup: 3 days ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatStatus(tt.state, tt.isRunning)
			if got != tt.want {
				t.Errorf("FormatStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatStatusDetailed(t *testing.T) {
	tests := []struct {
		name      string
		state     *state.State
		isRunning bool
		want      string
	}{
		{
			name:      "backup running",
			state:     &state.State{},
			isRunning: true,
			want:      "Backup in progress...",
		},
		{
			name: "failed with attempts",
			state: &state.State{
				LastBackupError:     "network timeout",
				ConsecutiveFailures: 3,
			},
			isRunning: false,
			want:      "Failed (3 attempts): network timeout",
		},
		{
			name: "error but zero failures (shouldn't happen, but handle it)",
			state: &state.State{
				LastBackupError:     "some error",
				ConsecutiveFailures: 0,
			},
			isRunning: false,
			want:      "Last backup failed",
		},
		{
			name:      "normal status - never backed up",
			state:     &state.State{},
			isRunning: false,
			want:      "Not yet backed up",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatStatusDetailed(tt.state, tt.isRunning)
			if got != tt.want {
				t.Errorf("FormatStatusDetailed() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"just now - seconds", 30 * time.Second, "just now"},
		{"just now - zero", 0, "just now"},
		{"1 minute", 1 * time.Minute, "1 minute ago"},
		{"1 minute 30 seconds", 90 * time.Second, "1 minute ago"},
		{"5 minutes", 5 * time.Minute, "5 minutes ago"},
		{"59 minutes", 59 * time.Minute, "59 minutes ago"},
		{"1 hour", 1 * time.Hour, "1 hour ago"},
		{"1 hour 30 minutes", 90 * time.Minute, "1 hour ago"},
		{"5 hours", 5 * time.Hour, "5 hours ago"},
		{"23 hours", 23 * time.Hour, "23 hours ago"},
		{"1 day", 24 * time.Hour, "1 day ago"},
		{"1 day 12 hours", 36 * time.Hour, "1 day ago"},
		{"3 days", 3 * 24 * time.Hour, "3 days ago"},
		{"7 days", 7 * 24 * time.Hour, "7 days ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.duration)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

func TestFormatProgress(t *testing.T) {
	tests := []struct {
		name     string
		progress *restic.BackupProgress
		want     string
	}{
		{
			name:     "nil progress",
			progress: nil,
			want:     "Backup running...",
		},
		{
			name:     "scanning - no progress yet",
			progress: &restic.BackupProgress{},
			want:     "Backup: Scanning files...",
		},
		{
			name: "percentage only",
			progress: &restic.BackupProgress{
				PercentDone: 0.45,
			},
			want: "Backup: 45%",
		},
		{
			name: "with bytes",
			progress: &restic.BackupProgress{
				PercentDone:    0.45,
				BytesProcessed: 2_400_000_000, // ~2.2 GB
				TotalBytes:     5_300_000_000, // ~4.9 GB
			},
			want: "Backup: 45% (2.2 GB / 4.9 GB)",
		},
		{
			name: "with files only",
			progress: &restic.BackupProgress{
				PercentDone:    0.30,
				FilesProcessed: 150,
				TotalFiles:     500,
			},
			want: "Backup: 30% (150 files)",
		},
		{
			name: "100 percent",
			progress: &restic.BackupProgress{
				PercentDone:    1.0,
				BytesProcessed: 1_000_000_000,
				TotalBytes:     1_000_000_000,
			},
			want: "Backup: 100% (953.7 MB / 953.7 MB)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatProgress(tt.progress)
			if got != tt.want {
				t.Errorf("FormatProgress() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{"zero bytes", 0, "0 B"},
		{"500 bytes", 500, "500 B"},
		{"1023 bytes", 1023, "1023 B"},
		{"1 KB", 1024, "1.0 KB"},
		{"1.5 KB", 1536, "1.5 KB"},
		{"1 MB", 1024 * 1024, "1.0 MB"},
		{"500 MB", 500 * 1024 * 1024, "500.0 MB"},
		{"1 GB", 1024 * 1024 * 1024, "1.0 GB"},
		{"2.5 GB", int64(2.5 * 1024 * 1024 * 1024), "2.5 GB"},
		{"1 TB", 1024 * 1024 * 1024 * 1024, "1.0 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBytes(tt.bytes)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestFormatNextBackup(t *testing.T) {
	tests := []struct {
		name     string
		offset   time.Duration
		want     string
	}{
		{
			name:   "past due",
			offset: -1 * time.Hour,
			want:   "Backup due",
		},
		{
			name:   "in 1 minute",
			offset: 30 * time.Second,
			want:   "in 1 minute",
		},
		{
			name:   "in 5 minutes",
			offset: 5*time.Minute + 30*time.Second, // add buffer for test execution time
			want:   "in 5 minutes",
		},
		{
			name:   "in 1 hour",
			offset: 1*time.Hour + 30*time.Second,
			want:   "in 1 hour",
		},
		{
			name:   "in 3 hours",
			offset: 3*time.Hour + 30*time.Second,
			want:   "in 3 hours",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate nextTime fresh for each test to avoid timing issues
			nextTime := time.Now().Add(tt.offset)
			got := FormatNextBackup(nextTime)
			if got != tt.want {
				t.Errorf("FormatNextBackup() = %q, want %q", got, tt.want)
			}
		})
	}
}
