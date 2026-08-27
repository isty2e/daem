//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

type createdDirectory struct {
	path                   string
	identity               EntryIdentity
	fd                     int
	cleanupState           createdDirectoryCleanupState
	pendingDurabilityError error
}

func (directory createdDirectory) valid() bool {
	if directory.fd < 0 || directory.path == "" || directory.identity.path != directory.path ||
		directory.identity.kind != entryKindDirectory || !directory.identity.valid() {
		return false
	}
	switch directory.cleanupState {
	case createdDirectoryCleanupActive, createdDirectoryCleanupRetired:
		return directory.pendingDurabilityError == nil
	case createdDirectoryCleanupPendingDurability:
		return directory.pendingDurabilityError != nil
	default:
		return false
	}
}

type createdDirectoryCleanupState uint8

const (
	createdDirectoryCleanupActive createdDirectoryCleanupState = iota
	createdDirectoryCleanupPendingDurability
	createdDirectoryCleanupRetired
)

// AncestorCleanup owns live handles for parent directories created by the
// operations run through it. Its authority is limited to empty rollback
// cleanup of those exact objects; it grants no recursive or durable ownership.
type AncestorCleanup struct {
	state *ancestorCleanupState
}

type ancestorCleanupState struct {
	directories []createdDirectory
	closed      bool
}

// PrepareParent creates missing commit parents and retains exact cleanup
// authority for every directory created by this invocation.
func (cleanup *AncestorCleanup) PrepareParent(ctx context.Context, path string) error {
	if _, err := cleanup.requireOpen(); err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	return prepareCommitParentWithFaults(ctx, path, faultPlan{}, cleanup)
}

// CommitFile publishes a file while retaining cleanup authority for any parent
// directories recreated by the commit after an earlier preparation step.
func (cleanup *AncestorCleanup) CommitFile(ctx context.Context, request FileCommit) error {
	if _, err := cleanup.requireOpen(); err != nil {
		return failureBeforeVisibility(phaseValidate, request.path, err)
	}
	return commitFileWithFaultsAndParentRefresh(ctx, request, faultPlan{}, nil, cleanup)
}

// CreatedDirectoryIdentity returns the exact identity retained for a directory
// created by this cleanup authority. Existing or externally created
// directories are never reported as created by this invocation.
func (cleanup *AncestorCleanup) CreatedDirectoryIdentity(path string) (EntryIdentity, bool, error) {
	state, err := cleanup.requireOpen()
	if err != nil {
		return EntryIdentity{}, false, err
	}
	for index := range state.directories {
		directory := state.directories[index]
		if directory.path == path && directory.cleanupState == createdDirectoryCleanupActive && directory.valid() {
			return directory.identity, true, nil
		}
	}
	return EntryIdentity{}, false, nil
}

// RemoveEmpty attempts exact empty cleanup in reverse creation order. It keeps
// checking later entries after one refusal so every retained path is reported.
// A prior post-unlink durability failure remains indeterminate on every retry.
func (cleanup *AncestorCleanup) RemoveEmpty(ctx context.Context) error {
	state, err := cleanup.requireOpen()
	if err != nil {
		return err
	}
	cleanupErrors := make([]error, 0)
	for index := len(state.directories) - 1; index >= 0; index-- {
		directory := &state.directories[index]
		if err := removeCreatedDirectoryIfEmptyWithFaults(ctx, directory, faultPlan{}); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf(
				"remove created ancestor %q: %w",
				directory.path,
				err,
			))
		}
	}
	return errors.Join(cleanupErrors...)
}

// Close releases all retained object handles. Close is idempotent; cleanup
// authority cannot be used after it returns.
func (cleanup *AncestorCleanup) Close() {
	if cleanup == nil {
		return
	}
	if cleanup.state == nil {
		cleanup.state = &ancestorCleanupState{closed: true}
		return
	}
	if cleanup.state.closed {
		return
	}
	for index := len(cleanup.state.directories) - 1; index >= 0; index-- {
		if cleanup.state.directories[index].fd >= 0 {
			_ = unix.Close(cleanup.state.directories[index].fd)
			cleanup.state.directories[index].fd = -1
		}
	}
	cleanup.state.closed = true
}

func (cleanup *AncestorCleanup) requireOpen() (*ancestorCleanupState, error) {
	if cleanup == nil {
		return nil, fmt.Errorf("ancestor cleanup authority is required")
	}
	if cleanup.state == nil {
		cleanup.state = &ancestorCleanupState{}
	}
	if cleanup.state.closed {
		return nil, fmt.Errorf("ancestor cleanup authority is closed")
	}
	return cleanup.state, nil
}

func (cleanup *AncestorCleanup) capture(anchor *anchoredParent) {
	if cleanup == nil || cleanup.state == nil || cleanup.state.closed || anchor == nil {
		return
	}
	for index := range anchor.directories {
		directory := &anchor.directories[index]
		candidate := createdDirectory{
			path:     directory.path,
			identity: directory.identity,
			fd:       directory.fd,
		}
		if !directory.created || !candidate.valid() {
			continue
		}
		cleanup.state.directories = append(cleanup.state.directories, candidate)
		directory.fd = -1
	}
}

