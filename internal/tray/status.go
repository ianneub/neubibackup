// Package tray provides system tray UI helpers.
package tray

import (
	"fmt"
	"time"

	"neubibackup/internal/restic"
	"neubibackup/internal/state"
)

// FormatStatus returns a human-readable status string.
func FormatStatus(st *state.State, isRunning bool) string {
	if isRunning {
		return "Backup running..."
	}

	if st.LastBackupSuccess.IsZero() {
		if st.LastBackupError != "" {
			return "Last backup failed"
		}
		return "Not yet backed up"
	}

	age := time.Since(st.LastBackupSuccess)
	return fmt.Sprintf("Last backup: %s", formatDuration(age))
}

// FormatStatusDetailed returns a detailed status with error info if present.
func FormatStatusDetailed(st *state.State, isRunning bool) string {
	if isRunning {
		return "Backup in progress..."
	}

	if st.LastBackupError != "" && st.ConsecutiveFailures > 0 {
		return fmt.Sprintf("Failed (%d attempts): %s", st.ConsecutiveFailures, st.LastBackupError)
	}

	return FormatStatus(st, isRunning)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}

	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

// FormatProgress returns a human-readable progress string for the status menu.
// Shows percentage and bytes processed (e.g., "Backup: 45% (2.3 GB / 5.1 GB)").
func FormatProgress(p *restic.BackupProgress) string {
	if p == nil {
		return "Backup running..."
	}

	pct := int(p.PercentDone * 100)

	// Full progress with bytes if available
	if p.TotalBytes > 0 && p.PercentDone > 0 {
		return fmt.Sprintf("Backup: %d%% (%s / %s)",
			pct,
			formatBytes(p.BytesProcessed),
			formatBytes(p.TotalBytes))
	}

	// File count progress if bytes not available
	if p.TotalFiles > 0 && p.FilesProcessed > 0 {
		return fmt.Sprintf("Backup: %d%% (%d files)",
			pct,
			p.FilesProcessed)
	}

	// Percentage only
	if p.PercentDone > 0 {
		return fmt.Sprintf("Backup: %d%%", pct)
	}

	// Scanning phase (no percentage yet)
	return "Backup: Scanning files..."
}

// formatBytes converts bytes to a human-readable string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// FormatNextBackup returns a human-readable string for the next backup time.
func FormatNextBackup(nextTime time.Time) string {
	now := time.Now()

	if nextTime.Before(now) {
		return "Backup due"
	}

	until := nextTime.Sub(now)

	if until < time.Hour {
		mins := int(until.Minutes())
		if mins <= 1 {
			return "in 1 minute"
		}
		return fmt.Sprintf("in %d minutes", mins)
	}

	if until < 24*time.Hour {
		hours := int(until.Hours())
		if hours == 1 {
			return "in 1 hour"
		}
		return fmt.Sprintf("in %d hours", hours)
	}

	return fmt.Sprintf("at %s", nextTime.Format("3:04 PM"))
}
