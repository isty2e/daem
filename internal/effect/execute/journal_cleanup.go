package execute

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
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

// JournalCleanupOptions grants recovery-root cleanup dependencies only,
// without host-document, statefile or ownership Registry mutation authority.
type JournalCleanupOptions struct {
	ValidateBeforeEffects func(context.Context) error
	Filesystem            mutationfs.RootedStore
	InventoryProfile      journal.RecoveryInventoryProfile
	PhysicalBudget        *recovery.PhysicalWorkBudget
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
	action := plan.Action()
	defer func() {
		returnErr = journal.WrapCleanupFailure(action, returnErr)
	}()
	if options.Filesystem == nil {
		return fmt.Errorf("journal cleanup filesystem is required")
	}
	execution, err := newJournalCleanupExecution(plan)
	if err != nil {
		return fmt.Errorf("compile journal cleanup effect structure: %w", err)
	}
	physicalWorkBudget := options.PhysicalBudget
	if physicalWorkBudget == nil {
		physicalWorkBudget, err = recovery.NewPhysicalWorkBudget(0)
	} else if physicalWorkBudget.RequireCapturePending() == nil {
		err = physicalWorkBudget.AdmitRemovalDemands(recovery.RemovalDemandSet{})
	}
	if err != nil {
		return errors.Join(
			fmt.Errorf("construct journal cleanup physical work budget: %w", err),
			execution.abortBeforePreparation(),
		)
	}
	prepared, err := journal.PrepareJournalCleanupWithInventoryProfile(
		ctx,
		plan,
		paths.RecoveryDir,
		recovery.MaximumPhysicalPathDepth,
		physicalWorkBudget,
		options.Filesystem,
		options.InventoryProfile,
	)
	if err != nil {
		return errors.Join(
			fmt.Errorf("prepare bounded journal cleanup: %w", err),
			execution.abortBeforePreparation(),
		)
	}
	preparedClosed := false
	defer func() {
		if !preparedClosed {
			returnErr = errors.Join(returnErr, prepared.Close())
		}
	}()
	executionErr := execution.validateBeforeEffects(func() error {
		if options.ValidateBeforeEffects == nil {
			return nil
		}
		return options.ValidateBeforeEffects(ctx)
	})
	if executionErr == nil {
		executionErr = prepared.ExecuteCleanupWithGate(ctx, plan, execution)
	}
	closeErr := execution.close(prepared.Close)
	preparedClosed = true
	return errors.Join(executionErr, closeErr)
}
