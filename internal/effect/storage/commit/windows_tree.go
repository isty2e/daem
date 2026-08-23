//go:build windows

package commit

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/windows"
)

const (
	preparedTreePrivateDirectoryMode fs.FileMode = 0o700
	preparedTreePrivateFileMode      fs.FileMode = 0o600
)

type preparedRootedTreeState uint8

const (
	preparedRootedTreeInvalid preparedRootedTreeState = iota
	preparedRootedTreeReady
	preparedRootedTreeTerminal
)

type windowsPreparedTreeExpectation struct {
	path   string
	kind   entryKind
	mode   fs.FileMode
	size   int64
	digest [sha256.Size]byte
}

// PreparedRootedTree owns one private Windows stage and retained destination
// authority until Commit or Abort consumes both.
type PreparedRootedTree struct {
	mu          sync.Mutex
	state       preparedRootedTreeState
	destination string
	capability  rootedpath.CommitCapability
	anchor      *windowsDestinationAnchor
	stageName   string
	stagePath   string
	stage       *windowsRelativeOpen
	stageObject EntryIdentity
	expected    EntryIdentity
	limits      mutationfs.TreeTraversalLimits
	rootMode    fs.FileMode
	rootModeSet bool
	entries     map[string]windowsPreparedTreeExpectation
}

type rootedTreeWriterWindows struct {
	mu       sync.Mutex
	ctx      context.Context
	prepared *PreparedRootedTree
	budget   *treeTraversalBudget
	active   bool
}

func PrepareRootedTree(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	populate func(mutationfs.RootedTreeWriter) error,
) (*PreparedRootedTree, error) {
	return PrepareRootedTreeWithLimits(ctx, capability, defaultTreeTraversalLimits(), populate)
}

func PrepareRootedTreeWithLimits(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	limits mutationfs.TreeTraversalLimits,
	populate func(mutationfs.RootedTreeWriter) error,
) (*PreparedRootedTree, error) {
	if ctx == nil {
		if capability != nil {
			_ = capability.Close()
		}
		return nil, fmt.Errorf("rooted tree context is required")
	}
	if err := ctx.Err(); err != nil {
		if capability != nil {
			_ = capability.Close()
		}
		return nil, err
	}
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		if capability != nil {
			_ = capability.Close()
		}
		return nil, err
	}
	if populate == nil {
		_ = capability.Close()
		return nil, fmt.Errorf("rooted tree populate callback is required")
	}
	budget, err := newTreeTraversalBudget(limits)
	if err != nil {
		_ = capability.Close()
		return nil, err
	}
	anchor, err := openWindowsDestinationAnchor(ctx, capability, true)
	if err != nil {
		_ = capability.Close()
		return nil, windowsFailureBeforeVisibility(phaseCreateAncestors, path, windowsUnsupportedCause(err))
	}
	if err := requireWindowsDestinationAbsent(anchor); err != nil {
		_ = anchor.close()
		_ = capability.Close()
		return nil, windowsFailureBeforeVisibility(phaseValidate, path, err)
	}
	privateSecurity, err := buildWindowsCanonicalSecurity(preparedTreePrivateDirectoryMode)
	if err != nil {
		_ = anchor.close()
		_ = capability.Close()
		return nil, windowsFailureBeforeVisibility(phaseCreateTemporary, path, windowsUnsupportedCause(err))
	}
	stageName, err := unusedWindowsSiblingName(anchor.parentHandle(), temporaryPrefix)
	if err != nil {
		_ = anchor.close()
		_ = capability.Close()
		return nil, windowsFailureBeforeVisibility(phaseCreateTemporary, path, err)
	}
	stage, err := createWindowsRelativeDirectory(
		anchor.parentHandle(),
		stageName,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.FILE_TRAVERSE|windows.FILE_READ_EA|
			windows.READ_CONTROL|windows.WRITE_DAC|windows.DELETE,
		windowsParentShareMode,
		true,
		privateSecurity.descriptor,
	)
	prepared := &PreparedRootedTree{
		state:       preparedRootedTreeReady,
		destination: path,
		capability:  capability,
		anchor:      anchor,
		stageName:   stageName,
		stagePath:   filepath.Join(filepath.Dir(path), stageName),
		stage:       stage,
		limits:      limits,
		rootMode:    preparedTreePrivateDirectoryMode,
		entries:     make(map[string]windowsPreparedTreeExpectation),
	}
	if err != nil {
		observed, observeErr := observeWindowsEntryAt(anchor.parentHandle(), stageName)
		if observed.exists || observeErr != nil {
			prepared.state = preparedRootedTreeTerminal
			_ = prepared.releaseLocked()
			return nil, newFailure(
				failureRetainedResidue,
				phaseCreateTemporary,
				path,
				errors.Join(err, observeErr),
				prepared.stagePath,
			)
		}
		return nil, prepared.failBeforeVisibility(phaseCreateTemporary, err)
	}
	facts, err := queryWindowsEntryFacts(stage.handle.Handle())
	if err != nil {
		return nil, prepared.failBeforeVisibility(phaseCreateTemporary, err)
	}
	prepared.stageObject = EntryIdentity{
		path:     prepared.stagePath,
		kind:     entryKindDirectory,
		platform: platformIdentity{native: facts.identity},
	}

	writer := &rootedTreeWriterWindows{ctx: ctx, prepared: prepared, budget: budget, active: true}
	returned := false
	defer func() {
		writer.deactivate()
		if !returned {
			_ = prepared.Abort(context.Background())
		}
	}()
	if err := populate(writer); err != nil {
		return nil, prepared.failBeforeVisibility(phaseWritePayload, err)
	}
	writer.deactivate()
	if err := ctx.Err(); err != nil {
		return nil, prepared.failBeforeVisibility(phaseWritePayload, err)
	}
	prepared.mu.Lock()
	err = prepared.finalizeLocked(ctx)
	prepared.mu.Unlock()
	if err != nil {
		return nil, prepared.failBeforeVisibility(phaseValidate, err)
	}
	returned = true
	return prepared, nil
}

