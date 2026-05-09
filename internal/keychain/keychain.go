// Package keychain provides cross-platform access to the OS-native credential
// vault (macOS Keychain on darwin, Windows Credential Manager on windows).
//
// Stored items use a fixed service name and an account string supplied by the
// caller (typically a restic repository path), so multiple repositories on the
// same machine get distinct entries.
//
// On platforms other than darwin and windows, all functions return
// ErrUnsupported.
package keychain

import "errors"

// ServiceName is the service identifier used for every item this package
// stores. Do not change without considering migration: existing entries on
// users' machines are keyed on this value.
const ServiceName = "com.neubibackup.repository"

// ErrNotFound is returned when no entry exists for the requested account.
var ErrNotFound = errors.New("keychain: entry not found")

// ErrUnsupported is returned on platforms where the native keychain backend
// is not implemented (anything other than darwin/windows).
var ErrUnsupported = errors.New("keychain: not supported on this platform")
