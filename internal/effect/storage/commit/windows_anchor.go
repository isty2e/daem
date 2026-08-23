//go:build windows

package commit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/windows"
)

type windowsDestinationAnchor struct {
	capability  rootedpath.CommitCapability
	root        *os.File
	parent      *windowsOwnedHandle
	parentFacts windowsEntryFactsNative
	name        windowsComponent
	path        string
}

func acquireWindowsPathCapability(path string) (rootedpath.CommitCapability, error) {
	root, destination, err := rootedpath.CaptureDestination(path)
	if err != nil {
		return nil, err
	}
	capability, acquireErr := root.Acquire(destination)
	closeErr := root.Close()
	if acquireErr != nil {
		return nil, errors.Join(acquireErr, closeErr)
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, capability.Close())
	}
	return capability, nil
}

func openWindowsDestinationAnchor(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	mutating bool,
) (*windowsDestinationAnchor, error) {
	if ctx == nil {
		return nil, fmt.Errorf("Windows destination context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return nil, err
	}
	destination := capability.Destination()
	if err := destination.Validate(); err != nil {
		return nil, err
	}
	components := strings.Split(destination.Relative().Path(), "/")
	if len(components) == 0 {
		return nil, fmt.Errorf("Windows destination has no components")
	}
	name, err := parseWindowsComponent(components[len(components)-1])
	if err != nil {
		return nil, err
	}
	if err := capability.AdmitPhysicalWork(len(components), 0, 0); err != nil {
		return nil, err
	}
	root, err := capability.OpenRootDirectory()
	if err != nil {
		return nil, err
	}
	anchor := &windowsDestinationAnchor{
		capability: capability,
		root:       root,
		name:       name,
		path:       path,
	}
	fail := func(cause error) (*windowsDestinationAnchor, error) {
		return nil, errors.Join(cause, anchor.close())
	}
	if err := capability.ValidateDirectoryHandle(root.Fd()); err != nil {
		return fail(err)
	}
	parentHandle := windows.Handle(root.Fd())
	parentFacts, err := queryWindowsEntryFacts(parentHandle)
	if err != nil {
		return fail(err)
	}
	if !parentFacts.standard.directory || parentFacts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fail(fmt.Errorf("Windows destination root is not a non-reparse directory"))
	}
	for _, raw := range components[:len(components)-1] {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		access := uint32(windows.FILE_GENERIC_READ | windows.FILE_TRAVERSE | windows.READ_CONTROL)
		if mutating {
			access |= windows.FILE_GENERIC_WRITE | windows.WRITE_DAC | windows.DELETE
		}
		opened, openErr := openWindowsRelativeDirectory(
			parentHandle,
			raw,
			access,
			windowsParentShareMode,
			windows.FILE_OPEN,
			false,
		)
		if openErr != nil {
			return fail(openErr)
		}
		facts, factsErr := queryWindowsEntryFacts(opened.handle.Handle())
		if factsErr != nil || !facts.standard.directory || facts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			if factsErr == nil {
				factsErr = fmt.Errorf("Windows destination ancestor is not a non-reparse directory")
			}
			_ = opened.handle.Close()
			return fail(factsErr)
		}
		if anchor.parent != nil {
			if closeErr := anchor.parent.Close(); closeErr != nil {
				_ = opened.handle.Close()
				return fail(closeErr)
			}
		}
		anchor.parent = opened.handle
		parentHandle = opened.handle.Handle()
		parentFacts = facts
	}
	anchor.parentFacts = parentFacts
	if err := anchor.revalidate(ctx); err != nil {
		return fail(err)
	}
	return anchor, nil
}

func (anchor *windowsDestinationAnchor) parentHandle() windows.Handle {
	if anchor == nil {
		return windows.InvalidHandle
	}
	if anchor.parent != nil {
		return anchor.parent.Handle()
	}
	if anchor.root == nil {
		return windows.InvalidHandle
	}
	return windows.Handle(anchor.root.Fd())
}

func (anchor *windowsDestinationAnchor) revalidate(ctx context.Context) error {
	if anchor == nil || anchor.root == nil || anchor.capability == nil {
		return fmt.Errorf("Windows destination anchor is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := anchor.capability.ValidateRetainedDirectoryHandle(anchor.root.Fd()); err != nil {
		return err
	}
	current, err := queryWindowsEntryFacts(anchor.parentHandle())
	if err != nil {
		return err
	}
	if !anchor.parentFacts.identity.sameObject(current.identity) ||
		!current.standard.directory || current.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("Windows destination parent changed during the operation")
	}
	return ctx.Err()
}

func (anchor *windowsDestinationAnchor) close() error {
	if anchor == nil {
		return nil
	}
	var failures []error
	if anchor.parent != nil {
		failures = append(failures, anchor.parent.Close())
		anchor.parent = nil
	}
	if anchor.root != nil {
		failures = append(failures, anchor.root.Close())
		anchor.root = nil
	}
	return errors.Join(failures...)
}
