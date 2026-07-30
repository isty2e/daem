package execute

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// JournalCleanupPaths contains only the recovery root reachable by cleanup-only
// recovery.
type JournalCleanupPaths struct {
	RecoveryDir string
}

func (paths JournalCleanupPaths) validate() error {
	if paths.RecoveryDir == "" ||
		!filepath.IsAbs(paths.RecoveryDir) ||
		filepath.Clean(paths.RecoveryDir) != paths.RecoveryDir {
		return fmt.Errorf(
			"journal cleanup recovery root must be one canonical absolute path",
		)
	}
	return nil
}

// JournalCleanupOptions supplies only the last-safe-point validator and
// recovery-root filesystem needed by cleanup-only recovery.
type JournalCleanupOptions struct {
	ValidateBeforeEffects func(context.Context) error
	Filesystem            mutationfs.RootedStore
}

// ExecuteJournalCleanupWithOptions finalizes one cleanup-only plan without
// acquiring host, statefile, or ownership authority.
func ExecuteJournalCleanupWithOptions(
	ctx context.Context,
	plan retirement.CleanupPlan,
	paths JournalCleanupPaths,
	options JournalCleanupOptions,
) (returnErr error) {
	if ctx == nil {
		return fmt.Errorf("journal cleanup context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := paths.validate(); err != nil {
		return err
	}
	if plan.Classification() == "" || plan.Action() == "" {
		return fmt.Errorf("journal cleanup plan is uninitialized")
	}
	if options.Filesystem == nil {
		return fmt.Errorf("journal cleanup filesystem is required")
	}
	root, err := rootedpath.CaptureRoot(paths.RecoveryDir)
	if err != nil {
		return fmt.Errorf("capture recovery root for journal cleanup: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, root.Close())
	}()
	if options.ValidateBeforeEffects != nil {
		if err := options.ValidateBeforeEffects(ctx); err != nil {
			return err
		}
	}
	return journal.FinalizeJournalCleanup(ctx, plan, root, options.Filesystem)
}
