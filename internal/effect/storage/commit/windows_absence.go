//go:build windows

package commit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/windows"
)

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
	missing, err := confirmWindowsMissingPass(ctx, capability)
	if err != nil {
		return fail(windowsFailureBeforeVisibility(phaseValidate, path, windowsUnsupportedCause(err)))
	}
	if !missing {
		return fail(windowsFailureBeforeVisibility(phaseVerifyEntry, path, fmt.Errorf("rooted entry is still present")))
	}
	missing, err = confirmWindowsMissingPass(context.WithoutCancel(ctx), capability)
	if err != nil {
		return fail(newFailure(failureIndeterminateCommit, phaseVerifyEntry, path, windowsUnsupportedCause(err)))
	}
	if !missing {
		return fail(newFailure(failureIndeterminateCommit, phaseVerifyEntry, path, fmt.Errorf("rooted entry appeared while confirming absence")))
	}
	return outcomeFromError(nil), nil
}

func confirmWindowsMissingPass(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	destination := capability.Destination()
	if err := destination.Validate(); err != nil {
		return false, err
	}
	components := strings.Split(destination.Relative().Path(), "/")
	if err := capability.AdmitPhysicalWork(len(components), 0, 0); err != nil {
		return false, err
	}
	root, err := capability.OpenRootDirectory()
	if err != nil {
		return false, err
	}
	var parent *windowsOwnedHandle
	closeAll := func() error {
		var failures []error
		if parent != nil {
			failures = append(failures, parent.Close())
		}
		failures = append(failures, root.Close())
		return errors.Join(failures...)
	}
	parentHandle := windows.Handle(root.Fd())
	for index, component := range components {
		if err := ctx.Err(); err != nil {
			return false, errors.Join(err, closeAll())
		}
		access := uint32(windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE)
		if index < len(components)-1 {
			access |= windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_TRAVERSE
		}
		opened, openErr := openWindowsRelativeEntry(
			parentHandle,
			component,
			access,
			windowsParentShareMode,
			windows.FILE_OPEN,
			false,
		)
		if openErr != nil {
			if windowsNativeErrorClassOf(openErr) != windowsNativeErrorNotFound {
				return false, errors.Join(openErr, closeAll())
			}
			flushErr := flushWindowsHandle(parentHandle, windowsFlushPolicy{directory: true})
			validationErr := capability.ValidateRetainedDirectoryHandle(root.Fd())
			return true, errors.Join(flushErr, validationErr, closeAll())
		}
		facts, factsErr := queryWindowsEntryFacts(opened.handle.Handle())
		if factsErr != nil {
			_ = opened.handle.Close()
			return false, errors.Join(factsErr, closeAll())
		}
		if index == len(components)-1 {
			_ = opened.handle.Close()
			return false, closeAll()
		}
		if !facts.standard.directory || facts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			_ = opened.handle.Close()
			return false, errors.Join(fmt.Errorf("Windows absence ancestor is not a non-reparse directory"), closeAll())
		}
		if parent != nil {
			if err := parent.Close(); err != nil {
				_ = opened.handle.Close()
				return false, errors.Join(err, closeAll())
			}
		}
		parent = opened.handle
		parentHandle = opened.handle.Handle()
	}
	return false, errors.Join(fmt.Errorf("rooted absence destination has no components"), closeAll())
}
