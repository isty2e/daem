package recover

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/fileset"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/recoverygate"
	permissions "github.com/isty2e/daem/internal/workflow/protection/recovery"
)

func planRecoveryOperation(ctx context.Context, input PlanInput) (*PreparedRecovery, error) {
	budget := physicalwork.NewCaptureBudget(nil)
	planned, journalErr := planRecoveryWithFilesystemFenceAndBudget(ctx, input, storagecommit.Adapter{}, func(ctx context.Context, authority recoverygate.StateDirAuthority) error {
		return authority.RequireClear(ctx)
	}, budget)
	// Retired-journal cleanup remains independent of host/state/Registry contents.
	if journalErr == nil && planned.plan.AuthorityKind() == journal.RecoveryAuthorityJournalCleanup {
		return newPreparedRecovery(planned), nil
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
		if journalErr == nil {
			return nil, fmt.Errorf("active journal and permission protection overlap for the selected owner")
		}
		state := recoverygate.StateOf(journalErr)
		if !errors.Is(journalErr, journal.ErrNoRecoverableJournal) || !state.FileSetKnown() || state.FileSet() != fileset.FileSetFenceClear {
			safety = safety.WithBlocker(journalErr)
		}
		return newPreparedRecovery(permissionPreparation{prepared: safety}), nil
	}
	if journalErr != nil {
		return nil, journalErr
	}
	return newPreparedRecovery(planned), nil
}