// PrepareCommitParent creates and persists missing parent directories for a
// later file or same-parent prepared-tree commit.
func PrepareCommitParent(ctx context.Context, path string) error {
	return prepareCommitParentWithFaults(ctx, path, faultPlan{}, nil)
}

func prepareCommitParentWithFaults(
	ctx context.Context,
	path string,
	faults faultPlan,
	cleanup *AncestorCleanup,
) error {
	if err := validateCommitPath(path); err != nil {
		return newFailure(failureUncommitted, phaseValidate, path, err)
	}
	if cleanup != nil {
		if _, err := cleanup.requireOpen(); err != nil {
			return newFailure(failureUncommitted, phaseValidate, path, err)
		}
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return newFailure(failureUncommitted, phaseValidate, path, err)
	}
	if err := faults.check(ctx, phaseCreateAncestors); err != nil {
		return newFailure(failureUncommitted, phaseCreateAncestors, path, err)
	}
	hooks := ancestorPublicationHooks{}
	if action := faults.actions[phaseCommitEntry]; action != nil {
		hooks.before = func(string) { action() }
	}
	if action := faults.actions[phasePublishAncestor]; action != nil {
		hooks.after = func(string) { action() }
	}
	anchor, err := openCommitParentWithPublicationHooks(path, nil, true, hooks)
	if anchor != nil {
		defer anchor.close()
		defer cleanup.capture(anchor)
	}
	if err != nil {
		return failFileBeforeVisibility(
			path,
			phaseCreateAncestors,
			err,
			anchor,
			"",
			EntryIdentity{},
			faults,
		)
	}
	if err := anchor.verifyChain(); err != nil {
		return failFileBeforeVisibility(path, phaseValidate, err, anchor, "", EntryIdentity{}, faults)
	}
	if err := faults.check(ctx, phaseSyncAncestors); err != nil {
		return failFileBeforeVisibility(
			path,
			phaseSyncAncestors,
			err,
			anchor,
			"",
			EntryIdentity{},
			faults,
		)
	}
	if err := syncCreatedAncestors(anchor); err != nil {
		return failFileBeforeVisibility(
			path,
			phaseSyncAncestors,
			err,
			anchor,
			"",
			EntryIdentity{},
			faults,
		)
	}
	if err := anchor.verifyChain(); err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, path, err)
	}
	return nil
}

func removeCreatedDirectoryIfEmptyWithFaults(
	ctx context.Context,
	directory *createdDirectory,
	faults faultPlan,
) error {
	if directory == nil {
		return failureBeforeVisibility(
			phaseValidate,
			"",
			fmt.Errorf("created directory cleanup evidence is required"),
		)
	}
	path := directory.path
	if ctx == nil {
		return failureBeforeVisibility(
			phaseValidate,
			path,
			fmt.Errorf("created directory cleanup context is required"),
		)
	}
	if !directory.valid() {
		return failureBeforeVisibility(
			phaseValidate,
			path,
			fmt.Errorf("valid created directory evidence is required"),
		)
	}
	switch directory.cleanupState {
	case createdDirectoryCleanupPendingDurability:
		return directory.pendingDurabilityError
	case createdDirectoryCleanupRetired:
		return nil
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}

	anchor, err := openCommitParent(path, nil, false)
	if anchor != nil {
		defer anchor.close()
	}
	if errors.Is(err, unix.ENOENT) {
		return classifyMissingCreatedDirectory(*directory, phaseValidate)
	}
	if err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := requireEmptyCreatedDirectory(ctx, anchor, *directory); errors.Is(err, unix.ENOENT) {
		return classifyMissingCreatedDirectory(*directory, phaseValidate)
	} else if err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return classifyMissingCreatedDirectory(*directory, phaseValidate)
		}
		return failureBeforeVisibility(phaseValidate, path, err)
	}

	err = faults.run(ctx, phaseRevalidateEntry, func() error {
		return requireEmptyCreatedDirectory(ctx, anchor, *directory)
	})
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return classifyMissingCreatedDirectory(*directory, phaseRevalidateEntry)
		}
		return failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return classifyMissingCreatedDirectory(*directory, phaseRevalidateEntry)
		}
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return failureBeforeVisibility(phaseCleanupEntry, path, err)
	}
	if err := requireEmptyCreatedDirectory(ctx, anchor, *directory); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return classifyMissingCreatedDirectory(*directory, phaseCleanupEntry)
		}
		return failureBeforeVisibility(phaseCleanupEntry, path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return classifyMissingCreatedDirectory(*directory, phaseCleanupEntry)
		}
		return failureBeforeVisibility(phaseCleanupEntry, path, err)
	}
	if err := unix.Unlinkat(anchor.parentFD(), anchor.base, unix.AT_REMOVEDIR); err != nil {
		return classifyCreatedDirectoryCleanupFailure(anchor, *directory, err)
	}
	directory.cleanupState = createdDirectoryCleanupPendingDurability
	if err := faults.run(ctx, phaseSyncCleanupParent, func() error {
		return syncDirectory(anchor.parentFD())
	}); err != nil {
		return retainCreatedDirectoryDurabilityFailure(directory, newFailure(
			failureIndeterminateCommit,
			phaseSyncCleanupParent,
			path,
			err,
			path,
		))
	}
	if err := anchor.verifyChain(); err != nil {
		return retainCreatedDirectoryDurabilityFailure(
			directory,
			newFailure(failureIndeterminateCommit, phaseVerifyEntry, path, err, path),
		)
	}
	observed, _, err := anchor.observe(anchor.base, path)
	switch {
	case errors.Is(err, unix.ENOENT):
		retireCreatedDirectory(directory)
		return nil
	case err != nil:
		return retainCreatedDirectoryDurabilityFailure(
			directory,
			newFailure(failureIndeterminateCommit, phaseVerifyEntry, path, err, path),
		)
	case directory.identity.sameObject(observed):
		return retainCreatedDirectoryDurabilityFailure(directory, newFailure(
			failureIndeterminateCommit,
			phaseVerifyEntry,
			path,
			fmt.Errorf("cleaned directory identity reappeared"),
			path,
		))
	default:
		retireCreatedDirectory(directory)
		return nil
	}
}

