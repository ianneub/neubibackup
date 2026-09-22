//go:build linux && amd64

package restic

import _ "embed"

//go:embed restic_linux_amd64
var resticBinary []byte
