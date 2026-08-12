package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type retirementDestinations struct {
	active       rootedpath.Destination
	activeRecord rootedpath.Destination
	control      rootedpath.Destination
	record       rootedpath.Destination
	residue      rootedpath.Destination
	garbage      rootedpath.Destination
}

type retirementExecutionEvidence struct {
	active               retirementTreeEvidence
	control              retirementTreeEvidence
	residue              retirementTreeEvidence
	controlPresent       bool
	residuePresent       bool
	activeLimits         mutationfs.TreeTraversalLimits
	activeEnvelopeLimits mutationfs.TreeTraversalLimits
	controlCurrentLimits mutationfs.TreeTraversalLimits
	controlFinalLimits   mutationfs.TreeTraversalLimits
	residueLimits        mutationfs.TreeTraversalLimits
	controlCurrentWork   recovery.ArtifactWork
	controlFinalWork     recovery.ArtifactWork
}

// RetirementContinuation is one single-use, pre-effect journal-retirement
// continuation. It owns the retained recovery root, exact entry bindings,
// freshness evidence, and the disjoint execution capacity reserved for every
// later validation and cleanup pass.
type RetirementContinuation struct {
	*retirementContinuationState
}

type retirementContinuationState struct {
	mu                   sync.Mutex
	execution            retirementExecution
	evidence             retirementExecutionEvidence
	root                 *rootedpath.CapturedRoot
	rootAuthority        rootedpath.Authority
	destinations         retirementDestinations
	bindings             retirementBindings
	executionBudget      *recovery.PhysicalWorkBudget
	maximumPhysicalDepth int
	filesystem           mutationfs.RootedStore
	stateCodec           durable.SnapshotCodec
	rebased              bool
	consumed             bool
	closed               bool
}

type retirementPathReservation struct {
	budget *recovery.PhysicalWorkBudget
}

func (reservation retirementPathReservation) AdmitPathComponents(count int) error {
	return reservation.budget.ReserveRetirementPathComponents(count)
}

// PrepareActiveJournalRetirement binds and budgets the complete retirement
// tail for an active journal. Plan need not yet be clean; ExecuteActive checks
// the final post-effect classification against this immutable journal basis.
func PrepareActiveJournalRetirement(
	ctx context.Context,
	plan recovery.Plan,
	activeAuthority ActiveJournalAuthority,
	recoveryRoot string,
	maximumPhysicalDepth int,
	physicalWorkBudget *recovery.PhysicalWorkBudget,
	filesystem mutationfs.RootedStore,
	stateCodec durable.SnapshotCodec,
) (*RetirementContinuation, error) {
	if err := activeAuthority.Validate(); err != nil {
		return nil, err
	}
	if stateCodec == nil {
		return nil, fmt.Errorf("journal retirement state codec is required")
	}
	fingerprint, err := plan.JournalAuthorityFingerprint()
	if err != nil {
		return nil, fmt.Errorf("read active journal retirement identity: %w", err)
	}
	record, err := retirement.NewRecord(
		plan.OperationID(),
		fingerprint,
		retirement.PhasePrepared,
	)
	if err != nil {
		return nil, fmt.Errorf("build active journal retirement record: %w", err)
	}
	execution := retirementExecution{
		record:             record,
		activePath:         plan.OperationDir(),
		activeAuthority:    activeAuthority,
		journalFingerprint: fingerprint,
		start:              retirementStartActive,
	}
	return prepareRetirement(
		ctx,
		execution,
		recoveryRoot,
		maximumPhysicalDepth,
		physicalWorkBudget,
		filesystem,
		stateCodec,
	)
}

// PrepareJournalCleanup binds and budgets one cleanup-only continuation.
func PrepareJournalCleanup(
	ctx context.Context,
	plan retirement.CleanupPlan,
	recoveryRoot string,
	maximumPhysicalDepth int,
	physicalWorkBudget *recovery.PhysicalWorkBudget,
	filesystem mutationfs.RootedStore,
) (*RetirementContinuation, error) {
	authority := plan.Authority()
	record, err := authority.CurrentRecord()
	if err != nil {
		return nil, err
	}
	start := retirementStartFinalizingWithoutResidue
	switch {
	case authority.RequiresPhaseAdvance():
		start = retirementStartPrepared
	case authority.ResiduePresent():
		start = retirementStartFinalizingWithResidue
	}
	return prepareRetirement(
		ctx,
		retirementExecution{record: record, start: start},
		recoveryRoot,
		maximumPhysicalDepth,
		physicalWorkBudget,
		filesystem,
		nil,
	)
}

