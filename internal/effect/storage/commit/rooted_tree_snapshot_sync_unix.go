//go:build darwin || linux

package commit

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"
)

func syncPreparedTreeSnapshotLocked(
	ctx context.Context,
	prepared *PreparedRootedTree,
	faults faultPlan,
) error {
	budget, err := newTreeTraversalBudget(prepared.limits)
	if err != nil {
		return err
	}
	if err := syncPreparedTreeSnapshotDirectory(
		ctx,
		prepared.stageFD,
		prepared.stagePath,
		prepared.snapshot.root,
		0,
		prepared.anchor.capability.ValidateDirectoryHandle,
		budget,
		faults,
	); err != nil {
		return err
	}
	if err := verifyPreparedTreeSnapshotLocked(ctx, prepared); err != nil {
		return atPhase(phaseValidate, err)
	}
	return nil
}

func syncPreparedTreeSnapshotDirectory(
	ctx context.Context,
	directoryFD int,
	directoryPath string,
	expected preparedTreeSnapshotEntry,
	depth int,
	validateMount func(uintptr) error,
	budget *treeTraversalBudget,
	faults faultPlan,
) error {
	if err := ctx.Err(); err != nil {
		return atPhase(phaseSyncTreeDirectory, err)
	}
	if err := budget.admitDepth(depth); err != nil {
		return atPhase(phaseValidate, err)
	}
	if err := verifyOpenedPreparedTreeEntry(ctx, directoryFD, directoryPath, expected, budget); err != nil {
		return atPhase(phaseValidate, err)
	}
	names, err := readDirectoryNames(ctx, directoryFD, directoryPath, len(expected.children))
	if err != nil {
		return atPhase(phaseValidate, err)
	}
	if err := budget.admitEntries(len(names)); err != nil {
		return atPhase(phaseValidate, err)
	}
	if !slices.Equal(names, preparedTreeSnapshotChildNames(expected)) {
		return atPhase(phaseValidate, fmt.Errorf("prepared tree directory entries changed at %q", directoryPath))
	}
	for index, child := range expected.children {
		if err := ctx.Err(); err != nil {
			return atPhase(phaseSyncTreeDirectory, err)
		}
		name := names[index]
		childPath := filepath.Join(directoryPath, name)
		identity, _, err := observeAnyAt(directoryFD, name, childPath)
		if err != nil {
			return atPhase(phaseValidate, err)
		}
		if !child.identity.sameEntry(identity) {
			return atPhase(phaseValidate, fmt.Errorf("prepared tree entry %q identity changed", childPath))
		}
		childFD, err := openExpectedAt(directoryFD, name, childPath, child.identity)
		if err != nil {
			return atPhase(phaseValidate, err)
		}
		if err := validateMount(uintptr(childFD)); err != nil {
			_ = unix.Close(childFD)
			return atPhase(phaseValidate, err)
		}
		switch child.expectation.kind {
		case entryKindDirectory:
			err = syncPreparedTreeSnapshotDirectory(
				ctx,
				childFD,
				childPath,
				child,
				depth+1,
				validateMount,
				budget,
				faults,
			)
		case entryKindRegular:
			if verifyErr := verifyOpenedPreparedTreeEntry(ctx, childFD, childPath, child, budget); verifyErr != nil {
				err = atPhase(phaseValidate, verifyErr)
			} else {
				err = atPhase(
					phaseSyncTreeFile,
					faults.run(ctx, phaseSyncTreeFile, func() error { return syncPayload(childFD) }),
				)
			}
		default:
			err = atPhase(phaseValidate, fmt.Errorf("prepared tree entry %q has unsupported kind", childPath))
		}
		closeErr := unix.Close(childFD)
		if err != nil {
			return err
		}
		if closeErr != nil {
			return atPhase(phaseSyncTreeDirectory, closeErr)
		}
	}
	if err := verifyPreparedTreeDirectoryBinding(ctx, directoryFD, directoryPath, expected); err != nil {
		return atPhase(phaseValidate, err)
	}
	return atPhase(
		phaseSyncTreeDirectory,
		faults.run(ctx, phaseSyncTreeDirectory, func() error { return syncDirectory(directoryFD) }),
	)
}
