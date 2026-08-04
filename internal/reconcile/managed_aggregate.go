package reconcile

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

// AggregateDecisionKind is the closed reconciliation result for one physical
// aggregate document batch.
type AggregateDecisionKind string

const (
	AggregateCreate  AggregateDecisionKind = "create"
	AggregateReplace AggregateDecisionKind = "replace"
	AggregateRemove  AggregateDecisionKind = "remove"
	AggregateRecord  AggregateDecisionKind = "record"
	AggregateNoOp    AggregateDecisionKind = "noop"
	AggregateBlocked AggregateDecisionKind = "blocked"
)

// AggregateSubjectDecisionInput contains one logical contribution transition
// inside an enclosing projection decision.
type AggregateSubjectDecisionInput struct {
	Subject     topology.SubjectID
	Contract    aggregate.ProjectionContract
	Previous    *aggregate.ManagedContribution
	Occupancy   aggregate.ContributionOccupancyState
	Kind        AggregateDecisionKind
	Reason      ActionReason
	Detail      string
	MutatesHost bool
}

// AggregateProjectionDecisionInput contains one complete semantic projection
// transition inside a physical document decision.
type AggregateProjectionDecisionInput struct {
	Kind               AggregateDecisionKind
	Reason             ActionReason
	Detail             string
	Contract           aggregate.ProjectionContract
	Desired            *aggregate.ContributionSet
	Previous           []durable.ManagedAggregateState
	ManagedBaseline    string
	HasManagedBaseline bool
	Before             aggregate.ProjectionState
	Expected           aggregate.ProjectionState
	Subjects           []AggregateSubjectDecisionInput
}

// AggregateDecisionInput contains every canonical fact for one physical
// aggregate document decision.
type AggregateDecisionInput struct {
	Kind            AggregateDecisionKind
	Reason          ActionReason
	Detail          string
	DocumentAddress aggregate.DocumentAddress
	CodecContractID aggregate.CodecContractID
	Projections     []AggregateProjectionDecisionInput
	Document        aggregate.Document
	Snapshot        aggregate.Snapshot
	CodecPlan       aggregate.Plan
	Rendered        aggregate.RenderedDocument
	Evidence        observe.AggregateEvidence
	Preconditions   []aggregate.OperationPrecondition
}

// AggregateDecision owns one codec plan over one physical document. Candidate
// bytes are non-authoritative until Effect revalidates and commits them.
type AggregateDecision struct {
	kind            AggregateDecisionKind
	reason          ActionReason
	detail          string
	documentAddress aggregate.DocumentAddress
	codecContractID aggregate.CodecContractID
	projections     []AggregateProjectionDecision
	document        aggregate.Document
	snapshot        aggregate.Snapshot
	codecPlan       aggregate.Plan
	rendered        aggregate.RenderedDocument
	evidence        observe.AggregateEvidence
	preconditions   []aggregate.OperationPrecondition
}

