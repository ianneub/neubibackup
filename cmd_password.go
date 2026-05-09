package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"neubibackup/internal/config"
	"neubibackup/internal/keychain"
)

// keychainBackend is the slice of the keychain package consumed by the
// password subcommands. Letting tests inject a fake keeps unit tests off
// the real Keychain.
type keychainBackend interface {
	Get(account string) (string, error)
	Set(account, password string) error
	Delete(account string) error
}

// realKeychain calls into the real keychain package.
type realKeychain struct{}

func (realKeychain) Get(a string) (string, error)  { return keychain.Get(a) }
func (realKeychain) Set(a, p string) error         { return keychain.Set(a, p) }
func (realKeychain) Delete(a string) error         { return keychain.Delete(a) }

// passwordReader is the function signature used by runSetPassword to
// obtain the password (separate from the keychain backend so tests don't
// have to drive a real terminal).
type passwordReader func(prompt string) (string, error)

// dispatchPasswordCmd handles top-level password-related subcommands.
// It is called by main() before the singleinstance lock or tray bootstrap.
// Returns (handled, exitCode). When handled=false the caller should
// continue with the normal flow.
func dispatchPasswordCmd(args []string) (bool, int) {
	if len(args) < 2 {
		return false, 0
	}
	switch args[1] {
	case "set-password":
		cfg, rc := loadCfgForSubcommand(os.Stderr)
		if rc != 0 {
			return true, rc
		}
		return true, runSetPassword(cfg, realKeychain{}, keychain.ReadPasswordStdin, os.Stderr)
	case "clear-password":
		cfg, rc := loadCfgForSubcommand(os.Stderr)
		if rc != 0 {
			return true, rc
		}
		return true, runClearPassword(cfg, realKeychain{}, os.Stderr)
	}
	return false, 0
}

func loadCfgForSubcommand(stderr io.Writer) (*config.Config, int) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "Error loading config: %v\n", err)
		return nil, 1
	}
	return cfg, 0
}

// runSetPassword prompts for a password and stores it in the keychain
// against the configured repository.path. Returns the process exit code.
func runSetPassword(cfg *config.Config, kc keychainBackend, read passwordReader, stderr io.Writer) int {
	if cfg.Repository.Path == "" {
		fmt.Fprintln(stderr, "Error: repository.path is not set in config.yaml")
		return 1
	}

	pw, err := read("Repository password: ")
	if err != nil {
		fmt.Fprintf(stderr, "Error reading password: %v\n", err)
		return 1
	}
	if pw == "" {
		fmt.Fprintln(stderr, "Error: empty password not stored")
		return 1
	}

	if err := kc.Set(cfg.Repository.Path, pw); err != nil {
		fmt.Fprintf(stderr, "Error writing to keychain: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "Password stored for %s\n", cfg.Repository.Path)
	return 0
}

// runClearPassword removes the keychain entry for the configured
// repository.path. Missing entries are treated as success.
func runClearPassword(cfg *config.Config, kc keychainBackend, stderr io.Writer) int {
	if cfg.Repository.Path == "" {
		fmt.Fprintln(stderr, "Error: repository.path is not set in config.yaml")
		return 1
	}

	err := kc.Delete(cfg.Repository.Path)
	switch {
	case err == nil:
		fmt.Fprintf(stderr, "Password cleared for %s\n", cfg.Repository.Path)
		return 0
	case errors.Is(err, keychain.ErrNotFound):
		fmt.Fprintf(stderr, "No password stored for %s\n", cfg.Repository.Path)
		return 0
	default:
		fmt.Fprintf(stderr, "Error clearing keychain: %v\n", err)
		return 1
	}
}
