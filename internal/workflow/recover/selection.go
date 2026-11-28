package recover

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal/qualification"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/recoverygate"
	permissions "github.com/isty2e/daem/internal/workflow/protection/recovery"
)

func planRecoveryOperation(ctx context.Context, input PlanInput) (*PreparedRecovery, error) {
	budget := physicalwork.NewCaptureBudget(nil)
	if input.BaselineTree != "" {
		hashes, err := qualification.ReadBaselineTree(ctx, input.BaselineTree, budget)
		if err != nil {
			return nil, err
		}
		input.treeProof = recovery.VerifiedTree(string(hashes.Source), string(hashes.Installed))
	}
	planned, journalErr := planRecoveryWithFilesystemFenceAndBudget(ctx, input, storagecommit.Adapter{}, func(ctx context.Context, authority recoverygate.StateDirAuthority) error {
		return authority.RequireClear(ctx)
	}, budget)
	// Retired-journal cleanup remains independent of host/state/Registry contents.
	if input.BaselineTree != "" {
		if journalErr != nil {
			return nil, journalErr
		}
		if planned.plan.AuthorityKind() != journal.RecoveryAuthorityActiveJournal {
			return nil, fmt.Errorf("--baseline-tree requires an active journal with a recorded directory")
		}
	}
	if journalErr == nil {
		if err := planned.requireProtectionClear(ctx); err != nil {
			return nil, fmt.Errorf("recover active journal: %w", err)
		}
		return newPreparedRecovery(planned), nil
	}
	var protected *ownership.ProtectionPendingError
	if errors.As(journalErr, &protected) {
		return nil, fmt.Errorf("recover active journal: %w", journalErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	paths, err := recoveryPaths(ctx, input)
	if err != nil {
		return nil, err
	}
	safety, found, err := permissions.Prepare(ctx, paths, budget)
	if err != nil {
		return nil, errors.Join(journalErr, err)
	}
	if found {
		state := recoverygate.StateOf(journalErr)
		if !errors.Is(journalErr, journal.ErrNoRecoverableJournal) || !state.FileSetKnown() || state.FileSet() != fileset.FileSetFenceClear {
			safety = safety.WithBlocker(journalErr)
		}
		return newPreparedRecovery(permissionPreparation{prepared: safety}), nil
	}
	return nil, journalErr
}
