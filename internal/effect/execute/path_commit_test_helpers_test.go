package execute

import (
	"context"
	"errors"
	"os"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

// commitFile is a fault-injection helper for tests whose callback contract
// intentionally supplies a selected pathname instead of production authority.
func commitFile(
	ctx context.Context,
	filesystem mutationfs.PathStore,
	path string,
	content []byte,
	fileMode os.FileMode,
) error {
	if filesystem == nil {
		return errors.New("path filesystem is required")
	}
	commitPath, err := mutation.CanonicalDirectoryEntryPath(path)
	if err != nil {
		return err
	}
	expected, err := filesystem.CaptureEntryIdentity(ctx, commitPath)
	if errors.Is(err, os.ErrNotExist) {
		return filesystem.CreateFile(ctx, commitPath, content, fileMode)
	}
	if err != nil {
		return err
	}
	return filesystem.ReplaceFile(ctx, commitPath, content, fileMode, expected)
}