func prepareRetirement(
	ctx context.Context,
	execution retirementExecution,
	recoveryRoot string,
	maximumPhysicalDepth int,
	physicalWorkBudget *recovery.PhysicalWorkBudget,
	filesystem mutationfs.RootedStore,
	stateCodec durable.SnapshotCodec,
) (_ *RetirementContinuation, returnErr error) {
	if ctx == nil {
		return nil, fmt.Errorf("journal retirement context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !execution.valid() {
		return nil, fmt.Errorf("journal retirement execution is uninitialized")
	}
	if filesystem == nil {
		return nil, fmt.Errorf("journal retirement filesystem is required")
	}
	if physicalWorkBudget == nil {
		return nil, fmt.Errorf("journal retirement physical work budget is required")
	}
	root, err := rootedpath.CaptureRootBounded(
		recoveryRoot,
		maximumPhysicalDepth,
		physicalWorkBudget,
	)
	if err != nil {
		return nil, fmt.Errorf("capture journal retirement root: %w", err)
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, root.Close())
		}
	}()
	rootAuthority, err := root.AuthorityBounded(physicalWorkBudget)
	if err != nil {
		return nil, fmt.Errorf("read journal retirement root authority: %w", err)
	}
	destinations, err := retirementDestinationsForExecution(
		rootAuthority,
		execution,
	)
	if err != nil {
		return nil, err
	}
	planningBindings, err := bindRetirementDestinations(
		root,
		destinations,
		maximumPhysicalDepth,
		physicalWorkBudget,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, planningBindings.close())
	}()

	evidence, err := observeRetirementPreparation(
		ctx,
		execution,
		planningBindings,
		physicalWorkBudget,
		filesystem,
	)
	if err != nil {
		return nil, err
	}
	if err := reserveRetirementExecution(
		execution,
		evidence,
		root,
		destinations,
		maximumPhysicalDepth,
		physicalWorkBudget,
	); err != nil {
		return nil, err
	}
	executionBudget, err := physicalWorkBudget.BeginReservedRetirementExecution()
	if err != nil {
		return nil, err
	}
	executionBindings, err := bindRetirementDestinations(
		root,
		destinations,
		maximumPhysicalDepth,
		executionBudget,
	)
	if err != nil {
		return nil, err
	}
	return &RetirementContinuation{
		retirementContinuationState: &retirementContinuationState{
			execution:            execution,
			evidence:             evidence,
			root:                 root,
			rootAuthority:        rootAuthority,
			destinations:         destinations,
			bindings:             executionBindings,
			executionBudget:      executionBudget,
			maximumPhysicalDepth: maximumPhysicalDepth,
			filesystem:           filesystem,
			stateCodec:           stateCodec,
		},
	}, nil
}

func retirementDestinationsForExecution(
	authority rootedpath.Authority,
	execution retirementExecution,
) (retirementDestinations, error) {
	recoveryRoot := authority.PhysicalRoot()
	if execution.activePath != "" &&
		filepath.Clean(execution.activePath) != filepath.Join(recoveryRoot, execution.record.Identity().OperationID()) {
		return retirementDestinations{}, fmt.Errorf("active recovery operation directory does not match its operation id")
	}
	bind := func(path string) (rootedpath.Destination, error) {
		relativePath, err := filepath.Rel(recoveryRoot, path)
		if err != nil {
			return rootedpath.Destination{}, err
		}
		relative, err := rootedpath.NewRelativeDestination(filepath.ToSlash(relativePath))
		if err != nil {
			return rootedpath.Destination{}, err
		}
		return authority.Bind(relative)
	}
	identity := execution.record.Identity()
	destinations := retirementDestinations{}
	var err error
	if execution.activePath != "" {
		if destinations.active, err = bind(execution.activePath); err != nil {
			return retirementDestinations{}, err
		}
		if destinations.activeRecord, err = bind(filepath.Join(execution.activePath, recoveryJournalFileName)); err != nil {
			return retirementDestinations{}, err
		}
	}
	if destinations.control, err = bind(filepath.Join(recoveryRoot, identity.ControlName())); err != nil {
		return retirementDestinations{}, err
	}
	if destinations.record, err = bind(filepath.Join(recoveryRoot, identity.ControlName(), retirement.RecordFileName)); err != nil {
		return retirementDestinations{}, err
	}
	if destinations.residue, err = bind(filepath.Join(recoveryRoot, identity.ResidueName())); err != nil {
		return retirementDestinations{}, err
	}
	if destinations.garbage, err = bind(filepath.Join(recoveryRoot, identity.GCName())); err != nil {
		return retirementDestinations{}, err
	}
	return destinations, nil
}

