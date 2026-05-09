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
				Backup: state.BackupState{
					LastError: "connection failed",
				},
			},
			isRunning: false,
			want:      "Last backup failed",
		},
		{
			name: "backed up recently",
			state: &state.State{
				Backup: state.BackupState{
					LastSuccess: time.Now().Add(-30 * time.Second),
				},
			},
			isRunning: false,
			want:      "Last backup: just now",
		},
		{
			name: "backed up 5 minutes ago",
			state: &state.State{
				Backup: state.BackupState{
					LastSuccess: time.Now().Add(-5 * time.Minute),
				},
			},
			isRunning: false,
			want:      "Last backup: 5 minutes ago",
		},
		{
			name: "backed up 2 hours ago",
			state: &state.State{
				Backup: state.BackupState{
					LastSuccess: time.Now().Add(-2 * time.Hour),
				},
			},
			isRunning: false,
			want:      "Last backup: 2 hours ago",
		},
		{
			name: "backed up 3 days ago",
			state: &state.State{
				Backup: state.BackupState{
					LastSuccess: time.Now().Add(-3 * 24 * time.Hour),
				},
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
				Backup: state.BackupState{
					LastError:           "network timeout",
					ConsecutiveFailures: 3,
				},
			},
			isRunning: false,
			want:      "Failed (3 attempts): network timeout",
		},
		{
			name: "error but zero failures (shouldn't happen, but handle it)",
			state: &state.State{
				Backup: state.BackupState{
					LastError:           "some error",
					ConsecutiveFailures: 0,
				},
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

func TestFormatNextBackup(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) // Saturday noon

	tests := []struct {
		name string
		next time.Time
		want string
	}{
		{"past due", now.Add(-1 * time.Hour), "Backup due"},
		{"now (boundary)", now, "Backup due"},
		{"in 1 minute", now.Add(30 * time.Second), "in 1 minute"},
		{"in 5 minutes", now.Add(5 * time.Minute), "in 5 minutes"},
		{"in 1 hour", now.Add(time.Hour + time.Second), "in 1 hour"},
		{"in 3 hours", now.Add(3*time.Hour + time.Second), "in 3 hours"},
		{"tomorrow at 3 PM", time.Date(2026, 5, 10, 15, 4, 0, 0, time.UTC), "tomorrow at 3:04 PM"},
		{"3 days out", time.Date(2026, 5, 12, 15, 4, 0, 0, time.UTC), "on Tue May 12 at 3:04 PM"},
		{"a week out", time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC), "on Sat May 16 at 9:30 AM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatNextBackupAt(tt.next, now); got != tt.want {
				t.Errorf("formatNextBackupAt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatNextBackup_PublicWrapper(t *testing.T) {
	// Smoke check that the public wrapper works against time.Now() without panic.
	got := FormatNextBackup(time.Now().Add(-time.Hour))
	if got != "Backup due" {
		t.Errorf("FormatNextBackup(past) = %q, want %q", got, "Backup due")
	}
}
