package execute

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

type removalRelationKey struct {
	scope       target.Scope
	destination output.Destination
}

// removalCleanupError keeps retirement failures typed and truthful without
// copying machine-local residue paths into CLI-facing error strings.
type removalCleanupError struct {
	destination  output.Destination
	readiness    recovery.RemovalCleanupReadiness
	reason       recovery.RemovalCleanupReason
	detail       string
	outcome      mutationfs.CommitOutcome
	outcomeKnown bool
	cause        error
}

func (failure *removalCleanupError) Error() string {
	if failure == nil {
		return "removal cleanup failed"
	}
	message := fmt.Sprintf(
		"removal cleanup for %q is %s (%s): %s",
		failure.destination,
		failure.readiness,
		failure.reason,
		failure.detail,
	)
	if failure.outcomeKnown {
		message += fmt.Sprintf(" (storage outcome: %s)", failure.outcome.State())
	}
	return message
}

func (failure *removalCleanupError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func (authority *mutationAuthority) prepareRemovalDemands(
	demands recovery.RemovalDemandSet,
	budget *recovery.PhysicalWorkBudget,
) error {
	if authority == nil {
		return fmt.Errorf("removal mutation authority is unavailable")
	}
	if authority.removalBindingsPrepared {
		if !authority.removalDemands.Equal(demands) {
			return fmt.Errorf("removal demand authority changed after physical binding")
		}
		return nil
	}
	if err := demands.Validate(); err != nil {
		return fmt.Errorf("removal demands: %w", err)
	}
	if budget == nil {
		return fmt.Errorf("removal operation work budget is required")
	}
	destinations := make(map[removalRelationKey]mutationDestination, demands.Len())
	for index, demand := range demands.Demands() {
		key := removalRelationKey{scope: demand.Scope(), destination: demand.Destination()}
		if _, duplicate := destinations[key]; duplicate {
			return fmt.Errorf("removal demands contain duplicate relation %q", demand.Destination())
		}
		var destination mutationDestination
		var err error
		switch demand.Scope() {
		case target.ScopeProject:
			destination, err = authority.resolveProject(demand.Destination())
			if err == nil {
				err = journal.ChargeRemovalPathWork(budget, destination.hostPath)
			}
		case target.ScopeGlobal:
			err = authority.bindGlobalDestination(demand.Destination())
			if err == nil {
				destination, err = authority.resolveBoundDestination(
					demand.Scope(),
					demand.Destination(),
				)
			}
		default:
			err = fmt.Errorf("mutation scope %q is unsupported", demand.Scope())
		}
		if err != nil {
			return fmt.Errorf("removal demand[%d] destination authority: %w", index, err)
		}
		destinations[key] = destination
	}
	authority.removalDemands = demands
	authority.removalDestinations = destinations
	authority.physicalWorkBudget = budget
	authority.removalBindingsPrepared = true
	return nil
}

func (authority *mutationAuthority) bindRemovalIntents(plan recovery.Plan) error {
	if authority == nil {
		return fmt.Errorf("removal mutation authority is unavailable")
	}
	intents := plan.RemovalIntents()
	if authority.removalAuthorityBound {
		return authority.validateReloadedRemovalIntents(intents)
	}
	if !authority.removalBindingsPrepared || authority.physicalWorkBudget == nil {
		return fmt.Errorf("removal physical bindings were not prepared before journal authority")
	}
	demands, err := removalDemandSetFromIntents(intents)
	if err != nil {
		return err
	}
	if !authority.removalDemands.Equal(demands) {
		return fmt.Errorf("captured removal intents do not match the prepared demand set")
	}
	next := make(map[removalRelationKey]recovery.RemovalIntent, len(intents))
	for index, intent := range intents {
		if err := intent.Validate(); err != nil {
			return fmt.Errorf("removal intent[%d]: %w", index, err)
		}
		key := removalRelationKey{scope: intent.Scope(), destination: intent.Destination()}
		if _, duplicate := next[key]; duplicate {
			return fmt.Errorf("removal authority contains duplicate relation %q", intent.Destination())
		}
		if _, bound := authority.removalDestinations[key]; !bound {
			return fmt.Errorf("removal relation %q has no prepared physical binding", intent.Destination())
		}
		next[key] = intent
	}
	authority.removalIntents = next
	authority.removalAuthorityBound = true
	return nil
}

func removalDemandSetFromIntents(
	intents []recovery.RemovalIntent,
) (recovery.RemovalDemandSet, error) {
	demands := make([]recovery.RemovalDemand, 0, len(intents))
	for index, intent := range intents {
		if err := intent.Validate(); err != nil {
			return recovery.RemovalDemandSet{}, fmt.Errorf("removal intent[%d]: %w", index, err)
		}
		demands = append(demands, intent.Demand())
	}
	return recovery.NewRemovalDemandSet(demands)
}

func (authority *mutationAuthority) validateReloadedRemovalIntents(
	intents []recovery.RemovalIntent,
) error {
	if len(intents) != len(authority.removalIntents) {
		return fmt.Errorf("reloaded removal authority changed intent cardinality")
	}
	for index, intent := range intents {
		if err := intent.Validate(); err != nil {
			return fmt.Errorf("removal intent[%d]: %w", index, err)
		}
		key := removalRelationKey{scope: intent.Scope(), destination: intent.Destination()}
		previous, present := authority.removalIntents[key]
		if !present || !previous.Equal(intent) {
			return fmt.Errorf("reloaded removal authority changed relation %q", intent.Destination())
		}
	}
	if authority.physicalWorkBudget == nil {
		return fmt.Errorf("reloaded removal authority has no operation work budget")
	}
	if len(authority.removalDestinations) != len(authority.removalIntents) {
		return fmt.Errorf("reloaded removal authority has incomplete physical bindings")
	}
	for key, destination := range authority.removalDestinations {
		if _, present := authority.removalIntents[key]; !present || !destination.isRooted() {
			return fmt.Errorf("reloaded removal authority has an invalid physical binding")
		}
	}
	return nil
}

func (authority *mutationAuthority) removalIntentFor(
	scope target.Scope,
	destination output.Destination,
) (recovery.RemovalIntent, error) {
	if authority == nil || !authority.removalAuthorityBound {
		return recovery.RemovalIntent{}, fmt.Errorf("complete removal authority is not bound before rooted removal")
	}
	intent, present := authority.removalIntents[removalRelationKey{scope: scope, destination: destination}]
	if !present {
		return recovery.RemovalIntent{}, fmt.Errorf("journalized rooted removal %q has no exact removal intent", destination)
	}
	return intent, nil
}

func (authority *mutationAuthority) removalDestinationFor(
	scope target.Scope,
	destination output.Destination,
) (mutationDestination, error) {
	if authority == nil || !authority.removalAuthorityBound {
		return mutationDestination{}, fmt.Errorf("complete removal authority is not bound before rooted removal")
	}
	bound, present := authority.removalDestinations[removalRelationKey{scope: scope, destination: destination}]
	if !present || !bound.isRooted() {
		return mutationDestination{}, fmt.Errorf("journalized rooted removal %q has no physical namespace binding", destination)
	}
	return bound, nil
}

func (authority *mutationAuthority) removeJournaledRootedEntry(
	ctx context.Context,
	destination mutationDestination,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, error) {
	if !destination.isRooted() {
		return uncommittedRemovalFailure(fmt.Errorf("mutation destination is invalid"))
	}
	intent, err := authority.removalIntentFor(destination.scope, destination.logical)
	if err != nil {
		if capability != nil {
			_ = capability.Close()
		}
		return uncommittedRemovalFailure(err)
	}
	budget := authority.forwardRemovalExecution
	if budget == nil {
		if capability != nil {
			_ = capability.Close()
		}
		return uncommittedRemovalFailure(fmt.Errorf("forward removal execution budget is unavailable"))
	}
	if err := budget.AdmitObservation(); err != nil {
		if capability != nil {
			_ = capability.Close()
		}
		return uncommittedRemovalFailure(err)
	}
	removalDestination, err := authority.removalDestinationFor(destination.scope, destination.logical)
	if err != nil {
		if capability != nil {
			_ = capability.Close()
		}
		return uncommittedRemovalFailure(err)
	}
	if filepath.Clean(destination.hostPath) != filepath.Clean(removalDestination.hostPath) {
		if capability != nil {
			_ = capability.Close()
		}
		return uncommittedRemovalFailure(fmt.Errorf(
			"rooted removal capability and namespace authority identify different destinations",
		))
	}
	if capability == nil {
		return uncommittedRemovalFailure(fmt.Errorf("forward removal capability is required"))
	}
	if err := capability.Close(); err != nil {
		return uncommittedRemovalFailure(fmt.Errorf("close pre-removal capability: %w", err))
	}
	capability, err = removalDestination.root.AcquireBounded(
		removalDestination.destination,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return uncommittedRemovalFailure(fmt.Errorf("acquire bounded forward removal capability: %w", err))
	}
	if err := validateRemovalNamespace(ctx, removalDestination, intent.Namespace(), budget); err != nil {
		if capability != nil {
			_ = capability.Close()
		}
		return uncommittedRemovalFailure(err)
	}
	limits, err := authority.beginForwardRemoval(
		ctx,
		removalDestination,
		capability,
		expected,
		intent,
	)
	if err != nil {
		if capability != nil {
			_ = capability.Close()
		}
		return uncommittedRemovalFailure(err)
	}
	outcome, err := authority.filesystem.RemoveRootedEntryWithResidue(
		ctx,
		capability,
		expected,
		intent.Namespace().Names(),
		limits,
	)
	if err != nil {
		return outcome, &rootedRemovalCommitError{outcome: outcome, cause: err}
	}
	return outcome, nil
}

// rootedRemovalCommitError preserves the storage conclusion across the
// execute boundary. The storage layer owns the outcome; execute only uses it
// to choose truthful rollback progress and diagnostics.
type rootedRemovalCommitError struct {
	outcome mutationfs.CommitOutcome
	cause   error
}

func (failure *rootedRemovalCommitError) Error() string {
	if failure == nil {
		return "rooted removal commit failed"
	}
	return fmt.Sprintf("rooted removal commit outcome %s", failure.outcome.State())
}

func uncommittedRemovalFailure(cause error) (mutationfs.CommitOutcome, error) {
	outcome, err := mutationfs.NewCommitOutcome(mutationfs.CommitOutcomeUncommitted, nil)
	if err != nil {
		return mutationfs.CommitOutcome{}, &rootedRemovalCommitError{cause: cause}
	}
	return outcome, &rootedRemovalCommitError{outcome: outcome, cause: cause}
}

func (failure *rootedRemovalCommitError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func (failure *rootedRemovalCommitError) Outcome() mutationfs.CommitOutcome {
	if failure == nil {
		return mutationfs.CommitOutcome{}
	}
	return failure.outcome
}

func validateRemovalNamespace(
	ctx context.Context,
	destination mutationDestination,
	namespace recovery.RemovalNamespaceAuthority,
	budget *recovery.PhysicalWorkBudget,
) error {
	observation, err := journal.ObserveRemovalNamespace(
		ctx,
		destination.root,
		destination.destination,
		namespace,
		budget,
	)
	if err != nil {
		return err
	}
	if observation.Status() != recovery.RemovalNamespaceMatched {
		return fmt.Errorf("removal namespace is %s: %s", observation.Status(), observation.Detail())
	}
	return nil
}

type removalCleanupCandidate struct {
	intent               recovery.RemovalIntent
	destination          mutationDestination
	residuePath          string
	cleanupPath          string
	obligation           recovery.RemovalCleanupObligation
	budget               *recovery.PhysicalWorkBudget
	residueWork          recovery.ArtifactWork
	cleanupSlotWork      recovery.ArtifactWork
	cleanupWork          recovery.ArtifactWork
	executionPreflighted bool
}

type removalSlotObservation struct {
	entry    recovery.RemovalResidueEntryObservation
	identity mutationfs.EntryIdentity
	work     recovery.ArtifactWork
}

// cleanupRemovalResidues performs a complete read-only retirement preflight
// before executing any removal-slot action. This prevents one blocked intent from
// being discovered after another intent has already been mutated.
func (authority *mutationAuthority) cleanupRemovalResidues(
	ctx context.Context,
	plan recovery.Plan,
) ([]recovery.RemovalCleanupObligation, error) {
	intents := plan.RemovalIntents()
	budget := authority.physicalWorkBudget
	if authority.generalExecutionWorkBudget != nil {
		budget = authority.removalCleanupExecution
	}
	if budget == nil {
		return nil, fmt.Errorf("removal operation work budget is unavailable")
	}
	candidates := make([]removalCleanupCandidate, 0, len(intents))
	for index, intent := range intents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		destination, err := authority.removalDestinationFor(intent.Scope(), intent.Destination())
		if err != nil {
			obligation, obligationErr := unavailableRemovalObligation(intent, "destination authority could not be resolved")
			if obligationErr != nil {
				return nil, obligationErr
			}
			return nil, newRemovalCleanupError(obligation, err)
		}
		if err := budget.AdmitObservation(); err != nil {
			return nil, err
		}
		namespace, err := journal.ObserveRemovalNamespace(
			ctx,
			destination.root,
			destination.destination,
			intent.Namespace(),
			budget,
		)
		if err != nil {
			return nil, fmt.Errorf("observe removal intent[%d] namespace contract: %w", index, err)
		}
		residuePath, cleanupPath, err := journal.RemovalNamespacePaths(intent.Namespace())
		if err != nil {
			return nil, fmt.Errorf("derive removal intent[%d] namespace paths: %w", index, err)
		}
		residue, err := unavailableRemovalSlotObservation("residue was not observed")
		if err != nil {
			return nil, err
		}
		cleanup, err := unavailableRemovalSlotObservation("cleanup stage was not observed")
		if err != nil {
			return nil, err
		}
		if namespace.Status() == recovery.RemovalNamespaceMatched {
			residue, err = authority.observeRemovalSlot(
				ctx,
				destination,
				residuePath,
				"residue",
				budget,
				budget.RemainingTreeWork(),
			)
			if err != nil {
				return nil, err
			}
			cleanup, err = authority.observeRemovalSlot(
				ctx,
				destination,
				cleanupPath,
				"cleanup stage",
				budget,
				budget.RemainingTreeWork(),
			)
			if err != nil {
				return nil, err
			}
		}
		obligation, err := intent.AssessCleanup(
			namespace,
			recovery.NewRemovalResidueObservation(residue.entry, cleanup.entry),
		)
		if err != nil {
			return nil, fmt.Errorf("assess removal intent[%d] cleanup: %w", index, err)
		}
		cleanupWork := cleanup.work
		cleanupRecursive := cleanup.entry.Kind() == recovery.PathKindDirectory
		if obligation.Action() == recovery.RemovalCleanupActionPromoteResidue {
			cleanupWork = residue.work
			cleanupRecursive = residue.entry.Kind() == recovery.PathKindDirectory
		}
		if obligation.Readiness() == recovery.RemovalCleanupReady {
			if err := journal.ReserveRemovalExecutionObservationWork(
				budget,
				destination.hostPath,
				residuePath,
				cleanupPath,
			); err != nil {
				return nil, fmt.Errorf("reserve removal intent[%d] execution observations: %w", index, err)
			}
			var reserveErr error
			if cleanupRecursive {
				reserveErr = budget.ReserveDirectoryReobservation(cleanupWork)
			} else {
				reserveErr = budget.ReserveReobservation(cleanupWork)
			}
			if reserveErr != nil {
				return nil, fmt.Errorf("reserve removal intent[%d] reobservation work: %w", index, reserveErr)
			}
			if cleanupRecursive {
				if err := budget.ReserveDirectoryCleanup(cleanupWork); err != nil {
					return nil, fmt.Errorf("reserve removal intent[%d] directory cleanup work: %w", index, err)
				}
			}
		}
		candidates = append(candidates, removalCleanupCandidate{
			intent: intent, destination: destination,
			residuePath: residuePath, cleanupPath: cleanupPath,
			obligation: obligation, budget: budget,
			residueWork: residue.work, cleanupSlotWork: cleanup.work,
			cleanupWork:          cleanupWork,
			executionPreflighted: obligation.Readiness() == recovery.RemovalCleanupReady,
		})
	}

	for _, candidate := range candidates {
		if candidate.obligation.Readiness() != recovery.RemovalCleanupReady {
			return nil, newRemovalCleanupError(candidate.obligation, nil)
		}
	}
	executionBudget, err := budget.BeginReservedExecution()
	if err != nil {
		return nil, err
	}
	for index := range candidates {
		candidates[index].budget = executionBudget
	}

	discharged := make([]recovery.RemovalCleanupObligation, 0, len(candidates))
	for index, candidate := range candidates {
		obligation, err := authority.executeRemovalCleanupCandidate(ctx, candidate)
		if err != nil {
			return nil, fmt.Errorf("reconcile removal intent[%d]: %w", index, err)
		}
		discharged = append(discharged, obligation)
	}
	if !plan.RetirementReady(discharged) {
		return nil, fmt.Errorf("complete removal cleanup authority was not discharged")
	}
	return discharged, nil
}

func (authority *mutationAuthority) prepareRecoveryRemovalCleanup(
	plan recovery.Plan,
) error {
	if authority == nil || authority.physicalWorkBudget == nil {
		return fmt.Errorf("removal cleanup operation budget is unavailable")
	}
	if authority.removalCleanupExecution != nil {
		return fmt.Errorf("removal cleanup lifecycle was already prepared")
	}
	for index, intent := range plan.RemovalIntents() {
		destination, err := authority.removalDestinationFor(intent.Scope(), intent.Destination())
		if err != nil {
			return fmt.Errorf("reserve removal intent[%d] cleanup destination: %w", index, err)
		}
		residuePath, cleanupPath, err := journal.RemovalNamespacePaths(intent.Namespace())
		if err != nil {
			return fmt.Errorf("reserve removal intent[%d] cleanup namespace: %w", index, err)
		}
		if err := journal.ReserveRemovalCleanupLifecycleWork(
			authority.physicalWorkBudget,
			destination.hostPath,
			residuePath,
			cleanupPath,
		); err != nil {
			return fmt.Errorf("reserve removal intent[%d] cleanup lifecycle: %w", index, err)
		}
	}
	execution, err := authority.physicalWorkBudget.BeginReservedCleanupLifecycle()
	if err != nil {
		return err
	}
	authority.removalCleanupExecution = execution
	return nil
}

func unavailableRemovalObligation(
	intent recovery.RemovalIntent,
	detail string,
) (recovery.RemovalCleanupObligation, error) {
	namespace, err := recovery.NewRemovalNamespaceObservation(
		recovery.RemovalNamespaceUnavailable,
		detail,
	)
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	entries, err := unavailableRemovalSlots()
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	return intent.AssessCleanup(
		namespace,
		entries,
	)
}

func unavailableRemovalEntryObservation(detail string) (recovery.RemovalResidueEntryObservation, error) {
	return recovery.NewRemovalResidueEntryObservation(
		recovery.RemovalResidueEntryUnavailable,
		"", "", nil, "", detail,
	)
}

func unavailableRemovalSlotObservation(detail string) (removalSlotObservation, error) {
	observation, err := unavailableRemovalEntryObservation(detail)
	if err != nil {
		return removalSlotObservation{}, err
	}
	return removalSlotObservation{entry: observation}, nil
}

func unavailableRemovalSlots() (recovery.RemovalResidueObservation, error) {
	residue, err := unavailableRemovalEntryObservation("residue was not observed")
	if err != nil {
		return recovery.RemovalResidueObservation{}, err
	}
	cleanup, err := unavailableRemovalEntryObservation("cleanup stage was not observed")
	if err != nil {
		return recovery.RemovalResidueObservation{}, err
	}
	return recovery.NewRemovalResidueObservation(residue, cleanup), nil
}

func (authority *mutationAuthority) observeRemovalSlot(
	ctx context.Context,
	destination mutationDestination,
	path string,
	role string,
	budget *recovery.PhysicalWorkBudget,
	maximumWork recovery.ArtifactWork,
) (removalSlotObservation, error) {
	capability, err := authority.acquireRemovalSlot(destination, path, budget)
	if err != nil {
		return unavailableRemovalSlotObservation(role + " authority could not be bound")
	}
	entry, identity, work, observeErr := journal.ObserveRootedRemovalEntry(
		ctx,
		authority.filesystem,
		capability,
		budget,
		maximumWork,
	)
	closeErr := capability.Close()
	if errors.Is(observeErr, context.Canceled) || errors.Is(observeErr, context.DeadlineExceeded) {
		return removalSlotObservation{}, errors.Join(observeErr, closeErr)
	}
	if observeErr != nil || closeErr != nil {
		return unavailableRemovalSlotObservation(role + " could not be observed")
	}
	return removalSlotObservation{entry: entry, identity: identity, work: work}, nil
}

func newRemovalCleanupError(
	obligation recovery.RemovalCleanupObligation,
	cause error,
) error {
	return newRemovalCleanupErrorWithOutcome(obligation, cause, mutationfs.CommitOutcome{}, false)
}

func newRemovalCleanupErrorWithOutcome(
	obligation recovery.RemovalCleanupObligation,
	cause error,
	outcome mutationfs.CommitOutcome,
	known bool,
) error {
	readiness := obligation.Readiness()
	if readiness == recovery.RemovalCleanupReady || readiness == recovery.RemovalCleanupPending {
		readiness = recovery.RemovalCleanupRetry
	}
	reason := obligation.Reason()
	detail := obligation.Detail()
	if reason == recovery.RemovalCleanupReasonNone {
		reason = recovery.RemovalCleanupReasonActionFailed
		detail = fmt.Sprintf("cleanup action %q did not complete", obligation.Action())
	} else if detail == "" {
		detail = "cleanup obligation could not be discharged"
	}
	return &removalCleanupError{
		destination:  obligation.Destination(),
		readiness:    readiness,
		reason:       reason,
		detail:       detail,
		outcome:      outcome,
		outcomeKnown: known,
		cause:        cause,
	}
}

func (authority *mutationAuthority) executeRemovalCleanupCandidate(
	ctx context.Context,
	candidate removalCleanupCandidate,
) (recovery.RemovalCleanupObligation, error) {
	if candidate.budget == nil || !candidate.executionPreflighted {
		return recovery.RemovalCleanupObligation{}, fmt.Errorf(
			"removal cleanup candidate lacks complete operation preflight",
		)
	}
	budget := candidate.budget
	if err := budget.AdmitObservation(); err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	namespace, err := journal.ObserveRemovalNamespace(
		ctx,
		candidate.destination.root,
		candidate.destination.destination,
		candidate.intent.Namespace(),
		budget,
	)
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	if namespace.Status() != recovery.RemovalNamespaceMatched {
		entries, entriesErr := unavailableRemovalSlots()
		if entriesErr != nil {
			return recovery.RemovalCleanupObligation{}, entriesErr
		}
		obligation, obligationErr := candidate.intent.AssessCleanup(
			namespace,
			entries,
		)
		if obligationErr != nil {
			return recovery.RemovalCleanupObligation{}, obligationErr
		}
		return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, nil)
	}
	residue, err := authority.observeRemovalSlot(
		ctx,
		candidate.destination,
		candidate.residuePath,
		"residue",
		budget,
		candidate.residueWork,
	)
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	cleanup, err := authority.observeRemovalSlot(
		ctx,
		candidate.destination,
		candidate.cleanupPath,
		"cleanup stage",
		budget,
		candidate.cleanupSlotWork,
	)
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	obligation, err := candidate.intent.AssessCleanup(
		namespace,
		recovery.NewRemovalResidueObservation(residue.entry, cleanup.entry),
	)
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	switch obligation.Readiness() {
	case recovery.RemovalCleanupBlocked, recovery.RemovalCleanupRetry:
		return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, nil)
	case recovery.RemovalCleanupReady:
	default:
		return recovery.RemovalCleanupObligation{}, fmt.Errorf("removal cleanup candidate is not actionable")
	}

	switch obligation.Action() {
	case recovery.RemovalCleanupActionConfirmAbsence:
		if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
			return recovery.RemovalCleanupObligation{}, err
		}
		if err := authority.confirmRemovalSlotsAbsent(ctx, candidate, obligation); err != nil {
			return recovery.RemovalCleanupObligation{}, err
		}
	case recovery.RemovalCleanupActionPromoteResidue:
		if residue.identity == nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, nil)
		}
		capability, acquireErr := authority.acquireRemovalSlot(
			candidate.destination,
			candidate.residuePath,
			candidate.budget,
		)
		if acquireErr != nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, acquireErr)
		}
		if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
			_ = capability.Close()
			return recovery.RemovalCleanupObligation{}, err
		}
		outcome, movedIdentity, renameErr := authority.filesystem.PromoteRootedRemovalResidue(
			ctx,
			capability,
			residue.identity,
			candidate.intent.Namespace().Names(),
		)
		if renameErr != nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupErrorWithOutcome(obligation, renameErr, outcome, true)
		}
		if err := authority.cleanupRemovalProgress(ctx, candidate, obligation, movedIdentity); err != nil {
			return recovery.RemovalCleanupObligation{}, err
		}
	case recovery.RemovalCleanupActionCleanupProgress:
		if cleanup.identity == nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, nil)
		}
		capability, acquireErr := authority.acquireRemovalSlot(
			candidate.destination,
			candidate.cleanupPath,
			candidate.budget,
		)
		if acquireErr != nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, acquireErr)
		}
		limits, limitErr := removalCleanupTraversalLimits(candidate.cleanupWork)
		if limitErr != nil {
			_ = capability.Close()
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupError(obligation, limitErr)
		}
		if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
			_ = capability.Close()
			return recovery.RemovalCleanupObligation{}, err
		}
		outcome, cleanupErr := authority.filesystem.CleanupRootedRemovalStage(
			ctx,
			capability,
			cleanup.identity,
			candidate.intent.Namespace().Names(),
			limits,
		)
		if cleanupErr != nil {
			return recovery.RemovalCleanupObligation{}, newRemovalCleanupErrorWithOutcome(obligation, cleanupErr, outcome, true)
		}
		if err := authority.confirmRemovalSlotsAbsent(ctx, candidate, obligation); err != nil {
			return recovery.RemovalCleanupObligation{}, err
		}
	default:
		return recovery.RemovalCleanupObligation{}, fmt.Errorf("removal cleanup action is unavailable")
	}
	return obligation.Discharge()
}

