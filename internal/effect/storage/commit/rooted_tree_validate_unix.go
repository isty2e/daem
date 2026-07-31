//go:build darwin || linux

package commit

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

// ValidateRootedDirectoryTree performs a bounded metadata-only validation of
// one selected private tree. It does not consume the capability.
func ValidateRootedDirectoryTree(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	limits mutationfs.TreeTraversalLimits,
) (EntryIdentity, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return EntryIdentity{}, err
	}
	budget, err := newTreeTraversalBudget(limits)
	if err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := ctx.Err(); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseValidate, path, err)
	}

	anchor, err := openCommitParent(path, capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	expected, stat, err := anchor.observe(anchor.base, path)
	if err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	if expected.kind != entryKindDirectory {
		return EntryIdentity{}, failureBeforeVisibility(
			phaseValidate,
			path,
			fmt.Errorf("entry is not a directory"),
		)
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	directoryFD, _, err := anchor.openExpected(anchor.base, path, expected)
	if err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	defer unix.Close(directoryFD)
	if err := capability.ValidateDirectoryHandle(uintptr(directoryFD)); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := validateDirectoryEntries(
		ctx,
		capability.ValidateDirectoryHandle,
		directoryFD,
		path,
		0,
		budget,
	); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseReadPayload, path, err)
	}
	if err := verifySnapshotEntry(
		anchor.parentFD(),
		anchor.base,
		directoryFD,
		path,
		expected,
	); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	return expected, nil
}

func validateDirectoryEntries(
	ctx context.Context,
	validateMount func(uintptr) error,
	directoryFD int,
	directoryPath string,
	depth int,
	budget *treeTraversalBudget,
) error {
	if validateMount == nil {
		return fmt.Errorf("directory mount validator is required")
	}
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	names, err := readDirectoryNames(
		ctx,
		directoryFD,
		directoryPath,
		budget.remainingEntries(),
	)
	if err != nil {
		return err
	}
	if err := budget.admitEntries(len(names)); err != nil {
		return err
	}

	expectedEntries := make(map[string]EntryIdentity, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		entryPath := filepath.Join(directoryPath, name)
		identity, stat, err := observeAnyAt(directoryFD, name, entryPath)
		if err != nil {
			return err
		}
		if err := validateOwnedStat(entryPath, &stat); err != nil {
			return err
		}
		expectedEntries[name] = identity

		switch identity.kind {
		case entryKindDirectory:
			entryFD, err := openExpectedAt(directoryFD, name, entryPath, identity)
			if err != nil {
				return err
			}
			if err := validateMount(uintptr(entryFD)); err != nil {
				_ = unix.Close(entryFD)
				return err
			}
			err = validateDirectoryEntries(
				ctx,
				validateMount,
				entryFD,
				entryPath,
				depth+1,
				budget,
			)
			if err == nil {
				err = verifySnapshotEntry(
					directoryFD,
					name,
					entryFD,
					entryPath,
					identity,
				)
			}
			closeErr := unix.Close(entryFD)
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		case entryKindRegular:
			entryFD, err := openExpectedAt(directoryFD, name, entryPath, identity)
			if err != nil {
				return err
			}
			if err := validateMount(uintptr(entryFD)); err != nil {
				_ = unix.Close(entryFD)
				return err
			}
			err = verifySnapshotEntry(
				directoryFD,
				name,
				entryFD,
				entryPath,
				identity,
			)
			closeErr := unix.Close(entryFD)
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		case entryKindSymlink:
			// A no-follow unlink removes the link itself and cannot cross into
			// its target. Its identity is revalidated below.
		default:
			return unsupported(
				fmt.Sprintf("rooted tree contains unsupported entry %q", entryPath),
				nil,
			)
		}
	}

	currentNames, err := readDirectoryNames(
		ctx,
		directoryFD,
		directoryPath,
		len(names),
	)
	if err != nil {
		return err
	}
	if !slices.Equal(names, currentNames) {
		return fmt.Errorf("directory entries changed while validating %q", directoryPath)
	}
	for _, name := range names {
		entryPath := filepath.Join(directoryPath, name)
		current, _, err := observeAnyAt(directoryFD, name, entryPath)
		if err != nil {
			return err
		}
		if !expectedEntries[name].sameEntry(current) {
			return fmt.Errorf(
				"directory entry identity changed while validating %q",
				entryPath,
			)
		}
	}
	return nil
}
