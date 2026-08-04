package projection

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
)

type aggregateDecision struct {
	kind            reconcile.AggregateDecisionKind
	reason          reconcile.ActionReason
	detail          string
	documentAddress aggregate.DocumentAddress
	codecContractID aggregate.CodecContractID
	projections     []aggregateProjectionDecision
	document        aggregate.Document
	snapshot        aggregate.Snapshot
	codecPlan       aggregate.Plan
	rendered        aggregate.RenderedDocument
	evidence        observe.AggregateEvidence
	preconditions   []aggregate.OperationPrecondition
}

type aggregateProjectionDecision struct {
	kind               reconcile.AggregateDecisionKind
	reason             reconcile.ActionReason
	detail             string
	contract           aggregate.ProjectionContract
	desired            *aggregate.ContributionSet
	previous           []durable.ManagedAggregateState
	managedBaseline    string
	hasManagedBaseline bool
	before             aggregate.ProjectionState
	expected           aggregate.ProjectionState
	deltas             []aggregateSubjectDelta
}

type aggregateSubjectDelta struct {
	subject     topology.SubjectID
	contract    aggregate.ProjectionContract
	previous    aggregate.ManagedContribution
	hasPrevious bool
	occupancy   aggregate.ContributionOccupancyState
	kind        reconcile.AggregateDecisionKind
	reason      reconcile.ActionReason
	detail      string
	mutatesHost bool
}

func canonicalAggregateDecisions(values []aggregateDecision) ([]reconcile.AggregateDecision, error) {
	result := make([]reconcile.AggregateDecision, 0, len(values))
	for index, value := range values {
		decision, err := value.canonical()
		if err != nil {
			return nil, fmt.Errorf("aggregate decision[%d]: %w", index, err)
		}
		result = append(result, decision)
	}
	return result, nil
}

func (decision aggregateDecision) canonical() (reconcile.AggregateDecision, error) {
	projections := make([]reconcile.AggregateProjectionDecisionInput, 0, len(decision.projections))
	for _, projection := range decision.projections {
		projections = append(projections, projection.canonicalInput())
	}
	return reconcile.NewAggregateDecision(reconcile.AggregateDecisionInput{
		Kind:            decision.kind,
		Reason:          decision.reason,
		Detail:          decision.detail,
		DocumentAddress: decision.documentAddress,
		CodecContractID: decision.codecContractID,
		Projections:     projections,
		Document:        decision.document,
		Snapshot:        decision.snapshot,
		CodecPlan:       decision.codecPlan,
		Rendered:        decision.rendered,
		Evidence:        decision.evidence,
		Preconditions:   decision.preconditions,
	})
}

func (projection aggregateProjectionDecision) canonicalInput() reconcile.AggregateProjectionDecisionInput {
	subjects := make([]reconcile.AggregateSubjectDecisionInput, 0, len(projection.deltas))
	for _, delta := range projection.deltas {
		var previous *aggregate.ManagedContribution
		if delta.hasPrevious {
			copy := delta.previous
			previous = &copy
		}
		subjects = append(subjects, reconcile.AggregateSubjectDecisionInput{
			Subject:     delta.subject,
			Contract:    delta.contract,
			Previous:    previous,
			Occupancy:   delta.occupancy,
			Kind:        delta.kind,
			Reason:      delta.reason,
			Detail:      delta.detail,
			MutatesHost: delta.mutatesHost,
		})
	}
	return reconcile.AggregateProjectionDecisionInput{
		Kind:               projection.kind,
		Reason:             projection.reason,
		Detail:             projection.detail,
		Contract:           projection.contract,
		Desired:            projection.desired,
		Previous:           projection.previous,
		ManagedBaseline:    projection.managedBaseline,
		HasManagedBaseline: projection.hasManagedBaseline,
		Before:             projection.before,
		Expected:           projection.expected,
		Subjects:           subjects,
	}
}

func (decision aggregateDecision) MutatesHost() bool {
	switch decision.kind {
	case reconcile.AggregateCreate, reconcile.AggregateReplace, reconcile.AggregateRemove:
		return true
	default:
		return false
	}
}

func (projection aggregateProjectionDecision) MutatesState() bool {
	return slices.ContainsFunc(projection.deltas, aggregateSubjectDeltaMutatesState)
}

func (projection aggregateProjectionDecision) Kind() reconcile.AggregateDecisionKind {
	return projection.kind
}

func (projection aggregateProjectionDecision) Reason() reconcile.ActionReason {
	return projection.reason
}

func cloneAggregateProjectionDecisions(values []aggregateProjectionDecision) []aggregateProjectionDecision {
	result := make([]aggregateProjectionDecision, len(values))
	for index, value := range values {
		result[index] = value
		result[index].desired = cloneContributionSetPointer(value.desired)
		result[index].previous = append([]durable.ManagedAggregateState(nil), value.previous...)
		result[index].deltas = append([]aggregateSubjectDelta(nil), value.deltas...)
	}
	return result
}
