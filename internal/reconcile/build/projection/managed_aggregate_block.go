package projection

import (
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
)

func blockedAggregateDocument(
	groups []aggregateGroupInput,
	address aggregate.DocumentAddress,
	codecContractID aggregate.CodecContractID,
	reason reconcile.ActionReason,
	detail string,
) aggregateDecision {
	projections := make([]aggregateProjectionDecision, 0, len(groups))
	hasLockBlocker := aggregateGroupsHaveBlockedSubjects(groups)
	for _, group := range groups {
		desired, _ := aggregateContributionSet(group.desired)
		projection := aggregateProjectionDecision{
			contract: group.contract,
			desired:  cloneContributionSetPointer(desired),
			previous: append([]durable.ManagedAggregateState(nil), group.previous...),
		}
		if len(group.blocked) != 0 {
			projections = append(projections, blockAggregateProjectionFromFacts(projection, group))
			continue
		}
		if hasLockBlocker {
			projections = append(projections, blockAggregateProjection(
				projection,
				reconcile.ReasonAggregateLockBlocked,
				"aggregate projection is blocked by another contribution's lock readiness",
			))
			continue
		}
		projections = append(projections, blockAggregateProjection(projection, reason, detail))
	}
	decisionReason, decisionDetail := firstAggregateProjectionLockFailure(projections)
	return aggregateDecision{
		kind: reconcile.AggregateBlocked, reason: decisionReason, detail: decisionDetail,
		documentAddress: address, codecContractID: codecContractID, projections: projections,
	}
}

func finalizeBlockedAggregateDocument(
	address aggregate.DocumentAddress,
	codecContractID aggregate.CodecContractID,
	projections []aggregateProjectionDecision,
	evidence observe.AggregateEvidence,
) aggregateDecision {
	reason, detail := firstAggregateProjectionFailure(projections)
	for index := range projections {
		if projections[index].kind != "" {
			continue
		}
		siblingReason := reason
		siblingDetail := "aggregate document is blocked by another projection: " + detail
		if aggregateLockReadinessReason(reason) {
			siblingReason = reconcile.ReasonAggregateLockBlocked
			siblingDetail = "aggregate projection is blocked by another contribution's lock readiness"
		}
		projections[index] = blockAggregateProjection(
			projections[index],
			siblingReason,
			siblingDetail,
		)
	}
	decision := aggregateDecision{
		kind: reconcile.AggregateBlocked, reason: reason, detail: detail,
		documentAddress: address, codecContractID: codecContractID,
		projections: cloneAggregateProjectionDecisions(projections),
		document:    evidence.Document(), snapshot: evidence.Snapshot(), evidence: evidence,
	}
	decision.disableHostMutation()
	return decision
}

func aggregateLockReadinessReason(reason reconcile.ActionReason) bool {
	switch reason {
	case reconcile.ReasonMissingLock,
		reconcile.ReasonStaleLock,
		reconcile.ReasonUnexpectedLockSubject,
		reconcile.ReasonAggregateLockBlocked:
		return true
	default:
		return false
	}
}

func firstAggregateProjectionFailure(
	projections []aggregateProjectionDecision,
) (reconcile.ActionReason, string) {
	for _, projection := range projections {
		if projection.kind == reconcile.AggregateBlocked {
			return projection.reason, projection.detail
		}
	}
	return reconcile.ReasonInvalidDesiredState, "aggregate document is blocked"
}

func firstAggregateProjectionLockFailure(
	projections []aggregateProjectionDecision,
) (reconcile.ActionReason, string) {
	for _, projection := range projections {
		for _, delta := range projection.deltas {
			switch delta.reason {
			case reconcile.ReasonMissingLock, reconcile.ReasonStaleLock, reconcile.ReasonUnexpectedLockSubject:
				return delta.reason, delta.detail
			}
		}
	}
	return firstAggregateProjectionFailure(projections)
}

func classifyAggregateDocument(
	before aggregate.Document,
	beforeMode os.FileMode,
	after aggregate.Document,
	projections []aggregateProjectionDecision,
) (reconcile.AggregateDecisionKind, reconcile.ActionReason) {
	projectionMutatesHost := false
	for _, projection := range projections {
		if projection.kind == reconcile.AggregateCreate ||
			projection.kind == reconcile.AggregateReplace ||
			projection.kind == reconcile.AggregateRemove {
			projectionMutatesHost = true
			break
		}
	}
	if !projectionMutatesHost {
		for _, projection := range projections {
			if projection.MutatesState() {
				return reconcile.AggregateRecord, projection.reason
			}
		}
		return reconcile.AggregateNoOp, reconcile.ReasonAlreadyCurrent
	}
	switch {
	case !before.Exists() && after.Exists():
		return reconcile.AggregateCreate, reconcile.ReasonMissingOutput
	case before.Exists() && !after.Exists():
		return reconcile.AggregateRemove, reconcile.ReasonRemovedFromManifest
	case !before.Equal(after):
		return reconcile.AggregateReplace, reconcile.ReasonContentChanged
	case after.Exists() && beforeMode.Perm() != aggregate.DocumentFileMode:
		return reconcile.AggregateReplace, reconcile.ReasonFileModeChanged
	}
	return reconcile.AggregateReplace, reconcile.ReasonContentChanged
}

func (decision *aggregateDecision) enableHostMutation() {
	if !decision.MutatesHost() {
		return
	}
	for projectionIndex := range decision.projections {
		projection := &decision.projections[projectionIndex]
		projectionMutatesHost := projection.kind == reconcile.AggregateCreate ||
			projection.kind == reconcile.AggregateReplace ||
			projection.kind == reconcile.AggregateRemove
		for deltaIndex := range projection.deltas {
			delta := &projection.deltas[deltaIndex]
			delta.mutatesHost = projectionMutatesHost &&
				delta.kind != reconcile.AggregateNoOp &&
				delta.kind != reconcile.AggregateRecord &&
				delta.kind != reconcile.AggregateBlocked
		}
	}
}

func (decision *aggregateDecision) disableHostMutation() {
	for projectionIndex := range decision.projections {
		for deltaIndex := range decision.projections[projectionIndex].deltas {
			decision.projections[projectionIndex].deltas[deltaIndex].mutatesHost = false
		}
	}
}
