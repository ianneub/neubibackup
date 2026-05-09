//go:build windows

package keychain

import (
	"errors"
	"syscall"

	"github.com/danieljoos/wincred"
)

// targetName builds the credential target name. Format:
//
//	"<service>:<account>"
//
// so multiple repositories get distinct entries.
func targetName(account string) string {
	return ServiceName + ":" + account
}

// Get retrieves the password for the given account. Returns ErrNotFound if
// no credential exists.
func Get(account string) (string, error) {
	cred, err := wincred.GetGenericCredential(targetName(account))
	if err != nil {
		// wincred returns syscall.Errno(0x80070490) (ERROR_NOT_FOUND)
		// when the credential is missing.
		var errno syscall.Errno
		if errors.As(err, &errno) && uint32(errno) == 0x80070490 {
			return "", ErrNotFound
		}
		return "", err
	}
	return string(cred.CredentialBlob), nil
}

// Set stores or replaces the password for the given account.
func Set(account, password string) error {
	cred := wincred.NewGenericCredential(targetName(account))
	cred.CredentialBlob = []byte(password)
	cred.UserName = account
	cred.Persist = wincred.PersistLocalMachine
	return cred.Write()
}

// Delete removes the credential for the given account. Returns ErrNotFound
// if it doesn't exist.
func Delete(account string) error {
	cred, err := wincred.GetGenericCredential(targetName(account))
	if err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) && uint32(errno) == 0x80070490 {
			return ErrNotFound
		}
		return err
	}
	return cred.Delete()
}
