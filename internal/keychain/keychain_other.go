//go:build !darwin && !windows

package keychain

// Get returns ErrUnsupported on non-darwin/non-windows platforms.
func Get(account string) (string, error) {
	return "", ErrUnsupported
}

// Set returns ErrUnsupported on non-darwin/non-windows platforms.
func Set(account, password string) error {
	return ErrUnsupported
}

// Delete returns ErrUnsupported on non-darwin/non-windows platforms.
func Delete(account string) error {
	return ErrUnsupported
}
