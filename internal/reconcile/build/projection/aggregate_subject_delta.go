package projection

import (
	"fmt"
	"os"
	"sort"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
)

func classifyAggregateProjection(
	codec aggregate.Codec,
	projection aggregateProjectionDecision,
	fileMode os.FileMode,
	manageUnmanagedMatches bool,
) (aggregateProjectionDecision, error) {
	previous := make(map[topology.SubjectID]aggregate.ManagedContribution, len(projection.previous))
	for _, state := range projection.previous {
		previous[state.Subject()] = state.Contribution()
	}
	desired := make(map[topology.SubjectID]aggregate.ManagedContribution)
	if projection.desired != nil {
		for _, item := range projection.desired.Contributions() {
			desired[item.SubjectID()] = item.Contribution()
		}
	}
	sameProjection := aggregateProjectionStatesEqual(projection.before, projection.expected)
	modeCurrent := fileMode == 0 || fileMode.Perm() == aggregate.DocumentFileMode
	if projection.hasManagedBaseline &&
		projection.before.CanonicalProjection() != projection.managedBaseline &&
		!sameProjection {
		return blockAggregateProjection(
			projection,
			reconcile.ReasonDriftedOutput,
			"managed aggregate projection differs from statefile baseline",
		), nil
	}
	if len(previous) == 0 && projection.before.Present() {
		if projection.desired == nil {
			return blockAggregateProjection(
				projection,
				reconcile.ReasonInvalidDesiredState,
				"unmanaged aggregate projection has no desired contribution",
			), nil
		}
		occupancy, err := codec.ClassifyContributionOccupancy(projection.before, *projection.desired)
		if err != nil {
			return aggregateProjectionDecision{}, fmt.Errorf(
				"observe aggregate subject occupancy: %w",
				err,
			)
		}
		if !manageUnmanagedMatches || !sameProjection {
			return blockUnmanagedAggregateProjection(projection, occupancy), nil
		}
		for _, item := range projection.desired.Contributions() {
			state, covered := occupancy.State(item.SubjectID())
			if !covered || state != aggregate.ContributionPresent {
				return blockAggregateProjection(
					projection,
					reconcile.ReasonInvalidDesiredState,
					"exact unmanaged aggregate adoption lacks unambiguous subject occupancy",
				), nil
			}
		}
	}

	subjects := aggregateProjectionSubjects(projection)
	deltas := make([]aggregateSubjectDelta, 0, len(subjects))
	for _, subject := range subjects {
		before, hadBefore := previous[subject]
		after, hasAfter := desired[subject]
		delta := aggregateSubjectDelta{
			subject: subject, contract: projection.contract,
			previous: before, hasPrevious: hadBefore,
		}
		switch {
		case len(previous) == 0 && !hadBefore && hasAfter &&
			projection.before.Present() && sameProjection && modeCurrent:
			delta.kind, delta.reason = reconcile.AggregateRecord, reconcile.ReasonManagedExisting
		case len(previous) == 0 && !hadBefore && hasAfter &&
			projection.before.Present() && sameProjection:
			delta.kind, delta.reason = reconcile.AggregateReplace, reconcile.ReasonFileModeChanged
		case !hadBefore && hasAfter && sameProjection && modeCurrent:
			delta.kind, delta.reason = reconcile.AggregateCreate, reconcile.ReasonMissingOutput
		case !hadBefore && hasAfter && sameProjection:
			delta.kind, delta.reason = reconcile.AggregateReplace, reconcile.ReasonFileModeChanged
		case !hadBefore && hasAfter:
			delta.kind, delta.reason = reconcile.AggregateCreate, reconcile.ReasonMissingOutput
		case hadBefore && !hasAfter:
			delta.kind, delta.reason = reconcile.AggregateRemove, reconcile.ReasonRemovedFromManifest
		case hadBefore && hasAfter && before.Equal(after):
			if !modeCurrent {
				delta.kind, delta.reason = reconcile.AggregateReplace, reconcile.ReasonFileModeChanged
			} else {
				delta.kind, delta.reason = reconcile.AggregateNoOp, reconcile.ReasonAlreadyCurrent
			}
		case hadBefore && hasAfter && !before.Equal(after):
			if sameProjection && modeCurrent {
				delta.kind, delta.reason = reconcile.AggregateRecord, reconcile.ReasonStateStale
			} else if sameProjection {
				delta.kind, delta.reason = reconcile.AggregateReplace, reconcile.ReasonFileModeChanged
			} else {
				delta.kind, delta.reason = reconcile.AggregateReplace, reconcile.ReasonContentChanged
			}
		default:
			delta.kind, delta.reason, delta.detail = reconcile.AggregateBlocked, reconcile.ReasonInvalidDesiredState, "aggregate subject has neither previous nor desired contribution"
		}
		deltas = append(deltas, delta)
	}
	projection.deltas = deltas
	projection.kind, projection.reason = classifyProjectionTransition(projection, modeCurrent)
	return projection, nil
}

func blockUnmanagedAggregateProjection(
	projection aggregateProjectionDecision,
	occupancy aggregate.ContributionOccupancySet,
) aggregateProjectionDecision {
	projection.kind = reconcile.AggregateBlocked
	projection.reason = reconcile.ReasonUnmanagedOutputExists
	projection.detail = "aggregate projection exists without managed authority"
	projection.deltas = make([]aggregateSubjectDelta, 0, len(aggregateProjectionSubjects(projection)))
	for _, subject := range aggregateProjectionSubjects(projection) {
		previous, hasPrevious := aggregatePreviousContribution(projection, subject)
		delta := aggregateSubjectDelta{
			subject: subject, contract: projection.contract,
			previous: previous, hasPrevious: hasPrevious,
			kind:   reconcile.AggregateBlocked,
			reason: reconcile.ReasonUnmanagedOutputExists,
			detail: "aggregate projection exists without managed authority",
		}
		state, covered := occupancy.State(subject)
		if covered {
			delta.occupancy = state
		}
		projection.deltas = append(projection.deltas, delta)
	}
	return projection
}

