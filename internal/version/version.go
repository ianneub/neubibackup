// Package version provides the application version information.
package version

// Version is set at build time via ldflags.
// Default "dev" triggers temp directory usage for data files.
var Version = "dev"

// IsDev returns true if this is a development build.
func IsDev() bool {
	return Version == "dev"
}