func (writer *rootedTreeWriterWindows) deactivate() {
	writer.mu.Lock()
	writer.active = false
	writer.mu.Unlock()
}

func (writer *rootedTreeWriterWindows) SetRootMode(mode fs.FileMode) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if err := writer.validateMode(mode, true); err != nil {
		return err
	}
	if writer.prepared.rootModeSet {
		return fmt.Errorf("rooted tree root mode is already set")
	}
	writer.prepared.rootMode = mode.Perm()
	writer.prepared.rootModeSet = true
	return nil
}

func (writer *rootedTreeWriterWindows) CreateDirectory(path mutationfs.TreeRelativePath, mode fs.FileMode) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if err := writer.validatePath(path, mode, true); err != nil {
		return err
	}
	if err := writer.budget.admitWrittenEntry(path.Depth()); err != nil {
		return err
	}
	parent, name, closeParent, err := writer.openParent(path)
	if err != nil {
		return err
	}
	defer closeParent()
	security, err := buildWindowsCanonicalSecurity(preparedTreePrivateDirectoryMode)
	if err != nil {
		return err
	}
	opened, err := createWindowsRelativeDirectory(
		parent,
		name,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.FILE_TRAVERSE|windows.FILE_READ_EA|
			windows.READ_CONTROL|windows.WRITE_DAC|windows.DELETE,
		windowsParentShareMode,
		true,
		security.descriptor,
	)
	if err != nil {
		return err
	}
	facts, factsErr := queryWindowsEntryFacts(opened.handle.Handle())
	metadata, metadataErr := queryWindowsMetadataFacts(opened.handle.Handle())
	closeErr := opened.handle.Close()
	if factsErr != nil || metadataErr != nil || closeErr != nil {
		return errors.Join(factsErr, metadataErr, closeErr)
	}
	if !facts.standard.directory {
		return fmt.Errorf("prepared Windows directory is not a directory")
	}
	if err := validateWindowsCanonicalEntryAttributes(facts.attribute.attributes, true); err != nil {
		return err
	}
	if err := ensureWindowsCanonicalMetadataSupported(metadata, security.facts); err != nil {
		return err
	}
	return writer.record(windowsPreparedTreeExpectation{path: path.Path(), kind: entryKindDirectory, mode: mode.Perm()})
}

