package recoverygate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/fileset"
	daempaths "github.com/isty2e/daem/internal/paths"
)

var ErrGlobalMetadataFence = errors.New("global metadata transaction boundary is not clear")

// IsGlobalMetadataRefusal distinguishes shared-transaction failure from the
// selected owner's journal and file-set recovery axes.
func IsGlobalMetadataRefusal(err error) bool {
	return errors.Is(err, ErrGlobalMetadataFence)
}

// GlobalMetadataAuthorityPath is shared by every participant in one DataDir.
func GlobalMetadataAuthorityPath(paths daempaths.Paths) (string, error) {
	if err := validateGlobalMetadataRoot(paths.DataDir); err != nil {
		return "", err
	}
	return fileset.FileSetAuthorityPath(paths.GlobalMetadataDir())
}

// RequireGlobalMetadataClear does not recover a transaction or grant access to
// its targets. The owning transaction must clear its durable evidence first.
func RequireGlobalMetadataClear(ctx context.Context, paths daempaths.Paths) error {
	return requireGlobalMetadataClear(ctx, paths, &stateDirOperationWorkBudget{})
}

func requireGlobalMetadataClear(ctx context.Context, paths daempaths.Paths, budget stateDirPhysicalWorkBudget) error {
	if err := requireBarrierContext(ctx); err != nil {
		return err
	}
	if err := validateGlobalMetadataRoot(paths.DataDir); err != nil {
		return err
	}
	if budget == nil {
		return fmt.Errorf("global metadata physical work budget is required")
	}
	err := fileset.ObserveClearFenceAt(ctx, paths.GlobalMetadataDir(), defaultStateDirMaximumPhysicalDepth, budget)
	if cancellation := cancellationCause(ctx.Err(), err); cancellation != nil {
		return cancellation
	}
	if err != nil {
		return fmt.Errorf("%w at %q: %w", ErrGlobalMetadataFence, paths.GlobalMetadataDir(), err)
	}
	return nil
}

func validateGlobalMetadataRoot(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("global metadata DataDir must be absolute and clean")
	}
	return nil
}
