package projection

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type managedPathEvidenceKey struct {
	subject     topology.SubjectID
	destination output.Destination
}

type managedPathAddressKey struct {
	scope       target.Scope
	destination output.Destination
}

func managedPathEvidenceIndex(values []observe.ManagedPathEvidence) (map[managedPathEvidenceKey]observe.ManagedPathEvidence, error) {
	result := make(map[managedPathEvidenceKey]observe.ManagedPathEvidence, len(values))
	for index, value := range values {
		key := managedPathEvidenceKey{subject: value.Subject(), destination: value.Destination()}
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("duplicate managed path evidence[%d] for %q", index, value.Destination())
		}
		result[key] = value
	}
	return result, nil
}

func managedPathStateIndex(values []durable.ManagedPathState) (map[topology.SubjectID]durable.ManagedPathState, error) {
	result := make(map[topology.SubjectID]durable.ManagedPathState, len(values))
	for index, value := range values {
		if _, duplicate := result[value.Subject()]; duplicate {
			return nil, fmt.Errorf("duplicate managed path state[%d] for subject %q", index, value.Subject())
		}
		result[value.Subject()] = value
	}
	return result, nil
}

func managedPathAddressConflicts(
	expectations []ManagedPathExpectation,
	states map[topology.SubjectID]durable.ManagedPathState,
) map[managedPathAddressKey]struct{} {
	owners := make(map[managedPathAddressKey]topology.SubjectID, len(expectations)+len(states))
	conflicts := make(map[managedPathAddressKey]struct{})
	add := func(key managedPathAddressKey, subject topology.SubjectID) {
		if owner, present := owners[key]; present && owner != subject {
			conflicts[key] = struct{}{}
			return
		}
		owners[key] = subject
	}
	for _, expectation := range expectations {
		input := expectation.decisionInput()
		add(managedPathAddressKey{scope: input.Scope, destination: input.Destination}, input.Subject)
	}
	for subject, state := range states {
		add(managedPathAddressKey{scope: state.Scope(), destination: state.Destination()}, subject)
	}
	return conflicts
}

func managedPathSupplyObservationIndex(values []observe.ExactSupplyObservation) (map[topology.SubjectID]observe.ExactSupplyObservation, error) {
	result := make(map[topology.SubjectID]observe.ExactSupplyObservation)
	for index, value := range values {
		if _, duplicate := result[value.Subject()]; duplicate {
			return nil, fmt.Errorf("duplicate managed path Supply observation[%d] for subject %q", index, value.Subject())
		}
		result[value.Subject()] = value
	}
	return result, nil
}

func managedPathSelection(values reconcile.SelectedTargets) map[target.Target]struct{} {
	result := make(map[target.Target]struct{}, values.Len())
	for _, value := range values.Values() {
		result[value] = struct{}{}
	}
	return result
}

func selectedManagedPathConsumers(values []target.Target, selected map[target.Target]struct{}) []target.Target {
	result := make([]target.Target, 0, len(values))
	for _, value := range values {
		if _, ok := selected[value]; ok {
			result = append(result, value)
		}
	}
	return result
}

func unselectedManagedPathConsumers(values []target.Target, selected map[target.Target]struct{}) []target.Target {
	result := make([]target.Target, 0, len(values))
	for _, value := range values {
		if _, ok := selected[value]; !ok {
			result = append(result, value)
		}
	}
	return result
}

