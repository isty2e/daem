//go:build windows

package commit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/windows"
)

const windowsRootedAbsenceSyncAttempts = mutationfs.RootedAbsencePathObservationCount - 1

func ConfirmRootedEntryAbsentWithOutcome(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) (mutationfs.CommitOutcome, error) {
	path, pathErr := rootedCapabilityPath(capability)
	if capability != nil {
		defer capability.Close()
	}
	fail := func(err error) (mutationfs.CommitOutcome, error) { return outcomeFromError(err), err }
	if pathErr != nil {
		return fail(windowsFailureBeforeVisibility(phaseValidate, path, pathErr))
	}
	if ctx == nil {
		return fail(windowsFailureBeforeVisibility(phaseValidate, path, fmt.Errorf("rooted absence context is required")))
	}

	for attempt := range windowsRootedAbsenceSyncAttempts {
		observation, err := observeWindowsRootedAbsence(ctx, capability)
		if err != nil {
			return fail(windowsFailureBeforeVisibility(phaseVerifyEntry, path, windowsUnsupportedCause(err)))
		}
		if !observation.absent {
			closeErr := observation.close()
			if attempt == 0 {
				return fail(windowsFailureBeforeVisibility(
					phaseVerifyEntry,
					path,
					errors.Join(fmt.Errorf("rooted entry is present before absence confirmation"), closeErr),
				))
			}
			return fail(newFailure(
				failureRetainedResidue,
				phaseVerifyEntry,
				path,
				errors.Join(fmt.Errorf("rooted entry reappeared while confirming durable absence"), closeErr),
				path,
			))
		}
		if err := ctx.Err(); err != nil {
			closeErr := observation.close()
			return fail(windowsFailureBeforeVisibility(phaseVerifyEntry, path, errors.Join(err, closeErr)))
		}
		flushErr := flushWindowsHandle(observation.parent.Handle(), windowsFlushPolicy{directory: true})
		validationErr := capability.ValidateRetainedDirectoryHandle(observation.root.Fd())
		closeErr := observation.close()
		if flushErr != nil || validationErr != nil || closeErr != nil {
			return fail(newFailure(
				failureRetainedResidue,
				phaseSyncCleanupParent,
				path,
				windowsUnsupportedCause(errors.Join(flushErr, validationErr, closeErr)),
				path,
			))
		}
	}

	final, err := observeWindowsRootedAbsence(ctx, capability)
	if err != nil {
		return fail(windowsFailureBeforeVisibility(phaseVerifyEntry, path, windowsUnsupportedCause(err)))
	}
	if !final.absent {
		closeErr := final.close()
		return fail(newFailure(
			failureRetainedResidue,
			phaseVerifyEntry,
			path,
			errors.Join(fmt.Errorf("rooted entry reappeared after final durability confirmation"), closeErr),
			path,
		))
	}
	if err := final.close(); err != nil {
		return fail(windowsFailureBeforeVisibility(phaseClosePayload, path, err))
	}
	return outcomeFromError(nil), nil
}

type windowsRootedAbsenceObservation struct {
	root   *os.File
	opened []*windowsOwnedHandle
	parent windowsDirectoryHandle
	absent bool
}

func (observation *windowsRootedAbsenceObservation) close() error {
	if observation == nil {
		return nil
	}
	var failures []error
	for index := len(observation.opened) - 1; index >= 0; index-- {
		failures = append(failures, observation.opened[index].Close())
	}
	observation.opened = nil
	if observation.root != nil {
		failures = append(failures, observation.root.Close())
		observation.root = nil
	}
	return errors.Join(failures...)
}

func observeWindowsRootedAbsence(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) (*windowsRootedAbsenceObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	destination := capability.Destination()
	if err := destination.Validate(); err != nil {
		return nil, err
	}
	components := strings.Split(destination.Relative().Path(), "/")
	if err := capability.AdmitPhysicalWork(len(components), 0, 0); err != nil {
		return nil, err
	}
	root, err := capability.OpenRootDirectoryForMutation()
	if err != nil {
		return nil, err
	}
	observation := &windowsRootedAbsenceObservation{root: root}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = observation.close()
		}
	}()
	rootDirectory, err := captureWindowsDirectoryHandle(windows.Handle(root.Fd()))
	if err != nil {
		return nil, err
	}
	if err := capability.ValidateDirectoryHandle(root.Fd()); err != nil {
		return nil, err
	}
	observation.parent = rootDirectory

	for index, component := range components {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		access := uint32(windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE)
		if index < len(components)-1 {
			access |= windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_TRAVERSE
		}
		opened, openErr := openWindowsRelativeEntry(
			observation.parent,
			component,
			access,
			windowsParentShareMode,
			windows.FILE_OPEN,
			false,
		)
		if openErr != nil {
			if windowsNativeErrorClassOf(openErr) != windowsNativeErrorNotFound {
				return nil, openErr
			}
			observation.absent = true
			closeOnError = false
			return observation, nil
		}
		facts, factsErr := queryWindowsEntryFacts(opened.handle.Handle())
		if factsErr != nil {
			_ = opened.handle.Close()
			return nil, factsErr
		}
		if index == len(components)-1 {
			_ = opened.handle.Close()
			observation.absent = false
			closeOnError = false
			return observation, nil
		}
		if !facts.standard.directory || facts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			_ = opened.handle.Close()
			return nil, fmt.Errorf("Windows absence ancestor is not a non-reparse directory")
		}
		directory, directoryErr := opened.Directory()
		if directoryErr != nil {
			_ = opened.handle.Close()
			return nil, directoryErr
		}
		observation.opened = append(observation.opened, opened.handle)
		observation.parent = directory
	}
	return nil, fmt.Errorf("rooted absence destination has no components")
}
