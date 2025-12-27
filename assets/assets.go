// Package assets contains embedded icons for the system tray.
package assets

import _ "embed"

// Icon files for different application states.
// These are 22x22 PNG images optimized for macOS/Windows system trays.

//go:embed icon_idle.png
var IconIdle []byte

//go:embed icon_success.png
var IconSuccess []byte

//go:embed icon_error.png
var IconError []byte

//go:embed icon_running.png
var IconRunning []byte
