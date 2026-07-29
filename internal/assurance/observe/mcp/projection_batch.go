package mcp

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// LockedProjectionBatchInput contains fresh generic aggregate evidence and the
// locked MCP projections it covers.
type LockedProjectionBatchInput struct {
	Contracts     []lock.LockedSubjectContract
	CurrentState  durable.Snapshot
	Evidence      []observe.AggregateEvidence
	Failures      []observe.AggregateObservationFailure
	Preconditions []observe.AggregatePreconditionEvidence
	Shadowing     map[topology.SubjectID]ShadowState
	Providers     map[topology.SubjectID]ProviderPrerequisiteObservation
}

// LockedProjectionObservation correlates one locked MCP projection with
// current passive evidence and separately classified attempt history.
type LockedProjectionObservation struct {
	current                AggregateProjectionObservation
	provider               ProviderPrerequisiteObservation
	lastDelegateAttempt    LastDelegateAttemptObservation
	target                 target.Target
	scope                  target.Scope
	configPath             output.Destination
	contentPath            aggregate.ContentPath
	adapterContractVersion aggregate.CodecContractID
}

// ClassifyLockedProjections derives typed MCP observations without performing
// I/O or granting mutation authority.
func ClassifyLockedProjections(input LockedProjectionBatchInput) ([]LockedProjectionObservation, error) {
	if len(input.Contracts) == 0 {
		return nil, nil
	}
	subjects := make(map[topology.SubjectID]struct{}, len(input.Contracts))
	for _, contract := range input.Contracts {
		subject := contract.SubjectID()
		if _, duplicate := subjects[subject]; duplicate {
			return nil, fmt.Errorf("duplicate locked MCP projection subject %q", subject)
		}
		subjects[subject] = struct{}{}
	}
	observed, err := newAggregateObservationIndex(input.Evidence, input.Failures, input.Preconditions)
	if err != nil {
		return nil, err
	}
	result := make([]LockedProjectionObservation, 0, len(input.Contracts))
	for _, contract := range input.Contracts {
		subject := contract.SubjectID()
		if _, ok := aggregate.MCPPlacementForSubject(subject); !ok {
			return nil, fmt.Errorf("subject %q is not an MCP projection", subject)
		}
		realization, ok := contract.Realization()
		if !ok {
			return nil, fmt.Errorf("MCP projection subject %q is missing a realization", subject.Key())
		}
		contribution, ok := realization.ManagedAggregateContribution()
		if !ok {
			return nil, fmt.Errorf("MCP projection subject %q is not a managed aggregate contribution", subject.Key())
		}
		projection, err := observed.projection(contribution.Contract())
		if err != nil {
			return nil, fmt.Errorf("MCP projection %q config evidence: %w", subject.Key(), err)
		}
		ownership, err := projectionOwnership(
			contract,
			contribution,
			input.CurrentState,
			projection.observed && projection.state.Present(),
		)
		if err != nil {
			return nil, fmt.Errorf("MCP projection %q ownership evidence: %w", subject.Key(), err)
		}
		lastDelegateInput, err := lastDelegateAttempt(contract, contribution, input.CurrentState)
		if err != nil {
			return nil, fmt.Errorf("MCP projection %q delegate attempt evidence: %w", subject.Key(), err)
		}
		lastDelegateObservation, err := LastDelegateAttemptObservationFromInput(lastDelegateInput)
		if err != nil {
			return nil, fmt.Errorf("MCP projection %q delegate attempt classification: %w", subject.Key(), err)
		}
		current, err := ObserveAggregateProjection(AggregateProjectionObservationInput{
			Contract:                   contract,
			Projection:                 projection.state,
			Observed:                   projection.observed,
			FailureReason:              projection.failureReason,
			UnsupportedAlternateConfig: projection.unsupportedAlternateConfig,
			Ownership:                  ownership,
			Shadowing:                  shadowingForSubject(input.Shadowing, subject),
		})
		if err != nil {
			return nil, err
		}
		provider, err := providerForContract(contract, input.Providers)
		if err != nil {
			return nil, fmt.Errorf("MCP projection %q provider evidence: %w", subject.Key(), err)
		}
		result = append(result, LockedProjectionObservation{
			current:                current,
			provider:               provider,
			lastDelegateAttempt:    lastDelegateObservation,
			target:                 contribution.Target(),
			scope:                  contribution.Scope(),
			configPath:             contribution.AggregateRoot(),
			contentPath:            contribution.Contract().Address().ContentPath(),
			adapterContractVersion: contribution.CodecContractID(),
		})
	}
	return result, nil
}

func providerForContract(
	contract lock.LockedSubjectContract,
	providers map[topology.SubjectID]ProviderPrerequisiteObservation,
) (ProviderPrerequisiteObservation, error) {
	subject := contract.SubjectID()
	_, mediated := contract.MCPProviderContribution()
	provider, present := providers[subject]
	switch {
	case mediated && !present:
		return ProviderPrerequisiteObservation{}, fmt.Errorf(
			"provider-mediated projection has no current provider evidence",
		)
	case !mediated && present:
		return ProviderPrerequisiteObservation{}, fmt.Errorf(
			"direct-host projection carries unexpected provider evidence",
		)
	case present:
		if provider.State() == ProviderNotApplicable {
			return ProviderPrerequisiteObservation{}, fmt.Errorf(
				"provider-mediated projection cannot be not_applicable",
			)
		}
		return provider, nil
	default:
		return NewProviderPrerequisiteObservation(ProviderPrerequisiteObservationInput{
			State: ProviderNotApplicable,
		})
	}
}

func shadowingForSubject(
	shadowing map[topology.SubjectID]ShadowState,
	subject topology.SubjectID,
) ShadowState {
	if state, present := shadowing[subject]; present {
		return state
	}
	return ShadowUnknown
}

func (observation LockedProjectionObservation) Subject() topology.SubjectID {
	return observation.current.Subject
}

func (observation LockedProjectionObservation) Target() target.Target {
	return observation.target
}

func (observation LockedProjectionObservation) Scope() target.Scope {
	return observation.scope
}

func (observation LockedProjectionObservation) ConfigPath() output.Destination {
	return observation.configPath
}

func (observation LockedProjectionObservation) ContentPath() aggregate.ContentPath {
	return observation.contentPath
}

func (observation LockedProjectionObservation) AdapterContractVersion() aggregate.CodecContractID {
	return observation.adapterContractVersion
}

func (observation LockedProjectionObservation) Current() AggregateProjectionObservation {
	return observation.current
}

func (observation LockedProjectionObservation) Provider() ProviderPrerequisiteObservation {
	return observation.provider
}

func (observation LockedProjectionObservation) LastDelegateAttempt() LastDelegateAttemptObservation {
	return observation.lastDelegateAttempt
}
