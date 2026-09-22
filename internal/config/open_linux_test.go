//go:build linux

package config

import (
	"errors"
	"slices"
	"testing"
)

func TestEditorCommand(t *testing.T) {
	tests := []struct {
		name       string
		hasOmarchy bool
		want       []string
	}{
		{"omarchy editor launcher present", true, []string{"omarchy-launch-editor", "/tmp/config.yaml"}},
		{"falls back to xdg-open", false, []string{"xdg-open", "/tmp/config.yaml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := lookPath
			t.Cleanup(func() { lookPath = orig })
			lookPath = func(file string) (string, error) {
				if tt.hasOmarchy && file == omarchyEditor {
					return "/usr/share/omarchy/bin/" + file, nil
				}
				return "", errors.New("not found")
			}

			if got := editorCommand("/tmp/config.yaml"); !slices.Equal(got, tt.want) {
				t.Errorf("editorCommand = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStartMissingBinaryReturnsError(t *testing.T) {
	if err := start([]string{"neubibackup-no-such-binary"}); err == nil {
		t.Fatal("start: expected error for missing binary, got nil")
	}
}
