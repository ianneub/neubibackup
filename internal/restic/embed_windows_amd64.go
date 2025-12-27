//go:build windows && amd64

package restic

import _ "embed"

//go:embed restic_windows_amd64.exe
var resticBinary []byte
