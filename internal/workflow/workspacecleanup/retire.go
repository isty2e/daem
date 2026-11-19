package workspacecleanup

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/fileset"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/recoverygate"
)

// Retire runs only the sealed cleanup, keeping one lease through both owners.
// A nil filesystem uses the production journal storage adapter. Even on
// failure the returned rows describe the approved steps in owner order.
func (plan *Plan) Retire(ctx context.Context, filesystem mutationfs.Store, revalidateCommand func(context.Context) error) (steps []Step, returnErr error) {
	if ctx == nil || plan == nil || plan.pending == nil || revalidateCommand == nil {
		return nil, fmt.Errorf("retirement requires a sealed cleanup plan and command revalidation")
	}
	if filesystem == nil {
		filesystem = storagecommit.Adapter{}
	}
	steps = plan.Steps()
	for index := range steps {
		steps[index].Outcome = NotAttempted
	}
	domains, err := plan.domains()
	if err != nil {
		return steps, err
	}
	store, err := mutation.NewStore(plan.pending.paths.DataDir)
	if err != nil {
		return steps, err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return steps, err
	}
	defer func() { returnErr = errors.Join(returnErr, leases.Release()) }()

	pending := plan.pending
	validateCommand := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
			return err
		} else if !matches {
			return mutation.StaleSnapshotError{}
		}
		return revalidateCommand(ctx)
	}
	validateBoth := func(ctx context.Context) error {
		if err := validateCommand(ctx); err != nil {
			return err
		}
		if err := pending.validate(ctx, filesystem); err != nil {
			return err
		}
		return plan.checkDisjoint(true)
	}
	if err := validateBoth(ctx); err != nil {
		return steps, err
	}

	index := 0
	if pending.journal != nil {
		outcome, err := pending.retireJournal(ctx, filesystem, validateBoth)
		steps[index].Outcome = outcome
		if err != nil {
			return steps, err
		}
		index++
	}
	if pending.pair != nil {
		validatePair := func(ctx context.Context) error {
			if err := validateCommand(ctx); err != nil {
				return err
			}
			if _, err := pending.validateAxes(ctx, false); err != nil {
				return err
			}
			if err := pending.barrier.ValidateFileSetRecovery(ctx); err != nil {
				return err
			}
			return plan.checkDisjoint(false)
		}
		if err := validatePair(ctx); err != nil {
			return steps, err
		}
		retired, err := pending.pair.Retire(ctx, pending.declarationControls(), physicalwork.NewPathBudget(), validatePair)
		switch {
		case retired:
			steps[index].Outcome = Retired
		case errors.Is(err, fileset.ErrFileSetRetirementUnconfirmed):
			steps[index].Outcome = Unconfirmed
		case fileset.FileSetFenceKindOf(pending.barrier.Validate(ctx)) == fileset.FileSetFencePublishedTransaction:
			steps[index].Outcome = StillFenced
		default:
			steps[index].Outcome = Unconfirmed
		}
		if err != nil {
			return steps, err
		}
	}
	if err := validateCommand(ctx); err != nil {
		return steps, err
	}
	return steps, pending.barrier.Validate(ctx)
}

func (pending *Pending) retireJournal(ctx context.Context, filesystem mutationfs.RootedStore, validate func(context.Context) error) (outcome Outcome, returnErr error) {
	cleanup, ok := journal.JournalCleanupPlan(pending.journal)
	if !ok {
		return NotAttempted, fmt.Errorf("journal has no cleanup authority")
	}
	depth := physicalwork.MaximumPhysicalPathDepth
	root, err := rootedpath.CaptureCanonicalRootBounded(pending.recoveryRoot, depth, physicalwork.NewPathBudget())
	if err != nil {
		return NotAttempted, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()

	started := false
	err = execute.ExecuteJournalCleanupWithOptions(ctx, cleanup, execute.JournalCleanupPaths{RecoveryDir: pending.recoveryRoot}, execute.JournalCleanupOptions{
		ValidateBeforeEffects: func(ctx context.Context) error {
			if err := validate(ctx); err != nil {
				return err
			}
			if err := root.ValidateSelectionBounded(pending.recoveryRoot, depth, physicalwork.NewPathBudget()); err != nil {
				return err
			}
			started = true
			return nil
		},
		Filesystem:       filesystem,
		InventoryProfile: pending.profile,
	})
	if !started {
		return NotAttempted, err
	}
	retired := err == nil || journal.IsRetirementFinalizedWithGCResidue(err)
	if rootErr := root.ValidateSelectionBounded(pending.recoveryRoot, depth, physicalwork.NewPathBudget()); rootErr != nil {
		if retired {
			return Retired, errors.Join(err, rootErr)
		}
		return Unconfirmed, errors.Join(err, rootErr)
	}
	clearErr := journal.RequireNoInterruptedApply(ctx, pending.recoveryRoot)
	switch {
	case retired:
		// Clearing the approved record does not authorize another action if
		// a later journal observation is blocked or cancelled.
		return Retired, errors.Join(err, clearErr)
	case recoverygate.IsCancellation(clearErr):
		return Unconfirmed, clearErr
	case errors.Is(clearErr, journal.ErrIncompleteJournalCleanup):
		return StillFenced, errors.Join(err, clearErr)
	default:
		return Unconfirmed, errors.Join(err, clearErr)
	}
}

func (plan *Plan) domains() ([]mutation.Domain, error) {
	pending := plan.pending
	domains := pending.barrier.Domains()
	add := func(path string, access mutation.AccessMode, effects ...mutation.PathEffect) error {
		if path == "" {
			return nil
		}
		for _, effect := range effects {
			domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: access, Effect: effect})
			if err != nil {
				return err
			}
			domains = append(domains, domain)
		}
		return nil
	}
	for _, path := range plan.protected {
		access := mutation.AccessShared
		if pending.pair != nil && (path == pending.paths.ManifestPath || path == pending.paths.LockfilePath) {
			access = mutation.AccessExclusive
		}
		if err := add(path, access, mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent); err != nil {
			return nil, err
		}
	}
	// The exclusive RecoveryDir lease covers its retiring slots. Lease-domain
	// revalidation must not include entries that the first owner removes.
	if err := add(pending.marker, mutation.AccessExclusive, mutation.PathEffectDirectoryEntry); err != nil {
		return nil, err
	}
	return domains, nil
}
