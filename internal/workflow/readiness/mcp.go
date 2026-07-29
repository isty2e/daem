package readiness

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
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
	effective []mcpeffective.Observation,
	providers []MCPProviderPrerequisite,
) ([]mcpobserve.LockedProjectionObservation, error) {
	contracts, err := selectedMCPProjectionContracts(locked, selection)
	if err != nil {
		return nil, err
	}
	shadowing, err := effectiveShadowingBySubject(effective)
	if err != nil {
		return nil, err
	}
	providerEvidence, err := providerEvidenceBySubject(providers)
	if err != nil {
		return nil, err
	}
	return mcpobserve.ClassifyLockedProjections(mcpobserve.LockedProjectionBatchInput{
		Contracts:     contracts,
		CurrentState:  currentState,
		Evidence:      evidence,
		Failures:      failures,
		Preconditions: preconditions,
		Shadowing:     shadowing,
		Providers:     providerEvidence,
	})
}

func providerEvidenceBySubject(
	prerequisites []MCPProviderPrerequisite,
) (map[topology.SubjectID]mcpobserve.ProviderPrerequisiteObservation, error) {
	index, err := providerConsumerIndex(prerequisites)
	if err != nil {
		return nil, err
	}
	result := make(
		map[topology.SubjectID]mcpobserve.ProviderPrerequisiteObservation,
		len(index),
	)
	for subject, prerequisite := range index {
		input := mcpobserve.ProviderPrerequisiteObservationInput{
			Version: prerequisite.Observation().Version(),
		}
		switch prerequisite.State() {
		case MCPProviderCurrent:
			input.State = mcpobserve.ProviderCurrent
		case MCPProviderInstallRequired:
			input.State = mcpobserve.ProviderInstallPending
			switch prerequisite.Reason() {
			case MCPProviderReasonRelationInstall:
				input.Reason = mcpobserve.ReasonProviderRelationInstall
			case MCPProviderReasonPackageAbsent:
				input.Reason = mcpobserve.ReasonProviderPackageAbsent
			default:
				return nil, fmt.Errorf(
					"pending MCP provider has unsupported reason %q",
					prerequisite.Reason(),
				)
			}
			input.Version = ""
		case MCPProviderBlocked:
			input.State = mcpobserve.ProviderBlocked
			switch prerequisite.Reason() {
			case MCPProviderReasonVersionUnobserved:
				input.Reason = mcpobserve.ReasonProviderVersionUnobserved
			case MCPProviderReasonVersionIncompatible:
				input.Reason = mcpobserve.ReasonProviderVersionIncompatible
			case MCPProviderReasonCodecMismatch:
				input.Reason = mcpobserve.ReasonProviderCodecMismatch
			default:
				return nil, fmt.Errorf(
					"blocked MCP provider has unsupported reason %q",
					prerequisite.Reason(),
				)
			}
		default:
			return nil, fmt.Errorf(
				"MCP provider prerequisite has unsupported state %q",
				prerequisite.State(),
			)
		}
		observation, err := mcpobserve.NewProviderPrerequisiteObservation(input)
		if err != nil {
			return nil, fmt.Errorf("MCP provider evidence for %q: %w", subject, err)
		}
		result[subject] = observation
	}
	return result, nil
}

func effectiveShadowingBySubject(
	observations []mcpeffective.Observation,
) (map[topology.SubjectID]mcpobserve.ShadowState, error) {
	result := make(map[topology.SubjectID]mcpobserve.ShadowState, len(observations))
	for _, observation := range observations {
		subject := observation.Subject()
		if _, duplicate := result[subject]; duplicate {
			return nil, fmt.Errorf("duplicate provider-effective observation for %q", subject)
		}
		switch observation.State() {
		case mcpeffective.StateExact:
			result[subject] = mcpobserve.ShadowUnshadowed
		case mcpeffective.StateUnobservable:
			result[subject] = mcpobserve.ShadowUnknown
		case mcpeffective.StateConflicting:
			if observation.HigherConflictPresent() {
				result[subject] = mcpobserve.ShadowShadowedByLocal
			} else {
				result[subject] = mcpobserve.ShadowLowerPrecedenceUserConflict
			}
		default:
			return nil, fmt.Errorf(
				"provider-effective observation for %q has unsupported state %q",
				subject,
				observation.State(),
			)
		}
	}
	return result, nil
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
