package readiness

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// ClassifyMCPProjections correlates selected locked MCP projections with one
// already-collected aggregate evidence set.
func ClassifyMCPProjections(
	locked lock.File,
	selection targetselection.Selection,
	currentState durable.Snapshot,
	evidence []observe.AggregateEvidence,
	failures []observe.AggregateObservationFailure,
	preconditions []observe.AggregatePreconditionEvidence,
) ([]mcpobserve.LockedProjectionObservation, error) {
	contracts, err := selectedMCPProjectionContracts(locked, selection)
	if err != nil {
		return nil, err
	}
	return mcpobserve.ClassifyLockedProjections(mcpobserve.LockedProjectionBatchInput{
		Contracts:     contracts,
		CurrentState:  currentState,
		Evidence:      evidence,
		Failures:      failures,
		Preconditions: preconditions,
	})
}

func selectedMCPProjectionContracts(
	locked lock.File,
	selection targetselection.Selection,
) ([]lock.LockedSubjectContract, error) {
	contracts := make([]lock.LockedSubjectContract, 0)
	for _, contract := range locked.Locked.Subjects() {
		subject := contract.SubjectID()
		if _, ok := aggregate.MCPPlacementForSubject(subject); !ok {
			continue
		}
		realization, ok := contract.Realization()
		if !ok {
			return nil, fmt.Errorf("MCP projection subject %q is missing a realization", subject.Key())
		}
		contribution, ok := realization.ManagedAggregateContribution()
		if !ok {
			return nil, fmt.Errorf("MCP projection subject %q is not a managed aggregate contribution", subject.Key())
		}
		if selection.Includes(contribution.Target()) {
			contracts = append(contracts, contract)
		}
	}
	return contracts, nil
}
