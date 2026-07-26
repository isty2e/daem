package mcp

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type aggregateProjectionObservation struct {
	state                      aggregate.ProjectionState
	observed                   bool
	failureReason              aggregate.CodecFailureReason
	unsupportedAlternateConfig bool
}

type aggregateObservationIndex struct {
	evidence      map[aggregate.DocumentAddress]observe.AggregateEvidence
	failures      map[aggregate.DocumentAddress]observe.AggregateObservationFailure
	preconditions map[aggregate.DocumentAddress]map[aggregate.OperationPrecondition]bool
}

func newAggregateObservationIndex(
	evidence []observe.AggregateEvidence,
	failures []observe.AggregateObservationFailure,
	preconditions []observe.AggregatePreconditionEvidence,
) (aggregateObservationIndex, error) {
	result := aggregateObservationIndex{
		evidence:      make(map[aggregate.DocumentAddress]observe.AggregateEvidence, len(evidence)),
		failures:      make(map[aggregate.DocumentAddress]observe.AggregateObservationFailure, len(failures)),
		preconditions: make(map[aggregate.DocumentAddress]map[aggregate.OperationPrecondition]bool),
	}
	for _, item := range evidence {
		address := item.Address()
		if _, duplicate := result.evidence[address]; duplicate {
			return aggregateObservationIndex{}, fmt.Errorf("duplicate MCP aggregate evidence")
		}
		result.evidence[address] = item
	}
	for _, item := range failures {
		address := item.Address()
		if _, duplicate := result.failures[address]; duplicate {
			return aggregateObservationIndex{}, fmt.Errorf("duplicate MCP aggregate failed observation")
		}
		if _, successful := result.evidence[address]; successful {
			return aggregateObservationIndex{}, fmt.Errorf("MCP aggregate has both evidence and failed observation")
		}
		result.failures[address] = item
	}
	for _, item := range preconditions {
		byContract := result.preconditions[item.Owner()]
		if byContract == nil {
			byContract = make(map[aggregate.OperationPrecondition]bool)
			result.preconditions[item.Owner()] = byContract
		}
		precondition := item.Precondition()
		if _, duplicate := byContract[precondition]; duplicate {
			return aggregateObservationIndex{}, fmt.Errorf("duplicate MCP aggregate precondition evidence")
		}
		byContract[precondition] = item.Satisfied()
	}
	return result, nil
}

func (index aggregateObservationIndex) projection(
	contract aggregate.ProjectionContract,
) (aggregateProjectionObservation, error) {
	address := contract.Address().Document()
	unsupportedAlternate, err := index.unsupportedAlternateConfig(contract)
	if err != nil {
		return aggregateProjectionObservation{}, err
	}
	if evidence, present := index.evidence[address]; present {
		if state, covered := evidence.Snapshot().State(contract); covered {
			return aggregateProjectionObservation{
				state:                      state,
				observed:                   true,
				unsupportedAlternateConfig: unsupportedAlternate,
			}, nil
		}
		return aggregateProjectionObservation{}, fmt.Errorf("successful evidence does not cover locked projection")
	}
	if failure, present := index.failures[address]; present {
		covered := false
		for _, selected := range failure.Selection().Contracts() {
			if selected.Equal(contract) {
				covered = true
				break
			}
		}
		if !covered {
			return aggregateProjectionObservation{}, fmt.Errorf("failed observation does not cover locked projection")
		}
		reason := failure.Reason()
		if failure.ContentPath() != "" &&
			failure.ContentPath() != contract.Address().ContentPath() {
			reason = aggregate.CodecFailureEquivalenceUndefined
		}
		return aggregateProjectionObservation{
			failureReason:              reason,
			unsupportedAlternateConfig: unsupportedAlternate,
		}, nil
	}
	return aggregateProjectionObservation{}, fmt.Errorf("fresh aggregate observation is required")
}

func (index aggregateObservationIndex) unsupportedAlternateConfig(
	contract aggregate.ProjectionContract,
) (bool, error) {
	expected, admitted, err := aggregate.OperationPreconditionsForCodec(contract.CodecContractID())
	if err != nil {
		return false, err
	}
	if !admitted {
		return false, fmt.Errorf("aggregate codec %q has no precondition profile", contract.CodecContractID())
	}
	observed := index.preconditions[contract.Address().Document()]
	if len(observed) != len(expected) {
		return false, fmt.Errorf("aggregate precondition evidence is incomplete")
	}
	for _, precondition := range expected {
		satisfied, present := observed[precondition]
		if !present {
			return false, fmt.Errorf("aggregate precondition evidence is incomplete")
		}
		if precondition.Kind() == aggregate.OperationPreconditionDocumentAbsent && !satisfied {
			return true, nil
		}
	}
	return false, nil
}