func (writer *rootedTreeWriterWindows) WriteFile(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	content io.Reader,
) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if content == nil {
		return fmt.Errorf("prepared tree file content is required")
	}
	if err := writer.validatePath(path, mode, false); err != nil {
		return err
	}
	if err := writer.budget.admitWrittenEntry(path.Depth() - 1); err != nil {
		return err
	}
	parent, name, closeParent, err := writer.openParent(path)
	if err != nil {
		return err
	}
	defer closeParent()
	security, err := buildWindowsCanonicalSecurity(preparedTreePrivateFileMode)
	if err != nil {
		return err
	}
	opened, err := createWindowsRelativeFile(
		parent,
		name,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.FILE_GENERIC_EXECUTE|windows.FILE_READ_EA|
			windows.READ_CONTROL|windows.WRITE_DAC|windows.DELETE,
		windowsParentShareMode,
		true,
		security.descriptor,
	)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = opened.handle.Close()
		}
	}()
	digest := sha256.New()
	written, err := copyWindowsPreparedFile(writer.ctx, opened.handle.Handle(), io.TeeReader(content, digest), writer.budget.remainingBytes())
	if err != nil {
		return err
	}
	if err := writer.budget.admitBytes(written); err != nil {
		return err
	}
	if err := flushWindowsHandle(opened.handle.Handle(), windowsFlushPolicy{}); err != nil {
		return err
	}
	facts, factsErr := queryWindowsEntryFacts(opened.handle.Handle())
	metadata, metadataErr := queryWindowsMetadataFacts(opened.handle.Handle())
	closeErr := opened.handle.Close()
	closed = true
	if factsErr != nil || metadataErr != nil || closeErr != nil {
		return errors.Join(factsErr, metadataErr, closeErr)
	}
	if facts.standard.directory || facts.standard.endOfFile != written {
		return fmt.Errorf("prepared Windows file size changed during staging")
	}
	if err := validateWindowsCanonicalEntryAttributes(facts.attribute.attributes, false); err != nil {
		return err
	}
	if err := ensureWindowsCanonicalMetadataSupported(metadata, security.facts); err != nil {
		return err
	}
	var contentDigest [sha256.Size]byte
	copy(contentDigest[:], digest.Sum(nil))
	return writer.record(windowsPreparedTreeExpectation{
		path: path.Path(), kind: entryKindRegular, mode: mode.Perm(), size: written, digest: contentDigest,
	})
}

func (writer *rootedTreeWriterWindows) validateMode(mode fs.FileMode, directory bool) error {
	if !writer.active || writer.prepared == nil || writer.prepared.state != preparedRootedTreeReady {
		return fmt.Errorf("rooted tree writer is no longer active")
	}
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	if err := validateFileMode(mode); err != nil {
		return err
	}
	if directory {
		return validateWindowsCanonicalDirectoryMode(mode)
	}
	return validateWindowsCanonicalFileMode(mode)
}

func (writer *rootedTreeWriterWindows) validatePath(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	directory bool,
) error {
	if err := writer.validateMode(mode, directory); err != nil {
		return err
	}
	if err := path.Validate(); err != nil {
		return err
	}
	if _, exists := writer.prepared.entries[path.Path()]; exists {
		return fmt.Errorf("prepared tree entry %q was planned more than once", path.Path())
	}
	return nil
}

func (writer *rootedTreeWriterWindows) record(expectation windowsPreparedTreeExpectation) error {
	writer.prepared.entries[expectation.path] = expectation
	return nil
}

func (writer *rootedTreeWriterWindows) openParent(
	path mutationfs.TreeRelativePath,
) (windows.Handle, string, func() error, error) {
	components := strings.Split(path.Path(), "/")
	parent := writer.prepared.stage.handle.Handle()
	opened := make([]*windowsOwnedHandle, 0, len(components)-1)
	closeOpened := func() error { return closeWindowsOwnedHandles(opened...) }
	for _, component := range components[:len(components)-1] {
		child, err := openWindowsRelativeDirectory(
			parent,
			component,
			windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.FILE_TRAVERSE|windows.FILE_READ_EA|
				windows.READ_CONTROL|windows.WRITE_DAC|windows.DELETE,
			windowsPublicationShareMode,
			windows.FILE_OPEN,
			false,
		)
		if err != nil {
			return windows.InvalidHandle, "", closeOpened, errors.Join(err, closeOpened())
		}
		facts, err := queryWindowsEntryFacts(child.handle.Handle())
		if err != nil || !facts.standard.directory || facts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			_ = child.handle.Close()
			return windows.InvalidHandle, "", closeOpened, errors.Join(err, closeOpened())
		}
		opened = append(opened, child.handle)
		parent = child.handle.Handle()
	}
	return parent, components[len(components)-1], closeOpened, nil
}

