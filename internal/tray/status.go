// Package tray provides system tray UI helpers.
package tray

import (
	"fmt"
	"time"

	"neubibackup/internal/restic"
	"neubibackup/internal/state"
	"neubibackup/internal/util"
)

// FormatStatus returns a human-readable status string.
func FormatStatus(st *state.State, isRunning bool) string {
	if isRunning {
		return "Backup running..."
	}

	if st.Backup.LastSuccess.IsZero() {
		if st.Backup.LastError != "" {
			return "Last backup failed"
		}
		return "Not yet backed up"
	}

	age := time.Since(st.Backup.LastSuccess)
	return fmt.Sprintf("Last backup: %s", util.FormatDuration(age))
}

// FormatStatusDetailed returns a detailed status with error info if present.
func FormatStatusDetailed(st *state.State, isRunning bool) string {
	if isRunning {
		return "Backup in progress..."
	}

	if st.Backup.LastError != "" && st.Backup.ConsecutiveFailures > 0 {
		return fmt.Sprintf("Failed (%d attempts): %s", st.Backup.ConsecutiveFailures, st.Backup.LastError)
	}

	return FormatStatus(st, isRunning)
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
			util.FormatBytes(p.BytesProcessed),
			util.FormatBytes(p.TotalBytes))
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
