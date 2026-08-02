//go:build darwin || linux

package commit

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func syncCreatedAncestors(anchor *anchoredParent) error {
	for index := len(anchor.directories) - 1; index > 0; index-- {
		if !anchor.directories[index].created {
			continue
		}
		if err := syncDirectory(anchor.directories[index-1].fd); err != nil {
			return fmt.Errorf("sync created ancestor %q: %w", anchor.directories[index].path, err)
		}
	}
	return nil
}

func hasCreatedAncestors(anchor *anchoredParent) bool {
	for _, directory := range anchor.directories {
		if directory.created {
			return true
		}
	}
	return false
}

func failFileBeforeVisibility(
	path string,
	failedPhase phase,
	cause error,
	anchor *anchoredParent,
	temporaryName string,
	temporaryIdentity EntryIdentity,
	faults faultPlan,
) error {
	var residue []string
	if anchor != nil {
		residue = append(residue, anchor.unpublishedResidue...)
		if temporaryName != "" {
			temporaryPath := filepath.Join(filepath.Dir(path), temporaryName)
			if cleanupErr := faults.failures[phaseCleanupTemporary]; cleanupErr != nil {
				residue = append(residue, temporaryPath)
			} else {
				observed, _, observeErr := anchor.observe(temporaryName, temporaryPath)
				switch {
				case errors.Is(observeErr, unix.ENOENT):
				case observeErr != nil || !temporaryIdentity.sameEntry(observed):
					residue = append(residue, temporaryPath)
				case unix.Unlinkat(anchor.parentFD(), temporaryName, 0) != nil:
					residue = append(residue, temporaryPath)
				case syncDirectory(anchor.parentFD()) != nil:
					residue = append(residue, temporaryPath)
				}
			}
		}
		residue = append(residue, cleanupCreatedAncestors(anchor, faults)...)
	}
	kind := failureUncommitted
	if isUnsupported(cause) {
		kind = failureUnsupportedGuarantee
	}
	return newFailure(kind, failedPhase, path, cause, residue...)
}

func cleanupCreatedAncestors(anchor *anchoredParent, faults faultPlan) []string {
	var residue []string
	for index := len(anchor.directories) - 1; index > 0; index-- {
		current := anchor.directories[index]
		if !current.created {
			continue
		}
		if faults.failures[phaseCleanupAncestors] != nil {
			residue = append(residue, current.path)
			continue
		}
		parent := anchor.directories[index-1]
		observed, _, err := observeAt(parent.fd, current.name, current.path)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil || !current.identity.sameObject(observed) {
			residue = append(residue, current.path)
			continue
		}
		if err := unix.Unlinkat(parent.fd, current.name, unix.AT_REMOVEDIR); err != nil {
			residue = append(residue, current.path)
			continue
		}
		if err := syncDirectory(parent.fd); err != nil {
			residue = append(residue, current.path)
		}
	}
	return residue
}