func copyWindowsPreparedFile(
	ctx context.Context,
	handle windows.Handle,
	reader io.Reader,
	maximumBytes int64,
) (int64, error) {
	if maximumBytes < 0 {
		return 0, fmt.Errorf("prepared file byte budget is invalid")
	}
	buffer := make([]byte, windowsPayloadChunkSize)
	var written int64
	zeroReads := 0
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		limit := len(buffer)
		remaining := maximumBytes - written
		if remaining < int64(limit) {
			limit = int(remaining) + 1
		}
		count, readErr := reader.Read(buffer[:limit])
		if count > 0 {
			zeroReads = 0
			if int64(count) > remaining {
				return written, fmt.Errorf("prepared tree exceeds %d regular-file bytes", maximumBytes)
			}
			if err := writeWindowsPayload(ctx, handle, buffer[:count]); err != nil {
				return written, err
			}
			written += int64(count)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
		if count == 0 {
			zeroReads++
			if zeroReads >= 100 {
				return written, io.ErrNoProgress
			}
		}
	}
}

func (prepared *PreparedRootedTree) finalizeLocked(ctx context.Context) error {
	if prepared.state != preparedRootedTreeReady || prepared.stage == nil {
		return fmt.Errorf("prepared rooted tree is not ready")
	}
	paths := make([]string, 0, len(prepared.entries))
	for path := range prepared.entries {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		leftDepth := strings.Count(paths[i], "/")
		rightDepth := strings.Count(paths[j], "/")
		if leftDepth != rightDepth {
			return leftDepth > rightDepth
		}
		return paths[i] > paths[j]
	})
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		expectation := prepared.entries[path]
		handle, closeHandle, err := prepared.openEntry(path, expectation.kind, true)
		if err != nil {
			return err
		}
		_, applyErr := applyWindowsCanonicalSecurity(handle, expectation.mode)
		flushErr := flushWindowsHandle(handle, windowsFlushPolicy{directory: expectation.kind == entryKindDirectory})
		closeErr := closeHandle()
		if applyErr != nil || flushErr != nil || closeErr != nil {
			return errors.Join(applyErr, flushErr, closeErr)
		}
	}
	if _, err := applyWindowsCanonicalSecurity(prepared.stage.handle.Handle(), prepared.rootMode); err != nil {
		return err
	}
	if err := flushWindowsHandle(prepared.stage.handle.Handle(), windowsFlushPolicy{directory: true}); err != nil {
		return err
	}
	rootIdentity, err := prepared.verifyLocked(ctx)
	if err != nil {
		return err
	}
	prepared.expected = rootIdentity
	return nil
}

func (prepared *PreparedRootedTree) openEntry(
	path string,
	kind entryKind,
	mutating bool,
) (windows.Handle, func() error, error) {
	components := strings.Split(path, "/")
	parent := prepared.stage.handle.Handle()
	opened := make([]*windowsOwnedHandle, 0, len(components))
	closeOpened := func() error { return closeWindowsOwnedHandles(opened...) }
	for index, component := range components {
		entryKindHint := windowsRelativeDirectory
		if index == len(components)-1 && kind == entryKindRegular {
			entryKindHint = windowsRelativeFile
		}
		access := uint32(windows.FILE_GENERIC_READ | windows.FILE_READ_EA | windows.READ_CONTROL | windows.SYNCHRONIZE)
		if entryKindHint == windowsRelativeDirectory {
			access |= windows.FILE_TRAVERSE
		}
		if mutating {
			access |= windows.FILE_GENERIC_WRITE | windows.WRITE_DAC | windows.DELETE
		}
		entry, err := openWindowsRelativeChild(
			parent,
			component,
			access,
			windowsPublicationShareMode,
			windows.FILE_OPEN,
			entryKindHint,
			false,
		)
		if err != nil {
			return windows.InvalidHandle, closeOpened, errors.Join(err, closeOpened())
		}
		opened = append(opened, entry.handle)
		parent = entry.handle.Handle()
	}
	return parent, closeOpened, nil
}

