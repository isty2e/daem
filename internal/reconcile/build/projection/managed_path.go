package projection

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
)

// ManagedPathInput contains locked desired facts, fresh evidence, and durable
// managed authority. It performs no observation or mutation.
type ManagedPathInput struct {
	Locked                 lock.LockedSection
	Expectations           []ManagedPathExpectation
	SelectedTargets        reconcile.SelectedTargets
	SupplyObservations     []observe.ExactSupplyObservation
	States                 []durable.ManagedPathState
	Evidence               []observe.ManagedPathEvidence
	ManageUnmanagedMatches bool
	Owner                  ownership.OwnerAuthority
	Ownership              []observe.OwnershipObservation
}

// BuildManagedPathDecisions reconciles every selected entity-backed path
// projection and every selected managed path state row.
func BuildManagedPathDecisions(input ManagedPathInput) ([]reconcile.ManagedPathDecision, error) {
	selection := managedPathSelection(input.SelectedTargets)
	states, err := managedPathStateIndex(input.States)
	if err != nil {
		return nil, err
	}
	evidence, err := managedPathEvidenceIndex(input.Evidence)
	if err != nil {
		return nil, err
	}
	lockEvidence, err := managedPathSupplyObservationIndex(input.SupplyObservations)
	if err != nil {
		return nil, err
	}
	ownershipEvidence, ownershipConflicts, err := ownershipObservations(input.Ownership)
	if err != nil {
		return nil, err
	}

	canonicalExpectations := append([]ManagedPathExpectation(nil), input.Expectations...)
	sort.Slice(canonicalExpectations, func(left int, right int) bool {
		return topology.CompareSubjectID(canonicalExpectations[left].subject(), canonicalExpectations[right].subject()) < 0
	})
	expectations, err := managedPathExpectationIndex(canonicalExpectations)
	if err != nil {
		return nil, err
	}
	addressConflicts := managedPathAddressConflicts(canonicalExpectations, states)
	desiredSubjects := make(map[topology.SubjectID]struct{}, len(expectations))
	decisions := make([]managedPathDecision, 0, len(expectations)+input.Locked.Len()+len(states))
	for _, expectation := range canonicalExpectations {
		facts := expectation.decisionInput()
		selectedConsumers := selectedManagedPathConsumers(facts.ConsumerTargets, selection)
		if len(selectedConsumers) == 0 {
			continue
		}
		desiredSubjects[facts.Subject] = struct{}{}
		facts.ConsumerTargets = selectedConsumers
		if _, conflict := addressConflicts[managedPathAddressKey{scope: facts.Scope, destination: facts.Destination}]; conflict {
			decisions = append(decisions, newManagedPathBlocked(
				facts,
				reconcile.ReasonDestinationConflict,
				"managed destination is owned by another projection subject",
			))
			continue
		}
		state, hasState := states[facts.Subject]
		consumers := selectedConsumers
		if hasState {
			consumers = mergeManagedPathConsumers(consumers, unselectedManagedPathConsumers(state.ConsumerTargets(), selection))
		}
		facts.ConsumerTargets = consumers
		contract, locked := input.Locked.Subject(facts.Subject)
		if !locked {
			decisions = append(decisions, newManagedPathBlocked(facts, reconcile.ReasonMissingLock, "expected managed path projection is absent from lock"))
			continue
		}
		if !expectation.matches(contract) {
			decisions = append(decisions, newManagedPathBlocked(facts, reconcile.ReasonStaleLock, "locked managed path projection does not match manifest placement"))
			continue
		}
		supply, ok := input.Locked.ExactSupplySubject(contract.EntityID())
		if !ok {
			return nil, fmt.Errorf("managed path subject %q has no correlated exact Supply", contract.SubjectID())
		}
		exact, ok := supply.ExactSupply()
		if !ok {
			return nil, fmt.Errorf("managed path subject %q exact Supply identity is missing", contract.SubjectID())
		}
		facts.DesiredHash = exact.ContentHash()
		if facts.ContentKind == realization.PathProjectionFile {
			fileUse, hasFileUse := supply.ExactFileUse()
			if !hasFileUse || fileUse.Scope() != facts.Scope {
				return nil, fmt.Errorf("managed file subject %q exact file use is missing or has the wrong scope", contract.SubjectID())
			}
			materialized, hasMaterialized := supply.MaterializedFileIdentity()
			if !hasMaterialized {
				return nil, fmt.Errorf("managed file subject %q materialized identity is missing", contract.SubjectID())
			}
			facts.DesiredHash = materialized.ContentHash()
			facts.DesiredFileMode = managedFileDesiredMode(
				facts.PermissionPolicy,
				facts.DesiredFileMode,
				fileUse.Executable(),
			)
		}
		if hasState {
			copy := state
			facts.Previous = &copy
		}
		lockObservation, observed := lockEvidence[supply.SubjectID()]
		if !observed {
			decisions = append(decisions, newManagedPathBlocked(facts, reconcile.ReasonMissingLock, "fresh exact-Supply lock observation is required"))
			continue
		}
		if lockObservation.Stale() {
			decisions = append(decisions, newManagedPathBlocked(facts, reconcile.ReasonStaleLock, "exact-Supply lock observation is stale"))
			continue
		}
		current, observed := evidence[managedPathEvidenceKey{subject: facts.Subject, destination: facts.Destination}]
		if !observed {
			decisions = append(decisions, newManagedPathBlocked(facts, reconcile.ReasonMissingLiveObservation, "fresh path evidence is required"))
			continue
		}
		facts.LiveHash = current.ContentHash()
		if facts.ContentKind == realization.PathProjectionFile {
			facts.LiveFileMode = current.FileMode()
		}
		decision := reconcileManagedPathDesired(
			facts,
			current,
			state,
			hasState,
			evidence,
			input.ManageUnmanagedMatches,
		)
		decision = enforceManagedPathOwnership(
			decision,
			hasState,
			input.Owner,
			ownershipEvidence,
			ownershipConflicts,
		)
		decisions = append(decisions, decision)
	}

	for _, contract := range input.Locked.Subjects() {
		realization, realized := contract.Realization()
		if !realized {
			continue
		}
		projection, managedPath := realization.ManagedPathProjection()
		if !managedPath {
			continue
		}
		if _, expected := expectations[contract.SubjectID()]; expected {
			continue
		}
		selectedConsumers := selectedManagedPathConsumers(projection.ConsumerTargets(), selection)
		if len(selectedConsumers) == 0 {
			continue
		}
		desiredSubjects[contract.SubjectID()] = struct{}{}
		facts := reconcile.ManagedPathDecisionInput{
			Subject: contract.SubjectID(), ConsumerTargets: selectedConsumers,
			Scope: projection.Scope(), Destination: projection.Destination(),
			ContentKind: projection.ContentKind(), PlacementMode: projection.PlacementMode(),
			PermissionPolicy: projection.PermissionPolicy(), DesiredFileMode: managedPathProjectionExactMode(projection),
		}
		decisions = append(decisions, newManagedPathBlocked(facts, reconcile.ReasonUnexpectedLockSubject, "locked managed path projection is not declared by the manifest"))
	}

	for subject, state := range states {
		if _, desired := desiredSubjects[subject]; desired {
			continue
		}
		selectedConsumers := selectedManagedPathConsumers(state.ConsumerTargets(), selection)
		if len(selectedConsumers) == 0 {
			continue
		}
		remaining := unselectedManagedPathConsumers(state.ConsumerTargets(), selection)
		facts := reconcile.ManagedPathDecisionInput{
			Subject:          subject,
			ConsumerTargets:  remaining,
			Scope:            state.Scope(),
			Destination:      state.Destination(),
			DesiredHash:      state.ContentHash(),
			ContentKind:      state.ContentKind(),
			PermissionPolicy: state.PermissionPolicy(),
			DesiredFileMode:  state.FileMode(),
			Previous:         &state,
		}
		if _, conflict := addressConflicts[managedPathAddressKey{scope: facts.Scope, destination: facts.Destination}]; conflict {
			decisions = append(decisions, newManagedPathBlocked(
				facts,
				reconcile.ReasonDestinationConflict,
				"managed destination is claimed by another projection subject",
			))
			continue
		}
		current, observed := evidence[managedPathEvidenceKey{subject: subject, destination: state.Destination()}]
		if !observed {
			decisions = append(decisions, newManagedPathBlocked(facts, reconcile.ReasonMissingLiveObservation, "fresh path evidence is required for managed removal"))
			continue
		}
		facts.LiveHash = current.ContentHash()
		if facts.ContentKind == realization.PathProjectionFile {
			facts.LiveFileMode = current.FileMode()
		}
		var decision managedPathDecision
		switch {
		case !current.Exists():
			decision = newManagedPathBlocked(facts, reconcile.ReasonMissingOutput, "managed output is missing")
		case current.ContentHash() != state.ContentHash():
			decision = newManagedPathBlocked(facts, reconcile.ReasonDriftedOutput, "managed output content differs from statefile baseline")
		case !state.PermissionPolicy().AcceptsMode(state.FileMode(), current.FileMode()):
			decision = newManagedPathBlocked(facts, reconcile.ReasonDriftedOutput, "managed output file mode differs from statefile baseline")
		case len(remaining) != 0:
			decision = newManagedPathRecord(facts, reconcile.ReasonStateStale, "selected consumer removed from shared ownership")
		default:
			decision = newManagedPathRemove(facts, reconcile.ReasonRemovedFromManifest)
		}
		decision = enforceManagedPathOwnership(
			decision,
			true,
			input.Owner,
			ownershipEvidence,
			ownershipConflicts,
		)
		decisions = append(decisions, decision)
	}

	sort.Slice(decisions, func(left int, right int) bool {
		return compareManagedPathDecisions(decisions[left], decisions[right]) < 0
	})
	return canonicalManagedPathDecisions(decisions)
}