func classifyProjectionTransition(
	projection aggregateProjectionDecision,
	modeCurrent bool,
) (reconcile.AggregateDecisionKind, reconcile.ActionReason) {
	switch {
	case !projection.before.Present() && projection.expected.Present():
		return reconcile.AggregateCreate, reconcile.ReasonMissingOutput
	case projection.before.Present() && !projection.expected.Present():
		return reconcile.AggregateRemove, reconcile.ReasonRemovedFromManifest
	case !aggregateProjectionStatesEqual(projection.before, projection.expected):
		return reconcile.AggregateReplace, reconcile.ReasonContentChanged
	case !modeCurrent && projection.expected.Present():
		return reconcile.AggregateReplace, reconcile.ReasonFileModeChanged
	}
	if aggregateProjectionOwnershipChanged(projection) {
		return reconcile.AggregateRecord, reconcile.ReasonStateStale
	}
	for _, delta := range projection.deltas {
		if aggregateSubjectDeltaMutatesState(delta) {
			return reconcile.AggregateRecord, delta.reason
		}
	}
	return reconcile.AggregateNoOp, reconcile.ReasonAlreadyCurrent
}

func aggregateProjectionOwnershipChanged(projection aggregateProjectionDecision) bool {
	previous, err := aggregateStateContributionSet(projection.previous)
	if err != nil {
		return true
	}
	switch {
	case previous == nil:
		return false
	case projection.desired == nil:
		return true
	default:
		return !previous.Equal(*projection.desired)
	}
}

func blockAggregateProjection(
	projection aggregateProjectionDecision,
	reason reconcile.ActionReason,
	detail string,
) aggregateProjectionDecision {
	subjects := aggregateProjectionSubjects(projection)
	projection.kind, projection.reason, projection.detail = reconcile.AggregateBlocked, reason, detail
	projection.deltas = make([]aggregateSubjectDelta, 0, len(subjects))
	for _, subject := range subjects {
		previous, hasPrevious := aggregatePreviousContribution(projection, subject)
		projection.deltas = append(projection.deltas, aggregateSubjectDelta{
			subject: subject, contract: projection.contract,
			previous: previous, hasPrevious: hasPrevious,
			kind: reconcile.AggregateBlocked, reason: reason, detail: detail,
		})
	}
	return projection
}

func blockAggregateProjectionFromFacts(
	projection aggregateProjectionDecision,
	group aggregateGroupInput,
) aggregateProjectionDecision {
	subjects := aggregateGroupSubjects(group)
	projection.kind = reconcile.AggregateBlocked
	projection.deltas = make([]aggregateSubjectDelta, 0, len(subjects))
	for _, subject := range subjects {
		fact, exact := group.blocked[subject]
		previous, hasPrevious := aggregatePreviousContribution(projection, subject)
		reason := reconcile.ReasonAggregateLockBlocked
		detail := "aggregate projection is blocked by another contribution's lock readiness"
		if exact {
			reason, detail = fact.reason, fact.detail
		}
		if projection.reason == "" || exact {
			projection.reason, projection.detail = reason, detail
		}
		projection.deltas = append(projection.deltas, aggregateSubjectDelta{
			subject: subject, contract: projection.contract,
			previous: previous, hasPrevious: hasPrevious,
			kind: reconcile.AggregateBlocked, reason: reason, detail: detail,
		})
	}
	return projection
}

func aggregatePreviousContribution(
	projection aggregateProjectionDecision,
	subject topology.SubjectID,
) (aggregate.ManagedContribution, bool) {
	for _, state := range projection.previous {
		if state.Subject() == subject {
			return state.Contribution(), true
		}
	}
	return aggregate.ManagedContribution{}, false
}

func aggregateProjectionSubjects(projection aggregateProjectionDecision) []topology.SubjectID {
	seen := make(map[topology.SubjectID]struct{}, len(projection.previous))
	for _, state := range projection.previous {
		seen[state.Subject()] = struct{}{}
	}
	if projection.desired != nil {
		for _, item := range projection.desired.Contributions() {
			seen[item.SubjectID()] = struct{}{}
		}
	}
	for _, delta := range projection.deltas {
		seen[delta.subject] = struct{}{}
	}
	subjects := make([]topology.SubjectID, 0, len(seen))
	for subject := range seen {
		subjects = append(subjects, subject)
	}
	sort.Slice(subjects, func(left int, right int) bool {
		return topology.CompareSubjectID(subjects[left], subjects[right]) < 0
	})
	return subjects
}

func aggregateProjectionStatesEqual(
	left aggregate.ProjectionState,
	right aggregate.ProjectionState,
) bool {
	return left.Contract().Equal(right.Contract()) &&
		left.ParentPresent() == right.ParentPresent() &&
		left.Present() == right.Present() &&
		left.CanonicalProjection() == right.CanonicalProjection()
}

func aggregateSubjectDeltaMutatesState(delta aggregateSubjectDelta) bool {
	return delta.kind == reconcile.AggregateCreate || delta.kind == reconcile.AggregateReplace ||
		delta.kind == reconcile.AggregateRemove || delta.kind == reconcile.AggregateRecord
}
