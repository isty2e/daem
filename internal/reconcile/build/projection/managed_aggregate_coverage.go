package projection

import (
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

// aggregateDocumentCoverage separates total decision membership from the
// projections whose current host state must be observed.
type aggregateDocumentCoverage struct {
	decision       aggregate.Selection
	observation    aggregate.Selection
	hasObservation bool
}

func newAggregateDocumentCoverage(
	groups []aggregateGroupInput,
) (aggregateDocumentCoverage, error) {
	if len(groups) == 0 {
		return aggregateDocumentCoverage{}, fmt.Errorf("aggregate document group is empty")
	}
	decisionContracts := make([]aggregate.ProjectionContract, 0, len(groups))
	observationContracts := make([]aggregate.ProjectionContract, 0, len(groups))
	for _, group := range groups {
		decisionContracts = append(decisionContracts, group.contract)
		if len(group.desired) != 0 || len(group.previous) != 0 {
			observationContracts = append(observationContracts, group.contract)
		}
	}
	decision, err := aggregate.NewSelection(decisionContracts)
	if err != nil {
		return aggregateDocumentCoverage{}, err
	}
	coverage := aggregateDocumentCoverage{decision: decision}
	if len(observationContracts) == 0 {
		if !aggregateGroupsHaveAnyBlockedSubjects(groups) {
			return aggregateDocumentCoverage{}, fmt.Errorf(
				"aggregate document has neither observable nor blocked projections",
			)
		}
		return coverage, nil
	}
	observation, err := aggregate.NewSelection(observationContracts)
	if err != nil {
		return aggregateDocumentCoverage{}, err
	}
	if observation.DocumentAddress() != decision.DocumentAddress() ||
		observation.CodecContractID() != decision.CodecContractID() {
		return aggregateDocumentCoverage{}, fmt.Errorf(
			"aggregate observation coverage differs from its decision document",
		)
	}
	coverage.observation = observation
	coverage.hasObservation = true
	return coverage, nil
}

func (coverage aggregateDocumentCoverage) DecisionSelection() aggregate.Selection {
	return coverage.decision
}

func (coverage aggregateDocumentCoverage) ObservationSelection() (aggregate.Selection, bool) {
	return coverage.observation, coverage.hasObservation
}
