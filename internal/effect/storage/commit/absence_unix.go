//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

const rootedAbsenceSyncAttempts = mutationfs.RootedAbsencePathObservationCount - 1

// ConfirmRootedEntryAbsentWithOutcome durably confirms that one exact rooted
// destination is absent without creating missing ancestors. It syncs the
// nearest existing parent and re-observes the complete relative path after
// each sync while preserving the storage conclusion for the execute boundary.
func ConfirmRootedEntryAbsentWithOutcome(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) (mutationfs.CommitOutcome, error) {
	err := confirmRootedEntryAbsentWithFaults(ctx, capability, faultPlan{})
	return outcomeFromError(err), err
}

func confirmRootedEntryAbsentWithFaults(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	faults faultPlan,
) error {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		_ = closeRootedCapability(capability)
		return err
	}
	if ctx == nil {
		_ = closeRootedCapability(capability)
		return failureBeforeVisibility(
			phaseValidate,
			path,
			fmt.Errorf("rooted absence context is required"),
		)
	}
	defer closeRootedCapability(capability)

	for attempt := 0; attempt < rootedAbsenceSyncAttempts; attempt++ {
		observation, observeErr := observeRootedAbsence(ctx, capability)
		if observeErr != nil {
			return failureBeforeVisibility(phaseVerifyEntry, path, observeErr)
		}
		if !observation.absent {
			observation.close()
			return newFailure(
				failureRetainedResidue,
				phaseVerifyEntry,
				path,
				fmt.Errorf("rooted entry reappeared while confirming durable absence"),
				path,
			)
		}
		if err := ctx.Err(); err != nil {
			observation.close()
			return failureBeforeVisibility(phaseVerifyEntry, path, err)
		}
		if err := faults.run(ctx, phaseSyncCleanupParent, func() error {
			return syncDirectory(observation.parentFD)
		}); err != nil {
			observation.close()
			return newFailure(failureRetainedResidue, phaseSyncCleanupParent, path, err, path)
		}
		observation.close()
	}

	final, err := observeRootedAbsence(ctx, capability)
	if err != nil {
		return failureBeforeVisibility(phaseVerifyEntry, path, err)
	}
	if !final.absent {
		final.close()
		return newFailure(
			failureRetainedResidue,
			phaseVerifyEntry,
			path,
			fmt.Errorf("rooted entry reappeared after final durability confirmation"),
			path,
		)
	}
	final.close()
	return nil
}

type rootedAbsenceObservation struct {
	files    []*os.File
	parentFD int
	absent   bool
}

func (observation *rootedAbsenceObservation) close() {
	if observation == nil {
		return
	}
	for index := len(observation.files) - 1; index >= 0; index-- {
		if observation.files[index] != nil {
			_ = observation.files[index].Close()
		}
	}
	observation.files = nil
}

func observeRootedAbsence(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) (*rootedAbsenceObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rootFile, err := capability.OpenRootDirectory()
	if err != nil {
		return nil, err
	}
	observation := &rootedAbsenceObservation{
		files:    []*os.File{rootFile},
		parentFD: int(rootFile.Fd()),
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			observation.close()
		}
	}()

	if err := capability.ValidateDirectoryHandle(rootFile.Fd()); err != nil {
		return nil, err
	}
	relative := filepath.FromSlash(capability.Destination().Relative().Path())
	components := strings.Split(relative, string(filepath.Separator))
	currentPath := capability.Destination().Root().PhysicalRoot()
	for index, component := range components {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		currentFD := observation.parentFD
		var stat unix.Stat_t
		err := unix.Fstatat(currentFD, component, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			observation.absent = true
			closeOnError = false
			return observation, nil
		}
		if err != nil {
			return nil, err
		}
		entryPath := filepath.Join(currentPath, component)
		identity := identityFromStat(entryPath, &stat)
		if err := validateOwnedStat(entryPath, &stat); err != nil {
			return nil, err
		}
		if index == len(components)-1 {
			observation.absent = false
			closeOnError = false
			return observation, nil
		}
		if identity.kind == entryKindSymlink {
			return nil, rootedpath.NewBoundaryFailure(
				rootedpath.FailureAncestorSymlink,
				entryPath,
				"rooted absence path contains a symbolic-link ancestor",
				nil,
			)
		}
		if identity.kind != entryKindDirectory {
			return nil, rootedpath.NewBoundaryFailure(
				rootedpath.FailureAncestorNotDirectory,
				entryPath,
				"rooted absence path ancestor is not a directory",
				nil,
			)
		}
		fd, openErr := unix.Openat(
			currentFD,
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if openErr != nil {
			return nil, openErr
		}
		var opened unix.Stat_t
		if statErr := unix.Fstat(fd, &opened); statErr != nil {
			_ = unix.Close(fd)
			return nil, statErr
		}
		if !identity.sameObject(identityFromStat(entryPath, &opened)) {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("rooted absence ancestor changed while opening %q", entryPath)
		}
		if err := capability.ValidateDirectoryHandle(uintptr(fd)); err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
		file := os.NewFile(uintptr(fd), entryPath)
		if file == nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("wrap rooted absence ancestor %q", entryPath)
		}
		observation.files = append(observation.files, file)
		observation.parentFD = fd
		currentPath = entryPath
	}
	return nil, fmt.Errorf("rooted absence path has no final component")
}