func bindRetirementDestinations(
	root *rootedpath.CapturedRoot,
	destinations retirementDestinations,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (retirementBindings, error) {
	var bindings retirementBindings
	bind := func(destination rootedpath.Destination) (*rootedpath.EntryAuthority, error) {
		return rootedpath.BindCapturedEntryAuthorityBounded(
			root,
			destination,
			maximumPhysicalDepth,
			budget,
		)
	}
	var err error
	if destinations.active.Validate() == nil {
		bindings.active, err = bind(destinations.active)
		if err != nil {
			return retirementBindings{}, err
		}
		bindings.activeRecord, err = bind(destinations.activeRecord)
		if err != nil {
			return retirementBindings{}, errors.Join(err, bindings.close())
		}
	}
	for _, item := range []struct {
		destination rootedpath.Destination
		target      **rootedpath.EntryAuthority
	}{
		{destinations.control, &bindings.control},
		{destinations.record, &bindings.record},
		{destinations.residue, &bindings.residue},
		{destinations.garbage, &bindings.garbage},
	} {
		*item.target, err = bind(item.destination)
		if err != nil {
			return retirementBindings{}, errors.Join(err, bindings.close())
		}
	}
	return bindings, nil
}

func reserveRetirementExecution(
	execution retirementExecution,
	evidence retirementExecutionEvidence,
	root *rootedpath.CapturedRoot,
	destinations retirementDestinations,
	maximumPhysicalDepth int,
	budget *recovery.PhysicalWorkBudget,
) error {
	reservation := retirementPathReservation{budget: budget}
	reserveAccesses := func(destination rootedpath.Destination, count int) error {
		for range count {
			if err := root.ReserveDestinationAccess(
				destination,
				maximumPhysicalDepth,
				reservation,
			); err != nil {
				return err
			}
		}
		return nil
	}
	reserveCleanup := func(destination rootedpath.Destination, work recovery.ArtifactWork) error {
		validationWork, err := destination.ParentChainValidationWork()
		if err != nil {
			return err
		}
		return budget.ReserveRetirementRootedCleanup(work, validationWork)
	}

	if execution.start == retirementStartActive {
		for _, item := range []struct {
			destination rootedpath.Destination
			count       int
		}{
			{destinations.active, 3},
			{destinations.activeRecord, 1},
			{destinations.control, map[bool]int{true: 5, false: 7}[evidence.controlPresent]},
			{destinations.record, 2},
			{destinations.residue, 4},
			{destinations.garbage, 3},
		} {
			if err := reserveAccesses(item.destination, item.count); err != nil {
				return fmt.Errorf("reserve active journal retirement path work: %w", err)
			}
		}
		// If an authorized journal CAS changes the retirement identity, the
		// obsolete artifact names must be proved absent before pure rebinding.
		for _, destination := range []rootedpath.Destination{
			destinations.control,
			destinations.residue,
			destinations.garbage,
		} {
			if err := reserveAccesses(destination, 1); err != nil {
				return fmt.Errorf("reserve active journal retirement rebind work: %w", err)
			}
		}
		activeEnvelopeWork, err := recovery.NewArtifactWork(
			evidence.activeEnvelopeLimits.MaximumEntries(),
			evidence.activeEnvelopeLimits.MaximumBytes(),
		)
		if err != nil {
			return err
		}
		// One authorized basis refresh and three exact snapshots surround the
		// visibility transitions. Rooted cleanup reserves its own fixed envelope.
		if err := budget.ReserveRetirementDirectoryPasses(activeEnvelopeWork, 4); err != nil {
			return err
		}
		if err := reserveCleanup(destinations.residue, activeEnvelopeWork); err != nil {
			return err
		}
		journalWork, err := recovery.NewArtifactWork(0, maximumRecoveryJournalBytes)
		if err != nil {
			return err
		}
		if err := budget.ReserveRetirementFilePasses(journalWork, 1); err != nil {
			return err
		}
	}

	controlCurrentWork := evidence.controlCurrentWork
	if !evidence.controlPresent {
		if err := budget.ReserveRetirementArtifactWork(controlCurrentWork); err != nil {
			return err
		}
	}
	finalizing, err := execution.record.Finalizing()
	if err != nil {
		return err
	}
	controlFinalWork := evidence.controlFinalWork

	currentPasses := 0
	finalPasses := 1
	switch execution.start {
	case retirementStartActive:
		if evidence.controlPresent {
			currentPasses = 2
		} else {
			currentPasses = 1
		}
	case retirementStartPrepared:
		currentPasses = 2
	case retirementStartFinalizingWithResidue, retirementStartFinalizingWithoutResidue:
		currentPasses = 1
		finalPasses = 1
	}
	if currentPasses > 0 {
		if err := budget.ReserveRetirementDirectoryPasses(controlCurrentWork, currentPasses); err != nil {
			return err
		}
	}
	if err := budget.ReserveRetirementDirectoryPasses(controlFinalWork, finalPasses); err != nil {
		return err
	}
	if err := reserveCleanup(destinations.garbage, controlFinalWork); err != nil {
		return err
	}
	if execution.advancesPhase() {
		currentRecord, err := retirement.Encode(execution.record)
		if err != nil {
			return err
		}
		finalRecord, err := retirement.Encode(finalizing)
		if err != nil {
			return err
		}
		currentRecordWork, err := recovery.NewArtifactWork(0, int64(len(currentRecord)))
		if err != nil {
			return err
		}
		finalRecordWork, err := recovery.NewArtifactWork(0, int64(len(finalRecord)))
		if err != nil {
			return err
		}
		if err := budget.ReserveRetirementFilePasses(currentRecordWork, 1); err != nil {
			return err
		}
		if err := budget.ReserveRetirementFilePasses(finalRecordWork, 1); err != nil {
			return err
		}
	}

	if execution.start != retirementStartActive {
		controlAccesses := 4
		if execution.advancesPhase() {
			controlAccesses += 2
		}
		for _, item := range []struct {
			destination rootedpath.Destination
			count       int
		}{
			{destinations.control, controlAccesses},
			{destinations.record, map[bool]int{true: 2, false: 0}[execution.advancesPhase()]},
			{destinations.residue, map[bool]int{true: 4, false: 2}[evidence.residuePresent]},
			{destinations.garbage, 3},
		} {
			if err := reserveAccesses(item.destination, item.count); err != nil {
				return fmt.Errorf("reserve journal cleanup path work: %w", err)
			}
		}
	}
	if evidence.residuePresent {
		passes := 2
		if execution.start == retirementStartPrepared {
			passes = 3
		}
		if err := budget.ReserveRetirementDirectoryPasses(evidence.residue.work, passes); err != nil {
			return err
		}
		if err := reserveCleanup(destinations.residue, evidence.residue.work); err != nil {
			return err
		}
	}
	return nil
}

// RequireActivePlan verifies that plan names the active operation whose
// retirement capacity was reserved. The journal payload may have advanced
// through an authorized provisional-acquire CAS since initial preparation.
func (prepared *RetirementContinuation) RequireActivePlan(plan recovery.Plan) error {
	if prepared == nil || prepared.retirementContinuationState == nil {
		return fmt.Errorf("prepared journal retirement is required")
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.closed || prepared.consumed {
		return fmt.Errorf("prepared journal retirement was already consumed")
	}
	return validateActiveRetirementPlanBasis(plan, prepared.execution)
}

// AdvanceActiveAuthority follows one journal-owned directory-authority refresh.
// The caller must supply the exact previous basis, which prevents unrelated
// authority replacement from being adopted as a continuation.
func (prepared *RetirementContinuation) AdvanceActiveAuthority(
	previous ActiveJournalAuthority,
	current ActiveJournalAuthority,
) error {
	if prepared == nil || prepared.retirementContinuationState == nil {
		return fmt.Errorf("prepared journal retirement is required")
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.closed || prepared.consumed || prepared.rebased {
		return fmt.Errorf("prepared journal retirement cannot advance its active authority")
	}
	if prepared.execution.start != retirementStartActive ||
		!prepared.execution.activeAuthority.equal(previous) {
		return fmt.Errorf("prepared journal retirement authority advance has a stale basis")
	}
	if err := current.Validate(); err != nil {
		return err
	}
	prepared.execution.activeAuthority = current
	return nil
}

// AdvanceActiveBasis admits the one final active-journal basis produced by
// authorized journal CAS operations. It requires the same directory object and
// byte-identical non-record tree, then replaces only the record-dependent
// evidence used by retirement.
func (prepared *RetirementContinuation) AdvanceActiveBasis(
	ctx context.Context,
	plan recovery.Plan,
	activeAuthority ActiveJournalAuthority,
) error {
	if prepared == nil || prepared.retirementContinuationState == nil {
		return fmt.Errorf("prepared journal retirement is required")
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	return prepared.advanceActiveBasisLocked(ctx, plan, activeAuthority)
}

func (prepared *RetirementContinuation) advanceActiveBasisLocked(
	ctx context.Context,
	plan recovery.Plan,
	activeAuthority ActiveJournalAuthority,
) error {
	if prepared.closed || prepared.consumed {
		return fmt.Errorf("prepared journal retirement was already consumed")
	}
	if prepared.rebased {
		return validateActiveRetirementPlan(plan, prepared.execution)
	}
	if err := validateActiveRetirementPlanBasis(plan, prepared.execution); err != nil {
		return err
	}
	if err := activeAuthority.Validate(); err != nil {
		return err
	}
	if !prepared.execution.activeAuthority.equal(activeAuthority) {
		return fmt.Errorf("active recovery journal authority differs from its prepared continuation")
	}
	current, err := observeRetirementDirectoryWithLimits(
		ctx,
		prepared.filesystem,
		prepared.bindings.active,
		prepared.evidence.activeEnvelopeLimits,
		newRetirementTreeSnapshotSinkWithMutableFile(recoveryJournalFileName),
	)
	if err != nil {
		return fmt.Errorf("refresh active recovery journal retirement evidence: %w", err)
	}
	if !activeAuthority.matches(current.identity) {
		return fmt.Errorf("active recovery journal identity changed before retirement")
	}
	if !prepared.evidence.active.sameTreeExceptMutableFile(current) {
		return fmt.Errorf("active recovery journal non-record content changed after retirement preparation")
	}
	fingerprint, err := plan.JournalAuthorityFingerprint()
	if err != nil {
		return err
	}
	if !journalFingerprintMatchesEvidence(fingerprint, current) {
		return fmt.Errorf("active recovery journal record differs from its final retirement plan")
	}
	if prepared.evidence.controlPresent && fingerprint != prepared.execution.journalFingerprint {
		return fmt.Errorf("active recovery journal cannot advance after retirement control publication")
	}
	record, err := retirement.NewRecord(
		plan.OperationID(),
		fingerprint,
		retirement.PhasePrepared,
	)
	if err != nil {
		return err
	}
	if fingerprint != prepared.execution.journalFingerprint {
		for _, item := range []struct {
			authority *rootedpath.EntryAuthority
			label     string
		}{
			{prepared.bindings.control, "obsolete journal retirement control"},
			{prepared.bindings.residue, "obsolete journal retirement residue"},
			{prepared.bindings.garbage, "obsolete journal retirement GC residue"},
		} {
			if err := requireRetirementEntryAbsent(
				ctx,
				prepared.filesystem,
				item.authority,
				item.label,
			); err != nil {
				return err
			}
		}
		rebasedExecution := prepared.execution
		rebasedExecution.record = record
		rebasedExecution.activeAuthority = activeAuthority
		rebasedExecution.journalFingerprint = fingerprint
		destinations, err := retirementDestinationsForExecution(
			prepared.rootAuthority,
			rebasedExecution,
		)
		if err != nil {
			return err
		}
		bindings, err := bindRetirementDestinations(
			prepared.root,
			destinations,
			prepared.maximumPhysicalDepth,
			prepared.executionBudget,
		)
		if err != nil {
			return err
		}
		if err := prepared.bindings.close(); err != nil {
			return errors.Join(err, bindings.close())
		}
		prepared.destinations = destinations
		prepared.bindings = bindings
	}
	activeLimits, err := exactRetirementTreeLimits(current.work, maximumRecoveryTreeDepth)
	if err != nil {
		return err
	}
	prepared.execution.record = record
	prepared.execution.activeAuthority = activeAuthority
	prepared.execution.journalFingerprint = fingerprint
	prepared.evidence.active = current
	prepared.evidence.activeLimits = activeLimits
	prepared.rebased = true
	return nil
}

// ExecuteActive consumes this continuation for the same journal after effects
// have converged to a clean plan.
func (prepared *RetirementContinuation) ExecuteActive(
	ctx context.Context,
	plan recovery.Plan,
) error {
	if prepared == nil || prepared.retirementContinuationState == nil {
		return fmt.Errorf("prepared journal retirement is required")
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.closed || prepared.consumed {
		return fmt.Errorf("prepared journal retirement was already consumed")
	}
	if prepared.execution.start != retirementStartActive {
		return fmt.Errorf("prepared journal retirement does not own an active journal")
	}
	if !prepared.rebased {
		if err := prepared.advanceActiveBasisLocked(
			ctx,
			plan,
			prepared.execution.activeAuthority,
		); err != nil {
			return err
		}
	}
	if err := validateActiveRetirementPlan(plan, prepared.execution); err != nil {
		return err
	}
	prepared.consumed = true
	return executePreparedRetirement(ctx, prepared)
}

// ExecuteCleanup consumes this continuation for the same cleanup-only plan.
func (prepared *RetirementContinuation) ExecuteCleanup(
	ctx context.Context,
	plan retirement.CleanupPlan,
) error {
	if prepared == nil || prepared.retirementContinuationState == nil {
		return fmt.Errorf("prepared journal cleanup is required")
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.closed || prepared.consumed {
		return fmt.Errorf("prepared journal cleanup was already consumed")
	}
	authority := plan.Authority()
	record, err := authority.CurrentRecord()
	if err != nil {
		return err
	}
	if !record.Equal(prepared.execution.record) ||
		authority.ResiduePresent() != prepared.evidence.residuePresent {
		return fmt.Errorf("journal cleanup authority changed after preparation")
	}
	prepared.consumed = true
	return executePreparedRetirement(ctx, prepared)
}

func validateActiveRetirementPlan(plan recovery.Plan, execution retirementExecution) error {
	if plan.Blocked() || plan.HasErrors() {
		return fmt.Errorf("active recovery plan is not clean enough to retire")
	}
	if classification := plan.Classification(); classification != recovery.ClassificationCleanBefore &&
		classification != recovery.ClassificationCleanAfter {
		return fmt.Errorf("active recovery plan classification %q cannot be retired", classification)
	}
	if err := validateActiveRetirementPlanBasis(plan, execution); err != nil {
		return err
	}
	fingerprint, err := plan.JournalAuthorityFingerprint()
	if err != nil {
		return err
	}
	if fingerprint != execution.journalFingerprint {
		return fmt.Errorf("active recovery journal authority changed after retirement preparation")
	}
	return nil
}

func validateActiveRetirementPlanBasis(
	plan recovery.Plan,
	execution retirementExecution,
) error {
	if execution.start != retirementStartActive {
		return fmt.Errorf("prepared journal retirement does not own an active journal")
	}
	if plan.OperationID() != execution.record.Identity().OperationID() ||
		plan.OperationDir() != execution.activePath {
		return fmt.Errorf("active recovery operation changed after retirement preparation")
	}
	return nil
}

// Close releases the retained root and every exact entry binding.
func (prepared *RetirementContinuation) Close() error {
	if prepared == nil || prepared.retirementContinuationState == nil {
		return nil
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.closed {
		return nil
	}
	prepared.closed = true
	err := prepared.bindings.close()
	err = errors.Join(err, prepared.root.Close())
	prepared.root = nil
	prepared.filesystem = nil
	prepared.stateCodec = nil
	return err
}
