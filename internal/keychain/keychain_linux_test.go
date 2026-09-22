//go:build linux

package keychain

import (
	"errors"
	"testing"

	gokeyring "github.com/zalando/go-keyring"
)

// These tests run against go-keyring's in-memory mock so they never touch
// the real Secret Service (and work in CI, where there is no session bus).

func TestSetGetDeleteRoundTrip(t *testing.T) {
	gokeyring.MockInit()

	const account = "rest:https://backup.example.com/repo"
	if err := Set(account, "hunter2"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := Get(account)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("Get = %q, want hunter2", got)
	}

	if err := Delete(account); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := Get(account); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: got %v, want ErrNotFound", err)
	}
}

func TestSetReplacesExistingPassword(t *testing.T) {
	gokeyring.MockInit()

	const account = "/srv/restic-repo"
	if err := Set(account, "old"); err != nil {
		t.Fatalf("Set old: %v", err)
	}
	if err := Set(account, "new"); err != nil {
		t.Fatalf("Set new: %v", err)
	}
	if got, err := Get(account); err != nil || got != "new" {
		t.Errorf("Get = %q, %v; want new, nil", got, err)
	}
}

func TestAccountsAreIndependent(t *testing.T) {
	gokeyring.MockInit()

	if err := Set("repo-a", "pw-a"); err != nil {
		t.Fatalf("Set repo-a: %v", err)
	}
	if _, err := Get("repo-b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get repo-b: got %v, want ErrNotFound", err)
	}
}

func TestMissingEntryMapsToErrNotFound(t *testing.T) {
	gokeyring.MockInit()

	if _, err := Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: got %v, want ErrNotFound", err)
	}
	if err := Delete("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete: got %v, want ErrNotFound", err)
	}
}

func TestBackendErrorsPassThrough(t *testing.T) {
	backendErr := errors.New("org.freedesktop.DBus.Error.ServiceUnknown")
	gokeyring.MockInitWithError(backendErr)

	if _, err := Get("acct"); !errors.Is(err, backendErr) {
		t.Errorf("Get: got %v, want %v", err, backendErr)
	}
	if err := Set("acct", "pw"); !errors.Is(err, backendErr) {
		t.Errorf("Set: got %v, want %v", err, backendErr)
	}
	if err := Delete("acct"); !errors.Is(err, backendErr) {
		t.Errorf("Delete: got %v, want %v", err, backendErr)
	}
}
