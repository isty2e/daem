package projection

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func enforceAggregateProjectionOwnership(
	projection aggregateProjectionDecision,
	owner stateauthority.Authority,
	observations map[ownershipObservationKey]observe.OwnershipObservation,
	conflicts map[ownershipObservationKey]struct{},
) aggregateProjectionDecision {
	address := projection.contract.Address()
	document := address.Document()
	if document.Scope() != target.ScopeGlobal {
		return projection
	}

	preblocked := projection.kind == reconcile.AggregateBlocked
	key := ownershipObservationKey{
		destination: document.AggregateRoot(),
		contentPath: output.ContentPath(address.ContentPath()),
	}
	observation, observed := observations[key]
	if !observed || owner.IsZero() {
		if preblocked {
			return projection
		}
		return blockAggregateProjection(
			projection,
			reconcile.ReasonOwnershipObservationMissing,
			"durable ownership observation and owner authority are required for a global aggregate projection",
		)
	}
	if _, conflicted := conflicts[key]; conflicted {
		return blockAggregateProjection(
			projection,
			reconcile.ReasonOwnershipConflict,
			"global aggregate projection overlaps another managed address",
		)
	}

	claim, claimed := observation.Claim().Get()
	hasState := len(projection.previous) != 0
	if !claimed {
		if hasState {
			return blockAggregateProjection(
				projection,
				reconcile.ReasonOwnershipClaimMissing,
				"managed global aggregate state has no durable ownership claim",
			)
		}
		return projection
	}
	observedAddress, exact := observation.ExactAddress()
	if !exact || !claim.Address().Equal(observedAddress) || !claim.OwnedBy(owner) {
		return blockAggregateProjection(
			projection,
			reconcile.ReasonOwnershipConflict,
			fmt.Sprintf("global aggregate projection is claimed by manifest %q", claim.Owner().ManifestPath()),
		)
	}
	if claim.State() == ownership.ClaimReserved {
		return blockAggregateProjection(
			projection,
			reconcile.ReasonOwnershipReserved,
			fmt.Sprintf("global aggregate projection is reserved by interrupted operation %q", claim.OperationID()),
		)
	}
	if !hasState {
		return blockAggregateProjection(
			projection,
			reconcile.ReasonOwnershipStateConflict,
			"active durable claim has no matching local managed aggregate state",
		)
	}
	return projection
}