// AggregateProjectionDecision owns one semantic projection transition inside
// an enclosing physical document decision.
type AggregateProjectionDecision struct {
	kind               AggregateDecisionKind
	reason             ActionReason
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

// aggregateSubjectDelta separates one logical contribution transition from
// the enclosing physical aggregate write.
type aggregateSubjectDelta struct {
	subject     topology.SubjectID
	contract    aggregate.ProjectionContract
	previous    aggregate.ManagedContribution
	hasPrevious bool
	occupancy   aggregate.ContributionOccupancyState
	kind        AggregateDecisionKind
	reason      ActionReason
	detail      string
	mutatesHost bool
}

// NewAggregateDecision constructs and validates one immutable physical
// aggregate decision from a builder-owned draft.
func NewAggregateDecision(input AggregateDecisionInput) (AggregateDecision, error) {
	projections := make([]AggregateProjectionDecision, 0, len(input.Projections))
	for projectionIndex, projectionInput := range input.Projections {
		projection, err := newAggregateProjectionDecision(projectionInput)
		if err != nil {
			return AggregateDecision{}, fmt.Errorf("projection[%d]: %w", projectionIndex, err)
		}
		projections = append(projections, projection)
	}
	decision := AggregateDecision{
		kind:            input.Kind,
		reason:          input.Reason,
		detail:          input.Detail,
		documentAddress: input.DocumentAddress,
		codecContractID: input.CodecContractID,
		projections:     projections,
		document:        input.Document,
		snapshot:        input.Snapshot,
		codecPlan:       input.CodecPlan,
		rendered:        input.Rendered,
		evidence:        input.Evidence,
		preconditions:   append([]aggregate.OperationPrecondition(nil), input.Preconditions...),
	}
	if err := validateActionReason(input.Reason); err != nil {
		return AggregateDecision{}, fmt.Errorf("aggregate decision: %w", err)
	}
	if err := validateAggregateDecision(decision); err != nil {
		return AggregateDecision{}, err
	}
	return decision, nil
}

func newAggregateProjectionDecision(input AggregateProjectionDecisionInput) (AggregateProjectionDecision, error) {
	if err := validateAggregateDecisionKind(input.Kind); err != nil {
		return AggregateProjectionDecision{}, err
	}
	if err := validateActionReason(input.Reason); err != nil {
		return AggregateProjectionDecision{}, err
	}
	deltas := make([]aggregateSubjectDelta, 0, len(input.Subjects))
	seenSubjects := make(map[topology.SubjectID]struct{}, len(input.Subjects))
	for subjectIndex, subjectInput := range input.Subjects {
		if err := subjectInput.Subject.Validate(); err != nil {
			return AggregateProjectionDecision{}, fmt.Errorf("subject[%d]: %w", subjectIndex, err)
		}
		if subjectInput.Subject.Kind() != topology.SubjectProjection {
			return AggregateProjectionDecision{}, fmt.Errorf("subject[%d] %q is not a projection", subjectIndex, subjectInput.Subject)
		}
		if _, duplicate := seenSubjects[subjectInput.Subject]; duplicate {
			return AggregateProjectionDecision{}, fmt.Errorf("duplicate subject %q", subjectInput.Subject)
		}
		seenSubjects[subjectInput.Subject] = struct{}{}
		if !subjectInput.Contract.Equal(input.Contract) {
			return AggregateProjectionDecision{}, fmt.Errorf("subject[%d] contract differs from projection contract", subjectIndex)
		}
		if err := validateAggregateDecisionKind(subjectInput.Kind); err != nil {
			return AggregateProjectionDecision{}, fmt.Errorf("subject[%d]: %w", subjectIndex, err)
		}
		if err := validateActionReason(subjectInput.Reason); err != nil {
			return AggregateProjectionDecision{}, fmt.Errorf("subject[%d]: %w", subjectIndex, err)
		}
		if subjectInput.Occupancy != "" {
			if err := subjectInput.Occupancy.Validate(); err != nil {
				return AggregateProjectionDecision{}, fmt.Errorf("subject[%d]: %w", subjectIndex, err)
			}
		}
		if subjectInput.MutatesHost &&
			subjectInput.Kind != AggregateCreate &&
			subjectInput.Kind != AggregateReplace &&
			subjectInput.Kind != AggregateRemove {
			return AggregateProjectionDecision{}, fmt.Errorf(
				"subject[%d] variant %q cannot carry host mutation",
				subjectIndex,
				subjectInput.Kind,
			)
		}
		previous := aggregate.ManagedContribution{}
		hasPrevious := subjectInput.Previous != nil
		if hasPrevious {
			previous = *subjectInput.Previous
			if err := previous.Validate(); err != nil {
				return AggregateProjectionDecision{}, fmt.Errorf("subject[%d] previous contribution: %w", subjectIndex, err)
			}
		}
		deltas = append(deltas, aggregateSubjectDelta{
			subject:     subjectInput.Subject,
			contract:    subjectInput.Contract,
			previous:    previous,
			hasPrevious: hasPrevious,
			occupancy:   subjectInput.Occupancy,
			kind:        subjectInput.Kind,
			reason:      subjectInput.Reason,
			detail:      subjectInput.Detail,
			mutatesHost: subjectInput.MutatesHost,
		})
	}
	desired := input.Desired
	if desired != nil {
		copy, err := aggregate.NewContributionSet(desired.Contributions())
		if err != nil {
			return AggregateProjectionDecision{}, fmt.Errorf("desired contributions: %w", err)
		}
		desired = &copy
	}
	return AggregateProjectionDecision{
		kind:               input.Kind,
		reason:             input.Reason,
		detail:             input.Detail,
		contract:           input.Contract,
		desired:            desired,
		previous:           append([]durable.ManagedAggregateState(nil), input.Previous...),
		managedBaseline:    input.ManagedBaseline,
		hasManagedBaseline: input.HasManagedBaseline,
		before:             input.Before,
		expected:           input.Expected,
		deltas:             deltas,
	}, nil
}
