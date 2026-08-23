//go:build windows

package rootedpath

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// CaptureDirectoryMountBoundary captures the admitted fixed NTFS volume from
// an already-open directory handle.
func CaptureDirectoryMountBoundary(handle uintptr) (DirectoryMountBoundary, error) {
	facts, err := queryWindowsDirectoryFacts(windows.Handle(handle))
	if err != nil {
		kind := FailureRootUnavailable
		if errors.Is(err, errMountIdentityUnsupported) {
			kind = FailureUnsupportedPlatform
		}
		return DirectoryMountBoundary{}, newFailure(kind, "", "capture directory volume identity", err)
	}
	if !facts.isDirectory {
		return DirectoryMountBoundary{}, newFailure(FailureRootUnavailable, "", "mount boundary requires a directory handle", nil)
	}
	mount := facts.mount
	boundary := DirectoryMountBoundary{mount: mount}
	if !boundary.valid() {
		return DirectoryMountBoundary{}, fmt.Errorf("captured directory mount boundary is uninitialized")
	}
	return boundary, nil
}

// ValidateDirectoryHandle rejects a directory on a different admitted volume.
func (boundary DirectoryMountBoundary) ValidateDirectoryHandle(handle uintptr) error {
	if !boundary.valid() {
		return newFailure(FailureRootUnavailable, "", "directory mount boundary is uninitialized", nil)
	}
	mount, err := nativeMountToken(windows.Handle(handle))
	if err != nil {
		kind := FailureRootUnavailable
		if errors.Is(err, errMountIdentityUnsupported) {
			kind = FailureUnsupportedPlatform
		}
		return newFailure(kind, "", "inspect descendant volume identity", err)
	}
	if mount != boundary.mount {
		return newFailure(FailureMountChanged, "", "directory crosses the captured cleanup volume", nil)
	}
	return nil
}

// ValidateEntryAt rejects an immediate entry on a different admitted volume.
func (boundary DirectoryMountBoundary) ValidateEntryAt(parentHandle uintptr, name string) error {
	if !boundary.valid() {
		return newFailure(FailureRootUnavailable, "", "directory mount boundary is uninitialized", nil)
	}
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name ||
		strings.ContainsRune(name, '\x00') {
		return newFailure(FailureInvalidDestination, name, "mount validation requires one immediate entry name", nil)
	}
	if err := validatePlatformComponent(name); err != nil {
		return err
	}
	mount, err := nativeMountTokenAt(windows.Handle(parentHandle), name)
	if err != nil {
		kind := FailureRootUnavailable
		if errors.Is(err, errMountIdentityUnsupported) {
			kind = FailureUnsupportedPlatform
		}
		return newFailure(kind, name, "inspect entry volume identity", err)
	}
	if mount != boundary.mount {
		return newFailure(FailureMountChanged, name, "entry crosses the captured cleanup volume", nil)
	}
	return nil
}