func (prepared *PreparedRootedTree) verifyLocked(ctx context.Context) (EntryIdentity, error) {
	facts, err := queryWindowsEntryFacts(prepared.stage.handle.Handle())
	if err != nil {
		return EntryIdentity{}, err
	}
	if !prepared.stageObject.platform.native.sameObject(facts.identity) || !facts.standard.directory {
		return EntryIdentity{}, fmt.Errorf("prepared Windows tree root changed")
	}
	if err := prepared.anchor.revalidate(ctx); err != nil {
		return EntryIdentity{}, err
	}
	if err := requireWindowsNameMatches(
		prepared.anchor.parentHandle(),
		prepared.stageName,
		facts.identity,
		false,
	); err != nil {
		return EntryIdentity{}, err
	}
	metadata, err := queryWindowsMetadataFacts(prepared.stage.handle.Handle())
	if err != nil {
		return EntryIdentity{}, err
	}
	if err := validateWindowsObservedMetadata(metadata); err != nil {
		return EntryIdentity{}, err
	}
	if err := validateWindowsCanonicalEntryAttributes(facts.attribute.attributes, true); err != nil {
		return EntryIdentity{}, err
	}
	mode, err := windowsCanonicalModeFromSecurity(metadata.security)
	if err != nil || mode != prepared.rootMode {
		return EntryIdentity{}, errors.Join(err, fmt.Errorf("prepared Windows tree root mode is not canonical"))
	}
	visited := make(map[string]struct{}, len(prepared.entries))
	budget, err := newTreeTraversalBudget(prepared.limits)
	if err != nil {
		return EntryIdentity{}, err
	}
	if err := prepared.verifyDirectory(ctx, prepared.stage.handle.Handle(), "", visited, budget); err != nil {
		return EntryIdentity{}, err
	}
	if len(visited) != len(prepared.entries) {
		return EntryIdentity{}, fmt.Errorf("prepared Windows tree shape differs from its plan")
	}
	return EntryIdentity{
		path:     prepared.stagePath,
		kind:     entryKindDirectory,
		platform: platformIdentity{native: facts.identity},
	}, nil
}

func (prepared *PreparedRootedTree) verifyDirectory(
	ctx context.Context,
	directory windows.Handle,
	prefix string,
	visited map[string]struct{},
	budget *treeTraversalBudget,
) error {
	depth := 0
	if prefix != "" {
		depth = strings.Count(prefix, "/") + 1
	}
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	entries, err := enumerateWindowsDirectoryOnce(ctx, directory, budget.remainingEntries()+1)
	if err != nil {
		return err
	}
	if err := budget.admitEntries(len(entries)); err != nil {
		return err
	}
	second, err := enumerateWindowsDirectoryOnce(ctx, directory, len(entries)+1)
	if err != nil || !equalWindowsDirectoryEntries(entries, second) {
		return errors.Join(err, fmt.Errorf("prepared Windows tree directory changed during verification"))
	}
	for _, entry := range entries {
		path := entry.name
		if prefix != "" {
			path = prefix + "/" + entry.name
		}
		expectation, ok := prepared.entries[path]
		if !ok {
			return fmt.Errorf("prepared Windows tree contains unplanned entry %q", path)
		}
		if _, duplicate := visited[path]; duplicate {
			return fmt.Errorf("prepared Windows tree visited %q twice", path)
		}
		visited[path] = struct{}{}
		handle, closeHandle, err := prepared.openEntry(path, expectation.kind, false)
		if err != nil {
			return err
		}
		facts, factsErr := queryWindowsEntryFacts(handle)
		metadata, metadataErr := queryWindowsMetadataFacts(handle)
		mode, modeErr := windowsCanonicalModeFromSecurity(metadata.security)
		observedMetadataErr := validateWindowsObservedMetadata(metadata)
		attributesErr := validateWindowsCanonicalEntryAttributes(
			facts.attribute.attributes,
			expectation.kind == entryKindDirectory,
		)
		if factsErr != nil || metadataErr != nil || modeErr != nil || observedMetadataErr != nil || attributesErr != nil {
			_ = closeHandle()
			return errors.Join(factsErr, metadataErr, modeErr, observedMetadataErr, attributesErr)
		}
		if mode != expectation.mode || windowsEntryKindFromFacts(facts) != expectation.kind ||
			!entry.identity.equal(facts.identity) {
			_ = closeHandle()
			return fmt.Errorf("prepared Windows tree entry %q changed", path)
		}
		if expectation.kind == entryKindRegular {
			if err := budget.admitBytes(facts.standard.endOfFile); err != nil {
				_ = closeHandle()
				return err
			}
			content, err := readWindowsPayloadUpTo(ctx, handle, expectation.size+1)
			if err != nil || int64(len(content)) != expectation.size || sha256.Sum256(content) != expectation.digest {
				_ = closeHandle()
				return errors.Join(err, fmt.Errorf("prepared Windows tree file %q content changed", path))
			}
		}
		closeErr := closeHandle()
		if closeErr != nil {
			return closeErr
		}
		if expectation.kind == entryKindDirectory {
			child, closeChild, err := prepared.openEntry(path, entryKindDirectory, false)
			if err != nil {
				return err
			}
			err = prepared.verifyDirectory(ctx, child, path, visited, budget)
			closeErr := closeChild()
			if err != nil || closeErr != nil {
				return errors.Join(err, closeErr)
			}
		}
	}
	final, err := enumerateWindowsDirectoryOnce(ctx, directory, len(entries)+1)
	if err != nil || !equalWindowsDirectoryEntries(entries, final) {
		return errors.Join(err, fmt.Errorf("prepared Windows tree directory changed during traversal"))
	}
	return nil
}

