//go:build !darwin

package updater

import (
	"context"
	"fmt"
)

// FetchZipFunc downloads the update ZIP. Defined here for non-darwin platforms
// so the type is available package-wide; the function is only called on darwin.
type FetchZipFunc func(ctx context.Context) ([]byte, error)

// applyDarwinBundleUpdate is a no-op stub on non-darwin platforms.
// It should never be called at runtime because DownloadAndApply gates
// the call behind runtime.GOOS == "darwin".
func applyDarwinBundleUpdate(_ context.Context, _ FetchZipFunc, _ string) error {
	return fmt.Errorf("applyDarwinBundleUpdate: not supported on this platform")
}
