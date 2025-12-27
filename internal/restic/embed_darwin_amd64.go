//go:build darwin && amd64

package restic

import _ "embed"

//go:embed restic_darwin_amd64
var resticBinary []byte
