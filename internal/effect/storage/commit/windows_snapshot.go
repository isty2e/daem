//go:build windows

package commit

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/windows"
)

func snapshotWindowsDirectoryEntries(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	diagnosticPath string,
	maximumEntries int,
) (mutationfs.DirectorySnapshot, error) {
	if maximumEntries <= 0 {
		return mutationfs.DirectorySnapshot{}, fmt.Errorf("directory snapshot maximum entries must be positive")
	}
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, err
	}
	if diagnosticPath != "" {
		path = diagnosticPath
	}
	anchor, err := openWindowsDestinationAnchor(ctx, capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	root, err := openWindowsObservedEntry(ctx, anchor, true, false, false)
	if root != nil {
		defer root.close()
	}
	if err != nil {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	root.identity.path = path
	if root.identity.kind == entryKindSymlink {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, rootedFinalSymlinkFailure(path))
	}
	if root.identity.kind != entryKindDirectory {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseValidate, path, fmt.Errorf("entry is not a directory"))
	}
	if err := validateWindowsCanonicalEntryAttributes(root.facts.attribute.attributes, true); err != nil {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseCaptureMetadata, path, err)
	}

	first, err := enumerateWindowsDirectoryOnce(ctx, root.handle.Handle(), maximumEntries)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseReadPayload, path, err)
	}
	if err := capability.AdmitPhysicalWork(0, len(first), 0); err != nil {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseReadPayload, path, err)
	}
	entries := make([]mutationfs.DirectoryEntrySnapshot, 0, len(first))
	for _, entry := range first {
		if err := ctx.Err(); err != nil {
			return mutationfs.DirectorySnapshot{}, err
		}
		child, childErr := openWindowsRelativeEntry(
			root.handle.Handle(),
			entry.name,
			windows.FILE_READ_ATTRIBUTES|windows.FILE_READ_EA|windows.READ_CONTROL|windows.SYNCHRONIZE,
			windowsPublicationShareMode,
			windows.FILE_OPEN,
			false,
		)
		if childErr != nil {
			return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseRevalidateEntry, filepath.Join(path, entry.name), childErr)
		}
		facts, factsErr := queryWindowsEntryFacts(child.handle.Handle())
		metadata, metadataErr := queryWindowsMetadataFacts(child.handle.Handle())
		closeErr := child.handle.Close()
		if factsErr != nil || metadataErr != nil || closeErr != nil {
			return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(
				phaseCaptureMetadata,
				filepath.Join(path, entry.name),
				joinWindowsErrors(factsErr, metadataErr, closeErr),
			)
		}
		if !entry.identity.equal(facts.identity) || entry.attributes != facts.attribute.attributes ||
			entry.reparseTag != facts.attribute.reparseTag || entry.eaSize != metadata.ea.size {
			return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(
				phaseRevalidateEntry,
				filepath.Join(path, entry.name),
				fmt.Errorf("Windows directory entry changed during snapshot"),
			)
		}
		mode, modeErr := windowsCanonicalModeFromSecurity(metadata.security)
		if modeErr != nil {
			return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseCaptureMetadata, filepath.Join(path, entry.name), modeErr)
		}
		if err := validateWindowsObservedMetadata(metadata); err != nil {
			return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseCaptureMetadata, filepath.Join(path, entry.name), err)
		}
		kind := windowsEntryKindFromFacts(facts)
		if kind == entryKindRegular || kind == entryKindDirectory {
			if err := validateWindowsCanonicalEntryAttributes(facts.attribute.attributes, kind == entryKindDirectory); err != nil {
				return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseCaptureMetadata, filepath.Join(path, entry.name), err)
			}
		}
		size := facts.standard.endOfFile
		if kind != entryKindRegular || size < 0 {
			size = 0
		}
		identity := EntryIdentity{
			path:     filepath.Join(path, entry.name),
			kind:     kind,
			platform: platformIdentity{native: facts.identity},
		}
		snapshot, snapshotErr := mutationfs.NewDirectoryEntrySnapshot(entry.name, identity, mode, true, size)
		if snapshotErr != nil {
			return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseValidate, filepath.Join(path, entry.name), snapshotErr)
		}
		entries = append(entries, snapshot)
	}

	second, err := enumerateWindowsDirectoryOnce(ctx, root.handle.Handle(), maximumEntries)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if err := capability.AdmitPhysicalWork(0, len(second), 0); err != nil {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if !equalWindowsDirectoryEntries(first, second) {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(
			phaseRevalidateEntry,
			path,
			fmt.Errorf("Windows directory changed during snapshot"),
		)
	}
	if err := revalidateWindowsObservedEntry(ctx, anchor, root); err != nil {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	return mutationfs.NewDirectorySnapshot(root.identity, root.mode, true, entries)
}

func equalWindowsDirectoryEntries(left, right []windowsDirectoryEntry) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]windowsDirectoryEntry(nil), left...)
	right = append([]windowsDirectoryEntry(nil), right...)
	sort.Slice(left, func(i, j int) bool { return left[i].name < left[j].name })
	sort.Slice(right, func(i, j int) bool { return right[i].name < right[j].name })
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func joinWindowsErrors(values ...error) error {
	return errors.Join(values...)
}
