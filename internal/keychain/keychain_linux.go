//go:build linux

package keychain

import (
	"errors"

	gokeyring "github.com/zalando/go-keyring"
)

// Linux stores entries through the freedesktop Secret Service API
// (GNOME Keyring, KWallet), with attributes service=ServiceName and
// username=account. They can be read from the shell with:
//
//	secret-tool lookup service com.neubibackup.repository username <account>
//
// Unlike macOS there is no per-application ACL: any process running as the
// user can read an unlocked keyring. The keyring must be unlocked (normally
// at login); if it isn't, the Secret Service shows its own unlock prompt.

// Get retrieves the password for the given account. Returns ErrNotFound if
// no entry exists for (ServiceName, account).
func Get(account string) (string, error) {
	pw, err := gokeyring.Get(ServiceName, account)
	if errors.Is(err, gokeyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return pw, err
}

// Set stores or replaces the password for the given account.
func Set(account, password string) error {
	return gokeyring.Set(ServiceName, account, password)
}

// Delete removes the entry for the given account. Returns ErrNotFound if no
// entry exists.
func Delete(account string) error {
	err := gokeyring.Delete(ServiceName, account)
	if errors.Is(err, gokeyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
