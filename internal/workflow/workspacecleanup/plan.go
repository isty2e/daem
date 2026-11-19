// Package workspacecleanup composes the declaration and journal owners'
// cleanup-only authority for a selected workspace.
package workspacecleanup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/fileset"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

// Kind is one cleanup owner, in execution order.
type Kind string

const (
	JournalRetirement Kind = "journal_retirement"
	DeclarationPair   Kind = "declaration_pair"
)

// Outcome reports what could be established about one approved step.
type Outcome string

const (
	NotAttempted Outcome = "not_attempted"
	Retired      Outcome = "retired"
	StillFenced  Outcome = "still_fenced"
	Unconfirmed  Outcome = "unconfirmed"
)

// Step is a path-neutral disclosure. Preview does not assign an outcome.
type Step struct {
	Kind    Kind
	Outcome Outcome
}

// MayQualify recognizes only known, potentially eligible barrier combinations.
// The owners still have to prove the exact evidence.
func MayQualify(err error) bool {
	if err == nil || recoverygate.IsCancellation(err) || recoverygate.IsGlobalMetadataRefusal(err) {
		return false
	}
	state := recoverygate.StateOf(err)
	return state.JournalKnown() && state.FileSetKnown() &&
		(state.Journal() == journal.InterruptionClear || state.Journal() == journal.InterruptionCleanupOnly) &&
		(state.FileSet() == fileset.FileSetFenceClear || state.FileSet() == fileset.FileSetFencePublishedTransaction) &&
		(state.Journal() != journal.InterruptionClear || state.FileSet() != fileset.FileSetFenceClear)
}

// Pending proves eligible records for read-only planning. It cannot retire them
// until the command has supplied and checked its protected paths.
type Pending struct {
	paths        daempaths.Paths
	barrier      recoverygate.EffectAuthority
	stateDir     string
	marker       string
	recoveryRoot string
	journal      journal.RecoverablePlan
	profile      journal.RecoveryInventoryProfile
	pair         *transaction.SettledPair
}

// Observe returns nil for a clear workspace or a refusal if either axis cannot
// be qualified. The ordinary barrier supplies the selected paths.
func Observe(ctx context.Context, barrier recoverygate.EffectAuthority, barrierErr error) (*Pending, error) {
	if !MayQualify(barrierErr) {
		return nil, barrierErr
	}
	paths, err := barrier.SelectedPaths()
	if err != nil {
		return nil, err
	}
	stateDir, err := barrier.ExistingStateDir(ctx)
	if err != nil {
		return nil, err
	}
	marker, err := fileset.FileSetAuthorityPath(stateDir)
	if err != nil {
		return nil, err
	}
	pending := &Pending{paths: paths, barrier: barrier, stateDir: stateDir, marker: marker}
	state := recoverygate.StateOf(barrierErr)
	budget := physicalwork.NewPathBudget()
	if state.Journal() == journal.InterruptionCleanupOnly {
		pending.recoveryRoot, err = pending.boundRecoveryRoot(budget)
		if err != nil {
			return nil, err
		}
		pending.profile, err = nativeconfig.RecoveryInventoryProfile(ctx)
		if err != nil {
			return nil, err
		}
		pending.journal, err = pending.observeJournal(ctx, storagecommit.Adapter{}, budget)
		if err != nil {
			return nil, err
		}
	}
	if state.FileSet() == fileset.FileSetFencePublishedTransaction {
		selection, err := transaction.CaptureFinalizationPair(ctx, paths.ManifestPath, paths.LockfilePath)
		if err != nil {
			return nil, err
		}
		pending.pair, err = selection.ObserveSettled(ctx, stateDir, pending.declarationControls(), budget)
		if err != nil {
			if fileset.FileSetFenceKindOf(err) != fileset.FileSetFenceClear {
				return nil, recoverygate.RefineFileSetRefusal(barrierErr, err)
			}
			return nil, err
		}
		if pending.pair == nil {
			return nil, mutation.StaleSnapshotError{}
		}
	}
	if err := pending.validateAxes(ctx, pending.journal != nil); err != nil {
		return nil, err
	}
	return pending, nil
}

func (pending *Pending) boundRecoveryRoot(budget *physicalwork.PathBudget) (string, error) {
	root, err := mutation.ResolveDirectoryEntryPathBounded(pending.paths.RecoveryDir, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return "", err
	}
	if root != filepath.Join(pending.stateDir, filepath.Base(pending.paths.RecoveryDir)) {
		return "", fmt.Errorf("journal cleanup requires the recovery directory of the selected workspace")
	}
	// The journal inventory opens this final entry without following a link.
	return root, nil
}

func (pending *Pending) observeJournal(ctx context.Context, filesystem journalFilesystem, budget *physicalwork.PathBudget) (journal.RecoverablePlan, error) {
	root, err := pending.boundRecoveryRoot(budget)
	if err != nil {
		return nil, err
	}
	if root != pending.recoveryRoot {
		return nil, mutation.StaleSnapshotError{}
	}
	return journal.ObserveJournalCleanup(ctx, root, filesystem, budget, pending.profile)
}

func (pending *Pending) validateAxes(ctx context.Context, journalPending bool) error {
	if current, err := pending.barrier.ExistingStateDir(ctx); err != nil {
		return err
	} else if current != pending.stateDir {
		return mutation.StaleSnapshotError{}
	}
	err := pending.barrier.Validate(ctx)
	if !MayQualify(err) {
		if err == nil {
			return mutation.StaleSnapshotError{}
		}
		return err
	}
	state := recoverygate.StateOf(err)
	if (state.Journal() == journal.InterruptionCleanupOnly) != journalPending ||
		(state.FileSet() == fileset.FileSetFencePublishedTransaction) != (pending.pair != nil) {
		return mutation.StaleSnapshotError{}
	}
	return nil
}