func retainCreatedDirectoryDurabilityFailure(directory *createdDirectory, err error) error {
	directory.cleanupState = createdDirectoryCleanupPendingDurability
	directory.pendingDurabilityError = err
	return err
}

func retireCreatedDirectory(directory *createdDirectory) {
	directory.cleanupState = createdDirectoryCleanupRetired
	directory.pendingDurabilityError = nil
}

func classifyMissingCreatedDirectory(directory createdDirectory, failedPhase phase) error {
	var stat unix.Stat_t
	if err := unix.Fstat(directory.fd, &stat); err != nil {
		return failureBeforeVisibility(
			failedPhase,
			directory.path,
			fmt.Errorf("inspect retained created directory handle %q: %w", directory.path, err),
		)
	}
	liveIdentity := identityFromStat(directory.path, &stat)
	if !directory.identity.sameObject(liveIdentity) || liveIdentity.kind != entryKindDirectory {
		return failureBeforeVisibility(
			failedPhase,
			directory.path,
			fmt.Errorf("created directory handle changed at %q", directory.path),
		)
	}
	linked, err := retainedDirectoryStillLinked(directory.fd, directory.identity, &stat)
	if err != nil {
		return newFailure(
			failureRetainedResidue,
			failedPhase,
			directory.path,
			fmt.Errorf("determine retained created directory link state: %w", err),
			directory.path,
		)
	}
	if !linked {
		return nil
	}
	return newFailure(
		failureRetainedResidue,
		failedPhase,
		directory.path,
		fmt.Errorf("created directory disappeared before daem retirement but remains linked"),
		directory.path,
	)
}

func requireEmptyCreatedDirectory(
	ctx context.Context,
	anchor *anchoredParent,
	directory createdDirectory,
) error {
	liveIdentity, err := refreshOpenedIdentity(directory.fd, directory.path)
	if err != nil {
		return fmt.Errorf("inspect retained created directory handle %q: %w", directory.path, err)
	}
	if !directory.identity.sameObject(liveIdentity) || liveIdentity.kind != entryKindDirectory {
		return fmt.Errorf("created directory handle changed at %q", directory.path)
	}
	observed, stat, err := anchor.observe(anchor.base, directory.path)
	if err != nil {
		return err
	}
	if !liveIdentity.sameObject(observed) || observed.kind != entryKindDirectory {
		return fmt.Errorf("created directory identity changed at %q", directory.path)
	}
	if err := validateOwnedStat(directory.path, &stat); err != nil {
		return err
	}
	directoryFD, _, err := anchor.openExpected(anchor.base, directory.path, observed)
	if err != nil {
		return err
	}
	names, readErr := readDirectoryNames(ctx, directoryFD, directory.path, 1)
	closeErr := unix.Close(directoryFD)
	if readErr != nil || closeErr != nil {
		return fmt.Errorf(
			"inspect created directory %q before cleanup: %w",
			directory.path,
			errors.Join(readErr, closeErr),
		)
	}
	if len(names) != 0 {
		return fmt.Errorf("created directory %q is not empty", directory.path)
	}
	return nil
}

func classifyCreatedDirectoryCleanupFailure(
	anchor *anchoredParent,
	directory createdDirectory,
	cause error,
) error {
	observed, _, observeErr := anchor.observe(anchor.base, directory.path)
	switch {
	case observeErr == nil && directory.identity.sameObject(observed):
		return failureBeforeVisibility(phaseCleanupEntry, directory.path, cause)
	case errors.Is(observeErr, unix.ENOENT):
		return newFailure(
			failureIndeterminateCommit,
			phaseCleanupEntry,
			directory.path,
			cause,
			directory.path,
		)
	default:
		return newFailure(
			failureIndeterminateCommit,
			phaseCleanupEntry,
			directory.path,
			errors.Join(cause, observeErr),
			directory.path,
		)
	}
}
