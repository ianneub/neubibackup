package main

import (
	"errors"
	"strings"
	"testing"

	"neubibackup/internal/config"
	"neubibackup/internal/keychain"
)

// fakeKeychain implements the keychainBackend interface used by the
// subcommand handlers.
type fakeKeychain struct {
	store  map[string]string
	getErr error
	setErr error
	delErr error
}

func newFakeKeychain() *fakeKeychain {
	return &fakeKeychain{store: map[string]string{}}
}

func (f *fakeKeychain) Get(account string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	v, ok := f.store[account]
	if !ok {
		return "", keychain.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeychain) Set(account, password string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.store[account] = password
	return nil
}

func (f *fakeKeychain) Delete(account string) error {
	if f.delErr != nil {
		return f.delErr
	}
	if _, ok := f.store[account]; !ok {
		return keychain.ErrNotFound
	}
	delete(f.store, account)
	return nil
}

func TestSetPasswordStoresInKeychain(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{Path: "/backup/repo"},
	}
	kc := newFakeKeychain()
	var stderr strings.Builder

	read := func(prompt string) (string, error) {
		if !strings.Contains(prompt, "password") {
			t.Errorf("prompt = %q, expected 'password' substring", prompt)
		}
		return "supersecret", nil
	}

	rc := runSetPassword(cfg, kc, read, &stderr)
	if rc != 0 {
		t.Fatalf("runSetPassword rc = %d, want 0; stderr=%q", rc, stderr.String())
	}
	if got := kc.store["/backup/repo"]; got != "supersecret" {
		t.Errorf("stored = %q, want supersecret", got)
	}
}

func TestSetPasswordEmptyRejected(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{Path: "/backup/repo"},
	}
	kc := newFakeKeychain()
	var stderr strings.Builder

	read := func(prompt string) (string, error) { return "", nil }

	rc := runSetPassword(cfg, kc, read, &stderr)
	if rc == 0 {
		t.Errorf("runSetPassword rc = 0, want non-zero on empty input")
	}
	if !strings.Contains(stderr.String(), "empty") {
		t.Errorf("stderr = %q, want mention of 'empty'", stderr.String())
	}
	if len(kc.store) != 0 {
		t.Errorf("store unexpectedly written: %v", kc.store)
	}
}

func TestSetPasswordRequiresRepoPath(t *testing.T) {
	cfg := &config.Config{}
	kc := newFakeKeychain()
	var stderr strings.Builder

	read := func(prompt string) (string, error) { return "ignored", nil }

	rc := runSetPassword(cfg, kc, read, &stderr)
	if rc == 0 {
		t.Error("runSetPassword rc = 0, want non-zero when repository.path is empty")
	}
	if !strings.Contains(stderr.String(), "repository.path") {
		t.Errorf("stderr = %q, want mention of 'repository.path'", stderr.String())
	}
}

func TestClearPasswordRemovesEntry(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{Path: "/backup/repo"},
	}
	kc := newFakeKeychain()
	kc.store["/backup/repo"] = "old"
	var stderr strings.Builder

	rc := runClearPassword(cfg, kc, &stderr)
	if rc != 0 {
		t.Errorf("runClearPassword rc = %d, want 0", rc)
	}
	if _, ok := kc.store["/backup/repo"]; ok {
		t.Error("entry not deleted")
	}
}

func TestClearPasswordIsIdempotent(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{Path: "/backup/repo"},
	}
	kc := newFakeKeychain()
	var stderr strings.Builder

	rc := runClearPassword(cfg, kc, &stderr)
	if rc != 0 {
		t.Errorf("runClearPassword rc = %d, want 0 even when entry missing", rc)
	}
}

func TestClearPasswordSurfacesUnexpectedError(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{Path: "/backup/repo"},
	}
	kc := newFakeKeychain()
	kc.delErr = errors.New("boom")
	var stderr strings.Builder

	rc := runClearPassword(cfg, kc, &stderr)
	if rc == 0 {
		t.Error("runClearPassword rc = 0, want non-zero on backend error")
	}
}
