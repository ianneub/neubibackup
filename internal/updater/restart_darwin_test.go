//go:build darwin

package updater

import (
	"testing"
)

func TestFindAppBundle(t *testing.T) {
	tests := []struct {
		name     string
		execPath string
		want     string
	}{
		{
			name:     "standard app bundle path",
			execPath: "/Applications/NeubiBackup.app/Contents/MacOS/neubibackup",
			want:     "/Applications/NeubiBackup.app",
		},
		{
			name:     "user applications folder",
			execPath: "/Users/test/Applications/MyApp.app/Contents/MacOS/myapp",
			want:     "/Users/test/Applications/MyApp.app",
		},
		{
			name:     "nested app bundle",
			execPath: "/some/deep/path/Test.app/Contents/MacOS/test",
			want:     "/some/deep/path/Test.app",
		},
		{
			name:     "not in app bundle - standalone binary",
			execPath: "/usr/local/bin/neubibackup",
			want:     "",
		},
		{
			name:     "not in app bundle - go run",
			execPath: "/var/folders/abc/T/go-build123/exe/main",
			want:     "",
		},
		{
			name:     "app bundle at root",
			execPath: "/MyApp.app/Contents/MacOS/myapp",
			want:     "/MyApp.app",
		},
		{
			name:     "path with .app in directory name but not bundle",
			execPath: "/Users/test/myapp.app.backup/bin/app",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findAppBundle(tt.execPath)
			if got != tt.want {
				t.Errorf("findAppBundle(%q) = %q, want %q", tt.execPath, got, tt.want)
			}
		})
	}
}
