package updater

import (
	"testing"
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
