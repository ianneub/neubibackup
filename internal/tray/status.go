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

	// Get a consistent snapshot of backup state
	backup := st.GetBackupState()

	if backup.LastSuccess.IsZero() {
		if backup.LastError != "" {
			return "Last backup failed"
		}
		return "Not yet backed up"
	}

	age := time.Since(backup.LastSuccess)
	return fmt.Sprintf("Last backup: %s", util.FormatDuration(age))
}

// FormatStatusDetailed returns a detailed status with error info if present.
func FormatStatusDetailed(st *state.State, isRunning bool) string {
	if isRunning {
		return "Backup in progress..."
	}

	// Get a consistent snapshot of backup state
	backup := st.GetBackupState()

	if backup.LastError != "" && backup.ConsecutiveFailures > 0 {
		return fmt.Sprintf("Failed (%d attempts): %s", backup.ConsecutiveFailures, backup.LastError)
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
	return formatNextBackupAt(nextTime, time.Now())
}

func formatNextBackupAt(nextTime, now time.Time) string {
	if !nextTime.After(now) {
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

	// Far future: distinguish "tomorrow" from "later this week or beyond".
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	nextDay := time.Date(nextTime.Year(), nextTime.Month(), nextTime.Day(), 0, 0, 0, 0, nextTime.Location())
	dayDelta := int(nextDay.Sub(nowDay).Hours() / 24)

	if dayDelta == 1 {
		return fmt.Sprintf("tomorrow at %s", nextTime.Format("3:04 PM"))
	}

	return fmt.Sprintf("on %s at %s", nextTime.Format("Mon Jan 2"), nextTime.Format("3:04 PM"))
}
