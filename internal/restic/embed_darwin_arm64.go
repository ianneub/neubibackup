//go:build darwin && arm64

package restic

import _ "embed"

//go:embed restic_darwin_arm64
var resticBinary []byte
