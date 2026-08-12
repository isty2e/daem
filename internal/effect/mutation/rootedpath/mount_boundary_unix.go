//go:build darwin || linux

package rootedpath

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

// ValidateEntryAt rejects an immediate no-follow entry on a different mount.
func (boundary DirectoryMountBoundary) ValidateEntryAt(parentHandle uintptr, name string) error {
	if !boundary.valid() {
		return newFailure(
			FailureRootUnavailable,
			"",
			"directory mount boundary is uninitialized",
			nil,
		)
	}
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name ||
		strings.ContainsRune(name, '\x00') {
		return newFailure(
			FailureInvalidDestination,
			name,
			"mount validation requires one immediate entry name",
			nil,
		)
	}
	mount, err := nativeMountTokenAt(int(parentHandle), name)
	if err != nil {
		kind := FailureRootUnavailable
		if errors.Is(err, errMountIdentityUnsupported) {
			kind = FailureUnsupportedPlatform
		}
		return newFailure(
			kind,
			"",
			"inspect entry mount identity",
			err,
		)
	}
	if mount != boundary.mount {
		return newFailure(
			FailureMountChanged,
			"",
			"entry crosses the captured cleanup mount",
			nil,
		)
	}
	return nil
}
