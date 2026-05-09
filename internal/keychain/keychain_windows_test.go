//go:build windows

package keychain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

func uniqueAccount(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return "test-" + hex.EncodeToString(b)
}

func TestRoundTrip(t *testing.T) {
	acct := uniqueAccount(t)
	t.Cleanup(func() { _ = Delete(acct) })

	const pw = "hunter2-correct-horse-battery-staple"

	if err := Set(acct, pw); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := Get(acct)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != pw {
		t.Errorf("Get: %q, want %q", got, pw)
	}

	if err := Delete(acct); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := Get(acct); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: %v, want ErrNotFound", err)
	}
}

func TestSetReplacesExisting(t *testing.T) {
	acct := uniqueAccount(t)
	t.Cleanup(func() { _ = Delete(acct) })

	if err := Set(acct, "first"); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if err := Set(acct, "second"); err != nil {
		t.Fatalf("second Set: %v", err)
	}
	got, err := Get(acct)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "second" {
		t.Errorf("Get: %q, want %q", got, "second")
	}
}

func TestDeleteMissing(t *testing.T) {
	acct := uniqueAccount(t)
	if err := Delete(acct); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete missing: %v, want ErrNotFound", err)
	}
}
