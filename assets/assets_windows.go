//go:build windows

package assets

import _ "embed"

// Icon files for different application states.
// Windows requires ICO format for system tray icons.

//go:embed icon_idle.ico
var IconIdle []byte

//go:embed icon_success.ico
var IconSuccess []byte

//go:embed icon_error.ico
var IconError []byte

//go:embed icon_running.ico
var IconRunning []byte
