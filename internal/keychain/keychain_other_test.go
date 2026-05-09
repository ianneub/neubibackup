//go:build !darwin && !windows

package keychain

import (
	"errors"
	"testing"
)

func TestStubReturnsUnsupported(t *testing.T) {
	if _, err := Get("acct"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Get: got %v, want ErrUnsupported", err)
	}
	if err := Set("acct", "pw"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Set: got %v, want ErrUnsupported", err)
	}
	if err := Delete("acct"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Delete: got %v, want ErrUnsupported", err)
	}
}
