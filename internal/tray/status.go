// Package tray provides system tray UI helpers.
package tray

import (
	"fmt"
	"time"

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
