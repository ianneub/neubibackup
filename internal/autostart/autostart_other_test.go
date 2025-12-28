//go:build !windows

package autostart

import (
	"testing"
)

func TestNew(t *testing.T) {
	m, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if m == nil {
		t.Fatal("New() returned nil")
	}
	if m.app == nil {
		t.Error("app should not be nil")
	}
}

// Note: Full integration tests require modifying system autostart,
// which is not safe for automated testing. The go-autostart library
// has its own test coverage.
