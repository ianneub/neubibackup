package updater

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/creativeprojects/go-selfupdate"
)

func TestNew(t *testing.T) {
	u := New("v1.0.0", "owner", "repo")

	if u.CurrentVersion() != "v1.0.0" {
		t.Errorf("expected version v1.0.0, got %s", u.CurrentVersion())
	}

	if u.repoOwner != "owner" {
		t.Errorf("expected owner 'owner', got %s", u.repoOwner)
	}

	if u.repoName != "repo" {
		t.Errorf("expected repo 'repo', got %s", u.repoName)
	}
}

// fakeSource is a test double for selfupdate.Source. Only DownloadReleaseAsset
// is exercised; the other interface methods panic to make accidental use loud.
type fakeSource struct {
	download func(ctx context.Context, rel *selfupdate.Release, assetID int64) (io.ReadCloser, error)
}

func (f *fakeSource) ListReleases(ctx context.Context, repo selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	panic("fakeSource.ListReleases not configured")
}

func (f *fakeSource) DownloadReleaseAsset(ctx context.Context, rel *selfupdate.Release, assetID int64) (io.ReadCloser, error) {
	return f.download(ctx, rel, assetID)
}

func TestReadAssetBytes_HappyPath(t *testing.T) {
	want := []byte("zip-bytes")
	src := &fakeSource{
		download: func(ctx context.Context, rel *selfupdate.Release, assetID int64) (io.ReadCloser, error) {
			if assetID != 42 {
				t.Errorf("assetID = %d, want 42", assetID)
			}
			return io.NopCloser(strings.NewReader(string(want))), nil
		},
	}
	rel := &selfupdate.Release{AssetID: 42}

	got, err := readAssetBytes(context.Background(), src, rel)
	if err != nil {
		t.Fatalf("readAssetBytes: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadAssetBytes_DownloadError(t *testing.T) {
	sentinel := errors.New("network down")
	src := &fakeSource{
		download: func(ctx context.Context, rel *selfupdate.Release, assetID int64) (io.ReadCloser, error) {
			return nil, sentinel
		},
	}
	rel := &selfupdate.Release{AssetID: 1}

	_, err := readAssetBytes(context.Background(), src, rel)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain does not wrap sentinel: %v", err)
	}
}

// errReader returns the supplied error from Read, used to exercise io.ReadAll
// failure paths.
type errReader struct{ err error }

func (e *errReader) Read(p []byte) (int, error) { return 0, e.err }

func TestReadAssetBytes_ReadError(t *testing.T) {
	sentinel := errors.New("disk full")
	src := &fakeSource{
		download: func(ctx context.Context, rel *selfupdate.Release, assetID int64) (io.ReadCloser, error) {
			return io.NopCloser(&errReader{err: sentinel}), nil
		},
	}
	rel := &selfupdate.Release{AssetID: 1}

	_, err := readAssetBytes(context.Background(), src, rel)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain does not wrap sentinel: %v", err)
	}
}
