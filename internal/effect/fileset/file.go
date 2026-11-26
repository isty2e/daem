package fileset

import (
	"context"
	"io/fs"

	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func readTransactionFile(
	ctx context.Context,
	path string,
) ([]byte, fs.FileMode, error) {
	return readTransactionFileUpTo(ctx, path, maximumTargetBytes)
}

func readTransactionFileUpTo(
	ctx context.Context,
	path string,
	maximumBytes int64,
) ([]byte, fs.FileMode, error) {
	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(
		ctx,
		path,
		maximumBytes,
	)
	if err != nil {
		return nil, 0, err
	}
	return snapshot.Content(), snapshot.Mode(), nil
}

func removeFile(ctx context.Context, path string, expected storagecommit.EntryIdentity) error {
	request, err := storagecommit.NewLogicalRemoval(path, expected)
	if err != nil {
		return err
	}
	return storagecommit.CommitLogicalRemoval(ctx, request)
}
