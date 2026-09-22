//go:build !darwin && !windows && !(linux && amd64)

package restic

// Stub for unsupported platforms or when binaries aren't embedded.
// Run scripts/download-restic.sh to download binaries for embedding.
var resticBinary []byte
