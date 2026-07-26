package readiness

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
)

// AggregateProjectionSummary is one fresh equivalence summary for a locked
// managed-aggregate realization.
type AggregateProjectionSummary struct {
	subject topology.SubjectID
	target  target.Target
	scope   target.Scope
	current observerelation.ObservationSummary
}

// ObserveAggregateProjectionSummaries reads and compares the selected locked
// aggregate projections named by subjects. Family-specific status semantics do
// not participate in this post-attempt equivalence check.
func ObserveAggregateProjectionSummaries(
	resolver liveobserve.DestinationResolver,
	locked lock.File,
	selection targetselection.Selection,
	currentState durable.Snapshot,
	codecs aggregate.CodecCatalog,
	subjects []topology.SubjectID,
) ([]AggregateProjectionSummary, error) {
	contracts, err := selectedAggregateProjectionContracts(locked, selection, subjects)
	if err != nil {
		return nil, err
	}
	if len(contracts) == 0 {
		return nil, nil
	}
	contributions, err := aggregateContributionsFromContracts(contracts)
	if err != nil {
		return nil, err
	}
	observations, err := observeAggregateDocuments(
		resolver,
		contributions,
		currentState.ManagedAggregates(),
		selection,
		codecs,
	)
	if err != nil {
		return nil, err
	}
	evidence := make(map[aggregate.DocumentAddress]observe.AggregateEvidence, len(observations.evidence))
	for _, item := range observations.evidence {
		evidence[item.Address()] = item
	}
	failures := make(map[aggregate.DocumentAddress]struct{}, len(observations.failures))
	for _, item := range observations.failures {
		failures[item.Address()] = struct{}{}
	}

	result := make([]AggregateProjectionSummary, 0, len(contracts))
	for _, contract := range contracts {
		contribution, _, err := contract.ManagedAggregateContribution()
		if err != nil {
			return nil, err
		}
		address := contribution.Contribution().Address().Document()
		summary := observerelation.ObservationUnknown
		if observed, ok := evidence[address]; ok {
			projection, covered := observed.Snapshot().State(contribution.Contribution().Contract())
			if !covered {
				return nil, fmt.Errorf(
					"aggregate projection %q is not covered by its document observation",
					contract.SubjectID(),
				)
			}
			switch {
			case !projection.Present():
				summary = observerelation.ObservationMissing
			case projection.CanonicalProjection() == contribution.Contribution().CanonicalContribution():
				summary = observerelation.ObservationPresent
			}
		} else if _, failed := failures[address]; !failed {
			return nil, fmt.Errorf(
				"aggregate projection %q has no fresh document observation",
				contract.SubjectID(),
			)
		}
		result = append(result, AggregateProjectionSummary{
			subject: contract.SubjectID(),
			target:  contribution.Contribution().Target(),
			scope:   contribution.Contribution().Scope(),
			current: summary,
		})
	}
	return result, nil
}

func selectedAggregateProjectionContracts(
	locked lock.File,
	selection targetselection.Selection,
	subjects []topology.SubjectID,
) ([]lock.LockedSubjectContract, error) {
	seen := make(map[topology.SubjectID]struct{}, len(subjects))
	contracts := make([]lock.LockedSubjectContract, 0, len(subjects))
	for _, subject := range subjects {
		if err := subject.Validate(); err != nil {
			return nil, fmt.Errorf("aggregate projection subject: %w", err)
		}
		if _, duplicate := seen[subject]; duplicate {
			continue
		}
		seen[subject] = struct{}{}
		contract, present := locked.Locked.Subject(subject)
		if !present {
			return nil, fmt.Errorf("aggregate projection subject %q is not locked", subject)
		}
		contribution, present, err := contract.ManagedAggregateContribution()
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, fmt.Errorf("subject %q has no aggregate projection realization", subject)
		}
		if !selection.Includes(contribution.Contribution().Target()) {
			return nil, fmt.Errorf("aggregate projection subject %q is outside target selection", subject)
		}
		contracts = append(contracts, contract)
	}
	return contracts, nil
}

func (summary AggregateProjectionSummary) Subject() topology.SubjectID {
	return summary.subject
}

func (summary AggregateProjectionSummary) Target() target.Target {
	return summary.target
}

func (summary AggregateProjectionSummary) Scope() target.Scope {
	return summary.scope
}

func (summary AggregateProjectionSummary) Current() observerelation.ObservationSummary {
	return summary.current
}