func (authority *mutationAuthority) cleanupRemovalProgress(
	ctx context.Context,
	candidate removalCleanupCandidate,
	obligation recovery.RemovalCleanupObligation,
	identity mutationfs.EntryIdentity,
) error {
	capability, err := authority.acquireRemovalSlot(
		candidate.destination,
		candidate.cleanupPath,
		candidate.budget,
	)
	if err != nil {
		return newRemovalCleanupError(obligation, err)
	}
	limits, limitErr := removalCleanupTraversalLimits(candidate.cleanupWork)
	if limitErr != nil {
		_ = capability.Close()
		return newRemovalCleanupError(obligation, limitErr)
	}
	if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
		_ = capability.Close()
		return err
	}
	outcome, err := authority.filesystem.CleanupRootedRemovalStage(
		ctx,
		capability,
		identity,
		candidate.intent.Namespace().Names(),
		limits,
	)
	if err != nil {
		return newRemovalCleanupErrorWithOutcome(obligation, err, outcome, true)
	}
	return authority.confirmRemovalSlotsAbsent(ctx, candidate, obligation)
}

func (authority *mutationAuthority) confirmRemovalSlotsAbsent(
	ctx context.Context,
	candidate removalCleanupCandidate,
	obligation recovery.RemovalCleanupObligation,
) error {
	for _, path := range []string{candidate.residuePath, candidate.cleanupPath} {
		for range mutationfs.RootedAbsencePathObservationCount {
			if err := candidate.budget.AdmitObservation(); err != nil {
				return newRemovalCleanupError(obligation, err)
			}
		}
		capability, err := authority.acquireRemovalSlot(candidate.destination, path, candidate.budget)
		if err != nil {
			return newRemovalCleanupError(obligation, err)
		}
		outcome, err := authority.filesystem.ConfirmRootedEntryAbsent(ctx, capability)
		if err != nil {
			return newRemovalCleanupErrorWithOutcome(obligation, err, outcome, true)
		}
	}
	return nil
}

func removalCleanupTraversalLimits(
	work recovery.ArtifactWork,
) (mutationfs.TreeTraversalLimits, error) {
	return mutationfs.NewTreeTraversalLimits(
		work.Entries(),
		recovery.MaximumArtifactTreeDepth,
		work.Bytes(),
	)
}

func (authority *mutationAuthority) acquireRemovalSlot(
	destination mutationDestination,
	slotPath string,
	budget *recovery.PhysicalWorkBudget,
) (rootedpath.CommitCapability, error) {
	if authority == nil || destination.root == nil {
		return nil, fmt.Errorf("removal slot root authority is unavailable")
	}
	rootAuthority, err := destination.root.AuthorityBounded(budget)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(rootAuthority.PhysicalRoot(), filepath.Clean(slotPath))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("removal slot path %q escaped destination root", slotPath)
	}
	rootRelative, err := rootedpath.NewRelativeDestination(filepath.ToSlash(relative))
	if err != nil {
		return nil, err
	}
	bound, err := rootAuthority.Bind(rootRelative)
	if err != nil {
		return nil, err
	}
	return destination.root.AcquireBounded(
		bound,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
}