func mergeManagedPathConsumers(left []target.Target, right []target.Target) []target.Target {
	seen := make(map[target.Target]struct{}, len(left)+len(right))
	result := make([]target.Target, 0, len(left)+len(right))
	for _, values := range [][]target.Target{left, right} {
		for _, value := range values {
			if _, duplicate := seen[value]; duplicate {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	slices.Sort(result)
	return result
}

func sameManagedPathConsumers(left []target.Target, right []target.Target) bool {
	left = mergeManagedPathConsumers(left, nil)
	right = mergeManagedPathConsumers(right, nil)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func enforceManagedPathOwnership(
	decision managedPathDecision,
	hasState bool,
	owner ownership.OwnerAuthority,
	observations map[ownershipObservationKey]observe.OwnershipObservation,
	conflicts map[ownershipObservationKey]struct{},
) managedPathDecision {
	preblocked := decision.IsBlocked()
	input := decision.input
	relocating := input.Previous != nil &&
		(input.Previous.Scope() != input.Scope || input.Previous.Destination() != input.Destination)
	previousGlobal := relocating && input.Previous.Scope() == target.ScopeGlobal
	currentGlobal := input.Scope == target.ScopeGlobal
	if !previousGlobal && !currentGlobal {
		return decision
	}
	if owner.IsZero() {
		if preblocked {
			return decision
		}
		return newManagedPathBlocked(input, reconcile.ReasonOwnershipObservationMissing, "durable ownership owner authority is required for a global output")
	}
	if previousGlobal {
		previousKey := ownershipObservationKey{destination: input.Previous.Destination()}
		previousObservation, present := observations[previousKey]
		if !present {
			if preblocked {
				return decision
			}
			return newManagedPathBlocked(input, reconcile.ReasonOwnershipObservationMissing, "durable ownership observation is required for the previous global output")
		}
		if _, conflicted := conflicts[previousKey]; conflicted {
			return newManagedPathBlocked(input, reconcile.ReasonOwnershipConflict, "previous global output overlaps another managed address")
		}
		if reason, detail, blocked := managedPathActiveClaimBlock(previousObservation, owner); blocked {
			return newManagedPathBlocked(input, reason, detail)
		}
	}
	if relocating {
		if !currentGlobal {
			return decision
		}
		key := ownershipObservationKey{destination: input.Destination}
		observation, present := observations[key]
		if !present {
			if preblocked {
				return decision
			}
			return newManagedPathBlocked(input, reconcile.ReasonOwnershipObservationMissing, "durable ownership observation is required for the new global output")
		}
		if _, conflicted := conflicts[key]; conflicted {
			return newManagedPathBlocked(input, reconcile.ReasonOwnershipConflict, "new global output overlaps another managed address")
		}
		if claim, claimed := observation.Claim.Get(); claimed {
			return newManagedPathBlocked(
				input,
				reconcile.ReasonOwnershipConflict,
				fmt.Sprintf("replacement address is claimed by manifest %q", claim.Owner().ManifestPath()),
			)
		}
		return decision
	}

	key := ownershipObservationKey{destination: input.Destination}
	observation, present := observations[key]
	if !present {
		if preblocked {
			return decision
		}
		return newManagedPathBlocked(input, reconcile.ReasonOwnershipObservationMissing, "durable ownership observation is required for a global output")
	}
	if _, conflicted := conflicts[key]; conflicted {
		return newManagedPathBlocked(input, reconcile.ReasonOwnershipConflict, "multiple outputs resolve to overlapping canonical managed addresses")
	}
	_, claimed := observation.Claim.Get()
	if !claimed {
		if hasState {
			return newManagedPathBlocked(input, reconcile.ReasonOwnershipClaimMissing, "managed global state has no durable ownership claim")
		}
		return decision
	}
	if reason, detail, blocked := managedPathActiveClaimBlock(observation, owner); blocked {
		return newManagedPathBlocked(input, reason, detail)
	}
	if !hasState {
		return newManagedPathBlocked(input, reconcile.ReasonOwnershipStateConflict, "active durable claim has no matching local managed state")
	}
	return decision
}

func managedPathActiveClaimBlock(
	observation observe.OwnershipObservation,
	owner ownership.OwnerAuthority,
) (reconcile.ActionReason, string, bool) {
	claim, claimed := observation.Claim.Get()
	if !claimed {
		return reconcile.ReasonOwnershipClaimMissing, "managed global state has no durable ownership claim", true
	}
	if !claim.Address().Equal(observation.Address) || !claim.OwnedBy(owner) {
		return reconcile.ReasonOwnershipConflict, fmt.Sprintf("managed address is claimed by manifest %q", claim.Owner().ManifestPath()), true
	}
	if claim.State() == ownership.ClaimReserved {
		return reconcile.ReasonOwnershipReserved, fmt.Sprintf("managed address is reserved by interrupted operation %q", claim.OperationID()), true
	}
	return "", "", false
}
