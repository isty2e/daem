//go:build darwin || linux

package rootedpath

import (
	"errors"
	"fmt"
)

// CaptureDirectoryMountBoundary captures the mount containing an already-open
// directory handle.
func CaptureDirectoryMountBoundary(
	handle uintptr,
) (DirectoryMountBoundary, error) {
	mount, err := nativeMountToken(int(handle))
	if err != nil {
		kind := FailureRootUnavailable
		if errors.Is(err, errMountIdentityUnsupported) {
			kind = FailureUnsupportedPlatform
		}
		return DirectoryMountBoundary{}, newFailure(
			kind,
			"",
			"capture directory mount identity",
			err,
		)
	}
	boundary := DirectoryMountBoundary{mount: mount}
	if !boundary.valid() {
		return DirectoryMountBoundary{}, fmt.Errorf(
			"captured directory mount boundary is uninitialized",
		)
	}
	return boundary, nil
}

// ValidateDirectoryHandle rejects a directory on a different mount.
func (boundary DirectoryMountBoundary) ValidateDirectoryHandle(
	handle uintptr,
) error {
	if !boundary.valid() {
		return newFailure(
			FailureRootUnavailable,
			"",
			"directory mount boundary is uninitialized",
			nil,
		)
	}
	mount, err := nativeMountToken(int(handle))
	if err != nil {
		kind := FailureRootUnavailable
		if errors.Is(err, errMountIdentityUnsupported) {
			kind = FailureUnsupportedPlatform
		}
		return newFailure(
			kind,
			"",
			"inspect descendant mount identity",
			err,
		)
	}
	if mount != boundary.mount {
		return newFailure(
			FailureMountChanged,
			"",
			"directory crosses the captured cleanup mount",
			nil,
		)
	}
	return nil
}
