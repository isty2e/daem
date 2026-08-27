//go:build !darwin && !linux && !windows

package rootedpath

import (
	"fmt"
	"os"
)

type capturedRootPlatform struct{}

func captureRootPlatform(
	selectedRoot string,
	_ rootSelectionMode,
	_ *physicalTraversal,
) (string, capturedRootPlatform, identityToken, mountIdentities, error) {
	return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
		FailureUnsupportedPlatform,
		selectedRoot,
		"native rooted-path authority is unavailable on this platform",
		nil,
	)
}

func resolveDirectoryPathPlatform(
	selectedPath string,
	_ bool,
	_ bool,
	_ *physicalTraversal,
) (string, capturedRootPlatform, identityToken, mountIdentities, []string, error) {
	return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, nil, newFailure(
		FailureUnsupportedPlatform,
		selectedPath,
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

func openCapturedCommitRootDirectory(_ *capturedRootPlatform) (*os.File, error) {
	return nil, fmt.Errorf("native rooted-path commit authority is unavailable on this platform")
}

func validateCapturedDirectoryHandle(_ *capturedRootPlatform, _ uintptr) error {
	return newFailure(FailureUnsupportedPlatform, "", "native rooted-path authority is unavailable on this platform", nil)
}

func capturedRootChildExistsNoFollow(_ *capturedRootPlatform, name string) (bool, error) {
	return false, newFailure(
		FailureUnsupportedPlatform,
		name,
		"native rooted-path child observation is unavailable on this platform",
		nil,
	)
}

func capturedRootValidationPathComponents(_ *capturedRootPlatform) (int, error) {
	return 0, newFailure(
		FailureUnsupportedPlatform,
		"",
		"native rooted-path child observation is unavailable on this platform",
		nil,
	)
}
