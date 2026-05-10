// Package updater provides automatic update checking and installation.
package updater

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
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
	// Skip update check if current version is not a valid semver (e.g., "dev")
	if !isValidSemver(u.currentVersion) {
		slog.Info("Skipping update check: current version is not a valid semver", "version", u.currentVersion)
		return "", false, nil
	}

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

// isValidSemver checks if a version string is a valid semantic version.
func isValidSemver(version string) bool {
	_, err := semver.NewVersion(version)
	return err == nil
}

// readAssetBytes downloads a release asset via the given source and returns
// its bytes. Extracted from DownloadAndApply so the asset-download logic is
// testable without hitting the network.
func readAssetBytes(ctx context.Context, source selfupdate.Source, rel *selfupdate.Release) ([]byte, error) {
	rc, err := source.DownloadReleaseAsset(ctx, rel, rel.AssetID)
	if err != nil {
		return nil, fmt.Errorf("download release asset: %w", err)
	}
	defer rc.Close()
	return io.ReadAll(rc)
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

	slog.Info("Updating", "from", u.currentVersion, "to", latest.Version())

	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	if runtime.GOOS == "darwin" && !strings.HasSuffix(strings.ToLower(latest.AssetName), ".zip") {
		return fmt.Errorf("expected .zip release asset for darwin, got %q", latest.AssetName)
	}

	if runtime.GOOS == "darwin" {
		slog.Info("Updating macOS app bundle", "path", exe)
		fetch := func(ctx context.Context) ([]byte, error) {
			return readAssetBytes(ctx, source, latest)
		}
		if err := applyDarwinBundleUpdate(ctx, fetch, exe); err != nil {
			return fmt.Errorf("applying darwin update: %w", err)
		}
	} else {
		if err := updater.UpdateTo(ctx, latest, exe); err != nil {
			return fmt.Errorf("applying update: %w", err)
		}
	}

	slog.Info("Update completed successfully", "version", latest.Version())
	return nil
}

// CurrentVersion returns the current version.
func (u *Updater) CurrentVersion() string {
	return u.currentVersion
}
