//go:build windows

package commit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/windows"
)

func SnapshotRootedDirectory(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	limits mutationfs.TreeTraversalLimits,
	sink mutationfs.RootedTreeSnapshotSink,
) (EntryIdentity, error) {
	if sink == nil {
		return EntryIdentity{}, fmt.Errorf("rooted tree snapshot sink is required")
	}
	return snapshotWindowsRootedDirectory(ctx, capability, limits, sink)
}

func ValidateRootedDirectoryTree(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	limits mutationfs.TreeTraversalLimits,
) (EntryIdentity, error) {
	return snapshotWindowsRootedDirectory(ctx, capability, limits, nil)
}

func snapshotWindowsRootedDirectory(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	limits mutationfs.TreeTraversalLimits,
	sink mutationfs.RootedTreeSnapshotSink,
) (EntryIdentity, error) {
	if err := limits.Validate(); err != nil {
		return EntryIdentity{}, err
	}
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return EntryIdentity{}, err
	}
	anchor, err := openWindowsDestinationAnchor(ctx, capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, windowsUnsupportedCause(err))
	}
	root, err := openWindowsObservedEntry(ctx, anchor, true, false, false)
	if root != nil {
		defer root.close()
	}
	if err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, windowsUnsupportedCause(err))
	}
	if root.identity.kind == entryKindSymlink {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, rootedFinalSymlinkFailure(path))
	}
	if root.identity.kind != entryKindDirectory {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseValidate, path, fmt.Errorf("entry is not a directory"))
	}
	if err := validateWindowsCanonicalEntryAttributes(root.facts.attribute.attributes, true); err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureMetadata, path, err)
	}
	budget, err := newTreeTraversalBudget(limits)
	if err != nil {
		return EntryIdentity{}, err
	}
	if sink != nil {
		if err := sink.VisitRoot(root.mode); err != nil {
			return EntryIdentity{}, windowsFailureBeforeVisibility(phaseReadPayload, path, err)
		}
	}
	if err := snapshotWindowsDirectoryRecursive(ctx, capability, root.directory, path, nil, budget, sink); err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseReadPayload, path, err)
	}
	if err := revalidateWindowsObservedEntry(ctx, anchor, root); err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	return root.identity, nil
}

