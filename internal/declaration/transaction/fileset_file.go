package transaction

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func readTransactionFile(
	ctx context.Context,
	path string,
) ([]byte, fs.FileMode, error) {
	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(
		ctx,
		path,
		maximumTargetBytes,
	)
	if err != nil {
		return nil, 0, err
	}
	return snapshot.Content(), snapshot.Mode(), nil
}

func commitFile(ctx context.Context, path string, content []byte, fileMode os.FileMode) error {
	commitPath, err := mutation.CanonicalDirectoryEntryPath(path)
	if err != nil {
		return err
	}
	_, err = os.Stat(commitPath)
	if errors.Is(err, os.ErrNotExist) {
		request, requestErr := storagecommit.NewFileCreate(commitPath, content, fileMode)
		if requestErr != nil {
			return requestErr
		}
		return storagecommit.CommitFile(ctx, request)
	}
	if err != nil {
		return err
	}
	expected, err := storagecommit.CaptureEntryIdentity(ctx, commitPath)
	if err != nil {
		return err
	}
	request, err := storagecommit.NewFileReplacement(commitPath, content, fileMode, expected)
	if err != nil {
		return err
	}
	return storagecommit.CommitFile(ctx, request)
}

func removeFile(ctx context.Context, path string) error {
	commitPath, err := mutation.CanonicalDirectoryEntryPath(path)
	if err != nil {
		return err
	}
	expected, err := storagecommit.CaptureEntryIdentity(ctx, commitPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	request, err := storagecommit.NewLogicalRemoval(commitPath, expected)
	if err != nil {
		return err
	}
	return storagecommit.CommitLogicalRemoval(ctx, request)
}

func commitMayBeVisible(err error) bool {
	kind, classified := mutationfs.FailureKindOf(err)
	return classified && kind == mutationfs.FailureIndeterminateCommit
}
