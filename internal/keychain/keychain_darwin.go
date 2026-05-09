//go:build darwin

package keychain

import (
	"errors"

	gokeychain "github.com/keybase/go-keychain"
)

// Get retrieves the password for the given account. Returns ErrNotFound if
// no entry exists for (ServiceName, account).
func Get(account string) (string, error) {
	q := gokeychain.NewItem()
	q.SetSecClass(gokeychain.SecClassGenericPassword)
	q.SetService(ServiceName)
	q.SetAccount(account)
	q.SetMatchLimit(gokeychain.MatchLimitOne)
	q.SetReturnData(true)

	results, err := gokeychain.QueryItem(q)
	if err != nil {
		if errors.Is(err, gokeychain.ErrorItemNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	if len(results) == 0 {
		return "", ErrNotFound
	}
	return string(results[0].Data), nil
}

// Set stores or replaces the password for the given account. The Keychain
// ACL is created with the calling process's designated requirement; for
// stable cross-release ACLs, the binary must be signed with a stable code
// signing identity (see docs/release-signing.md).
func Set(account, password string) error {
	// Best-effort delete first so the new entry's ACL is owned by us, not
	// inherited from a prior process. ErrNotFound is fine.
	if err := Delete(account); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	item := gokeychain.NewItem()
	item.SetSecClass(gokeychain.SecClassGenericPassword)
	item.SetService(ServiceName)
	item.SetAccount(account)
	item.SetData([]byte(password))
	item.SetSynchronizable(gokeychain.SynchronizableNo)
	item.SetAccessible(gokeychain.AccessibleWhenUnlocked)

	if err := gokeychain.AddItem(item); err != nil {
		return err
	}
	return nil
}

// Delete removes the keychain entry for the given account. Returns
// ErrNotFound if no entry exists.
func Delete(account string) error {
	q := gokeychain.NewItem()
	q.SetSecClass(gokeychain.SecClassGenericPassword)
	q.SetService(ServiceName)
	q.SetAccount(account)
	if err := gokeychain.DeleteItem(q); err != nil {
		if errors.Is(err, gokeychain.ErrorItemNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}
