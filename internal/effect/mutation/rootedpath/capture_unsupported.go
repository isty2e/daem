//go:build !darwin && !linux

package rootedpath

import (
	"fmt"
	"os"
)

type capturedRootPlatform struct{}

func captureRootPlatform(
	selectedRoot string,
	_ rootSelectionMode,
) (string, capturedRootPlatform, identityToken, identityToken, error) {
	return "", capturedRootPlatform{}, identityToken{}, identityToken{}, newFailure(
		FailureUnsupportedPlatform,
		selectedRoot,
		"native rooted-path authority is unavailable on this platform",
		nil,
	)
}

func validateCapturedRootPlatform(_ *capturedRootPlatform) error {
	return newFailure(FailureUnsupportedPlatform, "", "native rooted-path authority is unavailable on this platform", nil)
}

func cloneCapturedRootPlatform(_ *capturedRootPlatform) (capturedRootPlatform, error) {
	return capturedRootPlatform{}, fmt.Errorf("native rooted-path authority is unavailable on this platform")
}

func closeCapturedRootPlatform(_ *capturedRootPlatform) error {
	return nil
}

func openCapturedRootDirectory(_ *capturedRootPlatform) (*os.File, error) {
	return nil, fmt.Errorf("native rooted-path authority is unavailable on this platform")
}

func validateCapturedDirectoryHandle(_ *capturedRootPlatform, _ uintptr) error {
	return newFailure(FailureUnsupportedPlatform, "", "native rooted-path authority is unavailable on this platform", nil)
}
