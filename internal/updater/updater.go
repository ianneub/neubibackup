// Package updater provides automatic update checking and installation.
package updater

import (
	"context"
	"fmt"
	"log"
	"runtime"

	"github.com/creativeprojects/go-selfupdate"
)

// Updater handles checking for and applying updates.
type Updater struct {
	currentVersion string
	repoOwner      string
	repoName       string
}

// New creates a new Updater.
func New(currentVersion, repoOwner, repoName string) *Updater {
	return &Updater{
		currentVersion: currentVersion,
		repoOwner:      repoOwner,
		repoName:       repoName,
	}
}

// CheckForUpdate queries GitHub and returns the new version if available.
// Returns empty string and false if no update is available.
func (u *Updater) CheckForUpdate(ctx context.Context) (newVersion string, available bool, err error) {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return "", false, fmt.Errorf("creating GitHub source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return "", false, fmt.Errorf("creating updater: %w", err)
	}

	latest, found, err := updater.DetectLatest(ctx, selfupdate.NewRepositorySlug(u.repoOwner, u.repoName))
	if err != nil {
		return "", false, fmt.Errorf("detecting latest version: %w", err)
	}

	if !found {
		return "", false, nil
	}

	// Compare versions
	if !latest.GreaterThan(u.currentVersion) {
		return "", false, nil
	}

	return latest.Version(), true, nil
}

// DownloadAndApply downloads the latest update and applies it.
// The application should restart after this completes successfully.
func (u *Updater) DownloadAndApply(ctx context.Context) error {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return fmt.Errorf("creating GitHub source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return fmt.Errorf("creating updater: %w", err)
	}

	latest, found, err := updater.DetectLatest(ctx, selfupdate.NewRepositorySlug(u.repoOwner, u.repoName))
	if err != nil {
		return fmt.Errorf("detecting latest version: %w", err)
	}

	if !found {
		return fmt.Errorf("no release found")
	}

	if !latest.GreaterThan(u.currentVersion) {
		return fmt.Errorf("already up to date")
	}

	log.Printf("Updating from %s to %s", u.currentVersion, latest.Version())

	// Get the executable path
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	// On macOS, we need to update the .app bundle, not just the binary
	// go-selfupdate handles this when the asset is a ZIP containing the .app
	if runtime.GOOS == "darwin" {
		log.Printf("Updating macOS app bundle at: %s", exe)
	}

	// Download and apply the update
	if err := updater.UpdateTo(ctx, latest, exe); err != nil {
		return fmt.Errorf("applying update: %w", err)
	}

	log.Printf("Update to %s completed successfully", latest.Version())
	return nil
}

// CurrentVersion returns the current version.
func (u *Updater) CurrentVersion() string {
	return u.currentVersion
}
