//go:build windows

package commit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"golang.org/x/sys/windows"
)

type windowsObservedEntry struct {
	handle   *windowsOwnedHandle
	facts    windowsEntryFactsNative
	metadata windowsMetadataFacts
	identity EntryIdentity
	mode     fs.FileMode
}

func windowsEntryKindFromFacts(facts windowsEntryFactsNative) entryKind {
	if facts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		switch facts.attribute.reparseTag {
		case windows.IO_REPARSE_TAG_SYMLINK, windows.IO_REPARSE_TAG_MOUNT_POINT:
			return entryKindSymlink
		default:
			return entryKindSpecial
		}
	}
	if facts.standard.directory {
		return entryKindDirectory
	}
	return entryKindRegular
}

func openWindowsObservedEntry(
	ctx context.Context,
	anchor *windowsDestinationAnchor,
	withMetadata bool,
	readPayload bool,
	deleteAccess bool,
) (*windowsObservedEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	access := uint32(windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE)
	if withMetadata {
		access |= windows.FILE_GENERIC_READ | windows.FILE_READ_EA
	}
	if readPayload {
		access |= windows.FILE_GENERIC_READ
	}
	if deleteAccess {
		access |= windows.DELETE | windows.WRITE_DAC
	}
	opened, err := openWindowsRelativeEntry(
		anchor.parentHandle(),
		anchor.name.String(),
		access,
		windowsPublicationShareMode,
		windows.FILE_OPEN,
		false,
	)
	if err != nil {
		return nil, err
	}
	observed := &windowsObservedEntry{handle: opened.handle}
	fail := func(cause error) (*windowsObservedEntry, error) {
		return nil, joinWindowsClose(cause, observed.close())
	}
	observed.facts, err = queryWindowsEntryFacts(opened.handle.Handle())
	if err != nil {
		return fail(err)
	}
	kind := windowsEntryKindFromFacts(observed.facts)
	observed.identity = EntryIdentity{
		path:     anchor.path,
		kind:     kind,
		platform: platformIdentity{native: observed.facts.identity},
	}
	if withMetadata {
		observed.metadata, err = queryWindowsMetadataFacts(opened.handle.Handle())
		if err != nil {
			return fail(err)
		}
		observed.mode, err = windowsCanonicalModeFromSecurity(observed.metadata.security)
		if err != nil {
			return fail(err)
		}
		if err := validateWindowsObservedMetadata(observed.metadata); err != nil {
			return fail(err)
		}
		if kind == entryKindRegular || kind == entryKindDirectory {
			if err := validateWindowsCanonicalEntryAttributes(
				observed.facts.attribute.attributes,
				kind == entryKindDirectory,
			); err != nil {
				return fail(err)
			}
		}
	}
	if err := revalidateWindowsObservedEntry(ctx, anchor, observed); err != nil {
		return fail(err)
	}
	return observed, nil
}

func revalidateWindowsObservedEntry(
	ctx context.Context,
	anchor *windowsDestinationAnchor,
	expected *windowsObservedEntry,
) error {
	if expected == nil || expected.handle == nil {
		return fmt.Errorf("Windows expected entry observation is required")
	}
	if err := anchor.revalidate(ctx); err != nil {
		return err
	}
	current, err := openWindowsRelativeEntry(
		anchor.parentHandle(),
		anchor.name.String(),
		windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL|windows.SYNCHRONIZE,
		windowsPublicationShareMode,
		windows.FILE_OPEN,
		false,
	)
	if err != nil {
		return err
	}
	defer current.handle.Close()
	facts, err := queryWindowsEntryFacts(current.handle.Handle())
	if err != nil {
		return err
	}
	if !expected.facts.identity.equal(facts.identity) ||
		windowsEntryKindFromFacts(expected.facts) != windowsEntryKindFromFacts(facts) {
		return fmt.Errorf("Windows destination entry changed during observation")
	}
	return ctx.Err()
}

func (observed *windowsObservedEntry) close() error {
	if observed == nil || observed.handle == nil {
		return nil
	}
	err := observed.handle.Close()
	observed.handle = nil
	return err
}

func joinWindowsClose(primary error, closeErr error) error {
	if primary == nil {
		return closeErr
	}
	if closeErr == nil {
		return primary
	}
	return errors.Join(primary, fmt.Errorf("close Windows handle: %w", closeErr))
}