// Validate checks that the private planning exception still refers to the same
// records, not merely to another record with the same interruption category.
func (pending *Pending) Validate(ctx context.Context) error {
	return pending.validate(ctx, storagecommit.Adapter{})
}

// ReadSettledManifest reads the selected pair's verified manifest image.
func (pending *Pending) ReadSettledManifest(ctx context.Context) ([]byte, error) {
	if pending == nil || pending.pair == nil {
		return nil, fmt.Errorf("cleanup has no settled declaration pair")
	}
	return pending.pair.ReadManifest(ctx)
}

func (pending *Pending) validate(ctx context.Context, filesystem journalFilesystem) error {
	if err := pending.validateAxes(ctx, pending.journal != nil); err != nil {
		return err
	}
	budget := physicalwork.NewPathBudget()
	if pending.journal != nil {
		current, err := pending.observeJournal(ctx, filesystem, budget)
		if err != nil {
			return err
		}
		if !pending.journal.SameExecutionAuthority(current) {
			return mutation.StaleSnapshotError{}
		}
	}
	if pending.pair != nil {
		return pending.pair.Validate(ctx, pending.declarationControls(), budget)
	}
	return nil
}

func (pending *Pending) declarationControls() []string {
	return append(pending.workspaceControls(), pending.marker, pending.paths.RecoveryDir)
}

func (pending *Pending) workspaceControls() []string {
	paths := pending.paths
	return []string{
		paths.StatefilePath, paths.OwnershipRegistryPath, paths.CarrierClaimRegistryPath,
		paths.CacheDir, paths.SourceCacheDir, paths.GlobalMetadataDir(), paths.LegacyUserStateDir,
	}
}

// Plan is the owner-qualified cleanup sealed with command input and output
// locations. Only it can acquire leases and retire records.
type Plan struct {
	pending   *Pending
	protected []string
}

func (pending *Pending) Seal(ctx context.Context, commandPaths []string) (*Plan, error) {
	if pending == nil || (pending.journal == nil && pending.pair == nil) {
		return nil, fmt.Errorf("cleanup requires qualified evidence")
	}
	protected := append(pending.workspaceControls(), pending.paths.ManifestPath, pending.paths.LockfilePath)
	protected = append(protected, commandPaths...)
	slices.Sort(protected)
	plan := &Plan{pending: pending, protected: slices.Compact(protected)}
	if err := plan.checkDisjoint(true); err != nil {
		return nil, err
	}
	if err := pending.Validate(ctx); err != nil {
		return nil, err
	}
	return plan, nil
}

func (plan *Plan) Steps() []Step {
	if plan == nil || plan.pending == nil {
		return nil
	}
	var steps []Step
	if plan.pending.journal != nil {
		steps = append(steps, Step{Kind: JournalRetirement})
	}
	if plan.pending.pair != nil {
		steps = append(steps, Step{Kind: DeclarationPair})
	}
	return steps
}

// RequireSameDeclarationSelection carries the declaration scope into a fresh
// lock plan, without preserving old publication authority.
func (plan *Plan) RequireSameDeclarationSelection(revisions mutation.RevisionSet) error {
	if plan == nil || plan.pending == nil || plan.pending.pair == nil {
		return fmt.Errorf("cleanup has no settled declaration pair")
	}
	return plan.pending.pair.RequireSameSelection(revisions)
}

var errOverlap = errors.New("recovery cleanup overlaps a declaration, input, or managed location")

func (plan *Plan) checkDisjoint(includeJournal bool) error {
	depth := physicalwork.MaximumPhysicalPathDepth
	budget := physicalwork.NewPathBudget()
	var retirements []mutation.DirectoryEntryAuthorityObservation
	if cleanup, ok := journal.JournalCleanupPlan(plan.pending.journal); ok && includeJournal {
		authority := cleanup.Authority()
		for _, name := range []string{authority.ControlName(), authority.ResidueName(), authority.GCName()} {
			path := filepath.Join(plan.pending.recoveryRoot, name)
			entry, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, depth, budget)
			if err != nil {
				return err
			}
			retirements = append(retirements, entry)
		}
		// The declaration record may not be an alias of a journal-owned slot.
		if err := rejectOverlap(plan.pending.marker, retirements, budget); err != nil {
			return err
		}
	}
	if plan.pending.pair != nil {
		entry, err := mutation.ObserveDirectoryEntryAuthorityBounded(plan.pending.marker, depth, budget)
		if err != nil {
			return err
		}
		retirements = append(retirements, entry)
	}
	// The lease store is an overlap control, not a leasable resource.
	if err := rejectOverlap(filepath.Join(plan.pending.paths.DataDir, "locks"), retirements, budget); err != nil {
		return err
	}
	for _, path := range plan.protected {
		if err := rejectOverlap(path, retirements, budget); err != nil {
			return err
		}
	}
	return nil
}

func rejectOverlap(path string, retirements []mutation.DirectoryEntryAuthorityObservation, budget *physicalwork.PathBudget) error {
	if path == "" {
		return nil
	}
	depth := physicalwork.MaximumPhysicalPathDepth
	entry, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, depth, budget)
	if err != nil {
		return err
	}
	_, referent, err := mutation.ObserveFutureReferentAuthorityBounded(path, depth, budget)
	if err != nil {
		return err
	}
	for _, retirement := range retirements {
		if retirement.MayOverlap(entry) || retirement.MayOverlap(referent) {
			return errOverlap
		}
	}
	return nil
}