func snapshotWindowsDirectoryRecursive(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	directory windowsDirectoryHandle,
	directoryPath string,
	relative []string,
	budget *treeTraversalBudget,
	sink mutationfs.RootedTreeSnapshotSink,
) error {
	if err := budget.admitDepth(len(relative)); err != nil {
		return err
	}
	first, err := enumerateWindowsDirectoryOnce(ctx, directory.Handle(), budget.remainingEntries()+1)
	if err != nil {
		return err
	}
	sort.Slice(first, func(i, j int) bool { return first[i].name < first[j].name })
	if err := budget.admitEntries(len(first)); err != nil {
		return err
	}
	for _, entry := range first {
		if err := ctx.Err(); err != nil {
			return err
		}
		entryRelative := append(append([]string(nil), relative...), entry.name)
		relativePath, err := mutationfs.NewTreeRelativePath(entryRelative...)
		if err != nil {
			return err
		}
		entryPath := filepath.Join(directoryPath, entry.name)
		kind := entryKindRegular
		if entry.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return windowsNativeUnsupported(windowsNativePhaseMetadata, fmt.Sprintf("rooted tree contains reparse entry %q", entryPath), nil)
		}
		if entry.attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
			kind = entryKindDirectory
		}
		access := uint32(windows.FILE_GENERIC_READ | windows.FILE_READ_EA | windows.READ_CONTROL | windows.SYNCHRONIZE)
		if kind == entryKindDirectory {
			access |= windows.FILE_TRAVERSE
		}
		openKind := windowsRelativeFile
		if kind == entryKindDirectory {
			openKind = windowsRelativeDirectory
		}
		opened, err := openWindowsRelativeChild(
			directory,
			entry.name,
			access,
			windowsPublicationShareMode,
			windows.FILE_OPEN,
			openKind,
			false,
		)
		if err != nil {
			return err
		}
		facts, factsErr := queryWindowsEntryFacts(opened.handle.Handle())
		metadata, metadataErr := queryWindowsMetadataFacts(opened.handle.Handle())
		if factsErr != nil || metadataErr != nil {
			_ = opened.handle.Close()
			return errors.Join(factsErr, metadataErr)
		}
		if !entry.identity.equal(facts.identity) || windowsEntryKindFromFacts(facts) != kind {
			_ = opened.handle.Close()
			return fmt.Errorf("rooted tree entry changed while opening %q", entryPath)
		}
		if err := validateWindowsCanonicalEntryAttributes(facts.attribute.attributes, kind == entryKindDirectory); err != nil {
			_ = opened.handle.Close()
			return err
		}
		mode, err := windowsCanonicalModeFromSecurity(metadata.security)
		if err != nil {
			_ = opened.handle.Close()
			return err
		}
		if err := validateWindowsObservedMetadata(metadata); err != nil {
			_ = opened.handle.Close()
			return err
		}
		switch kind {
		case entryKindDirectory:
			if sink != nil {
				if err := sink.VisitDirectory(relativePath, mode); err != nil {
					_ = opened.handle.Close()
					return err
				}
			}
			err = snapshotWindowsDirectoryRecursive(
				ctx,
				capability,
				opened.directory,
				entryPath,
				entryRelative,
				budget,
				sink,
			)
		case entryKindRegular:
			size := facts.standard.endOfFile
			if size < 0 || size > budget.remainingBytes() {
				err = fmt.Errorf("rooted tree exceeds %d regular-file bytes", budget.limits.MaximumBytes())
				break
			}
			if err == nil {
				err = budget.admitBytes(size)
			}
			if err == nil && sink != nil {
				reader := &windowsRootedTreeFileReader{
					ctx:       ctx,
					handle:    opened.handle.Handle(),
					remaining: size,
				}
				err = sink.VisitRegularFile(relativePath, mode, size, reader)
				if err == nil && reader.count != size {
					err = fmt.Errorf(
						"rooted tree snapshot sink consumed %d of %d bytes for %q",
						reader.count,
						size,
						entryPath,
					)
				}
				if err == nil {
					var extra [1]byte
					count, readErr := reader.readRaw(extra[:])
					if count != 0 || readErr != io.EOF {
						if readErr == nil {
							readErr = fmt.Errorf("content exceeds observed size %d", size)
						}
						err = readErr
					}
				}
			}
		}
		closeErr := opened.handle.Close()
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
	}
	second, err := enumerateWindowsDirectoryOnce(ctx, directory.Handle(), len(first)+1)
	if err != nil {
		return err
	}
	if !equalWindowsDirectoryEntries(first, second) {
		return fmt.Errorf("rooted tree directory changed during snapshot at %q", directoryPath)
	}
	return ctx.Err()
}

// windowsRootedTreeFileReader streams one rooted regular file from the
// retained entry handle without materializing the payload. The walk enforces
// exact consumption: the sink must read exactly the observed size, and the
// trailing read must report EOF without content.
type windowsRootedTreeFileReader struct {
	ctx       context.Context
	handle    windows.Handle
	remaining int64
	count     int64
}

func (reader *windowsRootedTreeFileReader) Read(p []byte) (int, error) {
	count, err := reader.readRaw(p)
	reader.count += int64(count)
	return count, err
}

func (reader *windowsRootedTreeFileReader) readRaw(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if reader.remaining <= 0 {
		return 0, io.EOF
	}
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > reader.remaining {
		p = p[:reader.remaining]
	}
	var read uint32
	if err := windows.ReadFile(reader.handle, p, &read, nil); err != nil {
		return 0, normalizeWindowsNativeError(windowsNativePhaseRead, err, false)
	}
	if read == 0 {
		return 0, io.EOF
	}
	reader.remaining -= int64(read)
	return int(read), nil
}
