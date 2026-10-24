package execute

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// restoreDocumentDestination is only rooted publication. Its caller must supply
// the admitted recovery phase and full journal/state/ownership authority.
func restoreDocumentDestination(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationDestination,
	comparison journal.DocumentComparison,
	staged aggregate.Document,
	stagedMode *recovery.PermissionMode,
) (outcome fileMutationOutcome) {
	if ctx == nil {
		return fileMutationOutcome{err: fmt.Errorf("document restoration context is required")}
	}
	if err := ctx.Err(); err != nil {
		return fileMutationOutcome{err: err}
	}
	if err := comparison.Validate(); err != nil {
		return fileMutationOutcome{err: err}
	}
	if err := staged.Validate(); err != nil {
		return fileMutationOutcome{err: err}
	}
	stagedRoot, err := recovery.NewPartialDocumentState(staged.Exists(), stagedMode, nil)
	if err != nil {
		return fileMutationOutcome{err: err}
	}
	if authority == nil || authority.filesystem == nil {
		return fileMutationOutcome{err: fmt.Errorf("document restoration filesystem authority is required")}
	}

	capability, err := authority.acquire(destination)
	if err != nil {
		return fileMutationOutcome{err: err}
	}
	open := true
	defer func() {
		if open {
			outcome.err = errors.Join(outcome.err, capability.Close())
		}
	}()

	current, mode, identity, err := readRootedRestorationDocument(ctx, authority, capability, comparison.MaximumDocumentBytes())
	if err != nil {
		return fileMutationOutcome{err: err}
	}
	currentRoot, err := recovery.NewPartialDocumentState(current.Exists(), mode, nil)
	if err != nil {
		return fileMutationOutcome{err: err}
	}
	if !current.Equal(staged) || !currentRoot.SameRootMetadata(stagedRoot) {
		return fileMutationOutcome{err: fmt.Errorf("document changed after recovery staging")}
	}

	restored, err := comparison.RestoreBefore(ctx, current, mode)
	if err != nil {
		return fileMutationOutcome{err: err}
	}
	if err := restored.Validate(); err != nil {
		return fileMutationOutcome{err: err}
	}
	if !restored.Changed() {
		return fileMutationOutcome{}
	}

	fresh, freshMode, freshIdentity, err := readRootedRestorationDocument(ctx, authority, capability, comparison.MaximumDocumentBytes())
	if err != nil {
		return fileMutationOutcome{err: err}
	}
	freshRoot, err := recovery.NewPartialDocumentState(fresh.Exists(), freshMode, nil)
	if err != nil {
		return fileMutationOutcome{err: err}
	}
	if !fresh.Equal(current) || !freshRoot.SameRootMetadata(currentRoot) || current.Exists() && (identity == nil || freshIdentity == nil || !identity.Equal(freshIdentity)) {
		return fileMutationOutcome{err: fmt.Errorf("document changed while preparing recovery publication")}
	}

	candidate := restored.Document()
	fileMode, regular := restored.RegularFileMode()
	if candidate.Exists() != regular {
		return fileMutationOutcome{err: fmt.Errorf("restored document has inconsistent root metadata")}
	}
	open = false
	switch {
	case !candidate.Exists():
		_, err = authority.removeJournaledRootedEntry(ctx, destination, capability, freshIdentity)
	case !fresh.Exists():
		err = authority.filesystem.CreateRootedFile(ctx, capability, candidate.Content(), os.FileMode(fileMode))
	default:
		err = authority.filesystem.ReplaceRootedFile(ctx, capability, candidate.Content(), os.FileMode(fileMode), freshIdentity)
	}
	if err != nil {
		return attemptedFileMutation(err)
	}

	verification, err := authority.acquire(destination)
	if err != nil {
		return attemptedFileMutation(markHostEffectIndeterminate(err))
	}
	defer func() {
		if err := verification.Close(); err != nil {
			outcome.err = errors.Join(outcome.err, markHostEffectIndeterminate(err))
		}
	}()
	actual, actualMode, _, err := readRootedRestorationDocument(ctx, authority, verification, comparison.MaximumDocumentBytes())
	if err != nil {
		return attemptedFileMutation(markHostEffectIndeterminate(err))
	}
	actualRoot, err := recovery.NewPartialDocumentState(actual.Exists(), actualMode, nil)
	if err != nil {
		return attemptedFileMutation(markHostEffectIndeterminate(err))
	}
	var expectedMode *recovery.PermissionMode
	if regular {
		expectedMode = &fileMode
	}
	expectedRoot, err := recovery.NewPartialDocumentState(candidate.Exists(), expectedMode, nil)
	if err != nil || !actual.Equal(candidate) || !actualRoot.SameRootMetadata(expectedRoot) {
		return attemptedFileMutation(markHostEffectIndeterminate(fmt.Errorf("restored document postcondition differs")))
	}
	return attemptedFileMutation(nil)
}

func readRootedRestorationDocument(ctx context.Context, authority *mutationAuthority, capability rootedpath.CommitCapability, maximumBytes int64) (aggregate.Document, *recovery.PermissionMode, mutationfs.EntryIdentity, error) {
	content, mode, identity, err := authority.filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
	if errors.Is(err, os.ErrNotExist) {
		return aggregate.AbsentDocument(), nil, nil, nil
	}
	if err != nil {
		return aggregate.Document{}, nil, nil, err
	}
	if identity == nil {
		return aggregate.Document{}, nil, nil, fmt.Errorf("regular document read has no identity evidence")
	}
	permissionMode := recovery.PermissionMode(mode.Perm())
	return aggregate.ExistingDocument(content), &permissionMode, identity, nil
}