func (prepared *PreparedRootedTree) Commit(ctx context.Context) error {
	_, err := prepared.CommitWithOutcome(ctx)
	return err
}

func (prepared *PreparedRootedTree) CommitWithOutcome(ctx context.Context) (mutationfs.CommitOutcome, error) {
	outcome, _, err := prepared.CommitWithPublishedIdentity(ctx)
	return outcome, err
}

func (prepared *PreparedRootedTree) CommitWithPublishedIdentity(
	ctx context.Context,
) (mutationfs.CommitOutcome, EntryIdentity, error) {
	if prepared == nil {
		err := windowsFailureBeforeVisibility(phaseValidate, "", fmt.Errorf("prepared rooted tree is required"))
		return outcomeFromError(err), EntryIdentity{}, err
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.state != preparedRootedTreeReady || prepared.stage == nil {
		err := windowsFailureBeforeVisibility(phaseValidate, prepared.destination, fmt.Errorf("prepared rooted tree is not ready"))
		return outcomeFromError(err), EntryIdentity{}, err
	}
	if ctx == nil {
		err := prepared.failBeforeVisibilityLocked(phaseValidate, fmt.Errorf("prepared rooted tree context is required"))
		return outcomeFromError(err), EntryIdentity{}, err
	}
	if err := ctx.Err(); err != nil {
		err = prepared.failBeforeVisibilityLocked(phaseValidate, err)
		return outcomeFromError(err), EntryIdentity{}, err
	}
	current, err := prepared.verifyLocked(ctx)
	if err != nil || !prepared.expected.sameEntry(current) {
		err = prepared.failBeforeVisibilityLocked(
			phaseRevalidateEntry,
			errors.Join(err, fmt.Errorf("prepared tree changed before publication")),
		)
		return outcomeFromError(err), EntryIdentity{}, err
	}
	if err := requireWindowsDestinationAbsent(prepared.anchor); err != nil {
		err = prepared.failBeforeVisibilityLocked(phaseRevalidateEntry, err)
		return outcomeFromError(err), EntryIdentity{}, err
	}
	if _, err := renameWindowsByHandle(
		prepared.stage.handle.Handle(),
		prepared.anchor.parentHandle(),
		prepared.anchor.name.String(),
		windowsRenameNoReplace,
	); err != nil {
		moved, unchanged, observeErr := observeWindowsNamespaceTransition(
			prepared.anchor.parentHandle(), prepared.stageName, prepared.anchor.name.String(), current.platform.native,
		)
		if unchanged && observeErr == nil {
			err = prepared.failBeforeVisibilityLocked(phaseCommitEntry, err)
			return outcomeFromError(err), EntryIdentity{}, err
		}
		identity := EntryIdentity{}
		residue := []string(nil)
		if moved {
			published, publishedErr := observeWindowsEntryAt(
				prepared.anchor.parentHandle(),
				prepared.anchor.name.String(),
			)
			observeErr = errors.Join(observeErr, publishedErr)
			if publishedErr == nil && published.exists && current.platform.native.sameObject(published.identity) {
				identity = EntryIdentity{
					path:     prepared.destination,
					kind:     entryKindDirectory,
					platform: platformIdentity{native: published.identity},
				}
			}
			residue = []string{prepared.destination}
		}
		err = prepared.finishVisibleLocked(phaseCommitEntry, errors.Join(err, observeErr), residue...)
		return outcomeFromError(err), identity, err
	}
	published, observeErr := observeWindowsEntryAt(prepared.anchor.parentHandle(), prepared.anchor.name.String())
	if observeErr != nil || !published.exists || !current.platform.native.sameObject(published.identity) {
		err = prepared.finishVisibleLocked(phaseVerifyEntry, observeErr, prepared.destination)
		return outcomeFromError(err), EntryIdentity{}, err
	}
	identity := EntryIdentity{
		path:     prepared.destination,
		kind:     entryKindDirectory,
		platform: platformIdentity{native: published.identity},
	}
	if err := flushWindowsHandle(prepared.anchor.parentHandle(), windowsFlushPolicy{directory: true}); err != nil {
		err = prepared.finishVisibleLocked(phaseSyncParent, err, prepared.destination)
		return outcomeFromError(err), identity, err
	}
	closeErr := prepared.releaseLocked()
	prepared.state = preparedRootedTreeTerminal
	if closeErr != nil {
		err = newFailure(failureIndeterminateCommit, phaseClosePayload, prepared.destination, closeErr)
		return outcomeFromError(err), identity, err
	}
	return outcomeFromError(nil), identity, nil
}

func (prepared *PreparedRootedTree) Abort(ctx context.Context) error {
	if prepared == nil {
		return nil
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	return prepared.abortLocked(ctx)
}

func (prepared *PreparedRootedTree) abortLocked(ctx context.Context) error {
	if prepared.state == preparedRootedTreeTerminal {
		return nil
	}
	prepared.state = preparedRootedTreeTerminal
	if ctx == nil {
		ctx = context.Background()
	}
	var cleanupErr error
	if prepared.anchor != nil && prepared.stage != nil && !prepared.stageObject.valid() {
		cleanupErr = fmt.Errorf("prepared Windows stage identity is unavailable for safe cleanup")
	} else if prepared.anchor != nil && prepared.stage != nil {
		budget, err := newTreeTraversalBudget(prepared.limits)
		if err == nil {
			cleanupErr = removeWindowsEntryTree(
				context.WithoutCancel(ctx),
				prepared.anchor.parentHandle(),
				prepared.stageName,
				prepared.stagePath,
				prepared.stageObject,
				0,
				budget,
			)
		} else {
			cleanupErr = err
		}
		if cleanupErr == nil {
			cleanupErr = flushWindowsHandle(prepared.anchor.parentHandle(), windowsFlushPolicy{directory: true})
		}
	}
	return errors.Join(cleanupErr, prepared.releaseLocked())
}

func (prepared *PreparedRootedTree) releaseLocked() error {
	var failures []error
	if prepared.stage != nil && prepared.stage.handle != nil {
		failures = append(failures, prepared.stage.handle.Close())
		prepared.stage = nil
	}
	if prepared.anchor != nil {
		failures = append(failures, prepared.anchor.close())
		prepared.anchor = nil
	}
	if prepared.capability != nil {
		failures = append(failures, prepared.capability.Close())
		prepared.capability = nil
	}
	return errors.Join(failures...)
}

func (prepared *PreparedRootedTree) failBeforeVisibility(current phase, cause error) error {
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	return prepared.failBeforeVisibilityLocked(current, cause)
}

func (prepared *PreparedRootedTree) failBeforeVisibilityLocked(current phase, cause error) error {
	cleanupErr := prepared.abortLocked(context.Background())
	if cleanupErr != nil {
		return newFailure(failureRetainedResidue, current, prepared.destination, errors.Join(cause, cleanupErr), prepared.stagePath)
	}
	return windowsFailureBeforeVisibility(current, prepared.destination, windowsUnsupportedCause(cause))
}

func (prepared *PreparedRootedTree) finishVisibleLocked(
	current phase,
	cause error,
	residue ...string,
) error {
	prepared.state = preparedRootedTreeTerminal
	closeErr := prepared.releaseLocked()
	return newFailure(
		failureIndeterminateCommit,
		current,
		prepared.destination,
		errors.Join(cause, closeErr),
		residue...,
	)
}
