package lock

import (
	"context"
	"errors"
	"io/fs"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

type lockPublication struct {
	path     string
	mode     fs.FileMode
	expected mutationfs.EntryIdentity // nil only for an observed absent entry
}

func observeLockPublication(ctx context.Context, revisions mutation.RevisionSet, selected string) (lockPublication, previousLock, error) {
	requests, err := mutation.BoundedFileRevisionRequests(declarationartifact.MaximumBytes, selected)
	if err != nil {
		return lockPublication{}, previousLock{}, err
	}
	referentRequest, err := mutation.NewBoundedFileRevisionRequest(declarationartifact.MaximumBytes, selected, mutation.PathEffectReferent)
	if err != nil {
		return lockPublication{}, previousLock{}, err
	}
	path, present, err := revisions.CapturedReferent(referentRequest)
	if err != nil {
		return lockPublication{}, previousLock{}, err
	}
	publication := lockPublication{path: path, mode: 0o600}
	previous := missingPreviousLock()
	if present {
		snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, path, declarationartifact.MaximumBytes)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				err = errors.Join(mutation.StaleSnapshotError{}, err)
			}
			return lockPublication{}, previousLock{}, err
		}
		publication.mode = snapshot.Mode()
		publication.expected = snapshot.Identity()
		previous, err = decodePreviousLock(ctx, snapshot.Content())
		if err != nil {
			return lockPublication{}, previousLock{}, err
		}
	}

	lockRevisions, err := revisions.Subset(requests...)
	if err != nil {
		return lockPublication{}, previousLock{}, err
	}
	matches, err := lockRevisions.MatchesCurrent(ctx)
	if err != nil {
		return lockPublication{}, previousLock{}, err
	}
	if !matches {
		return lockPublication{}, previousLock{}, mutation.StaleSnapshotError{}
	}
	return publication, previous, nil
}

func (publication lockPublication) commit(ctx context.Context, content []byte) (returnErr error) {
	defer func() {
		if returnErr != nil {
			returnErr = declarationartifact.ReplacementError{Path: publication.path, Err: returnErr}
		}
	}()
	filesystem := storagecommit.Adapter{}
	if publication.expected == nil {
		return filesystem.CreateFile(ctx, publication.path, content, publication.mode)
	}
	return filesystem.ReplaceFile(ctx, publication.path, content, publication.mode, publication.expected)
}
