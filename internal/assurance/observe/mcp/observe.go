package mcp

import (
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/topology"
)

// ObserveAggregateProjection classifies passive MCP evidence already
// normalized by the generic aggregate observer.
func ObserveAggregateProjection(
	input AggregateProjectionObservationInput,
) (AggregateProjectionObservation, error) {
	subject, contribution, err := lockedMCPProjection(input.Contract)
	if err != nil {
		return AggregateProjectionObservation{}, err
	}
	if input.Observed == (input.FailureReason != "") {
		return AggregateProjectionObservation{}, fmt.Errorf("MCP aggregate observation requires exactly one success or failure fact")
	}
	if input.Observed && !input.Projection.Contract().Equal(contribution.Contract()) {
		return AggregateProjectionObservation{}, fmt.Errorf("MCP aggregate projection evidence differs from its locked contract")
	}
	ownership := ownershipObservation(input.Ownership)
	observation := AggregateProjectionObservation{
		Subject:   subject,
		Ownership: ownership,
		Shadowing: shadowObservation(input.Shadowing),
	}
	observation.Projection = projectionObservationFromAggregate(input, contribution, ownership)
	return observation, nil
}

func lockedMCPProjection(
	contract lock.LockedSubjectContract,
) (topology.SubjectID, aggregate.ManagedContribution, error) {
	subject := contract.SubjectID()
	if _, ok := aggregate.MCPPlacementForSubject(subject); !ok {
		return topology.SubjectID{}, aggregate.ManagedContribution{}, fmt.Errorf(
			"locked subject %s/%s %q is not an admitted MCP projection",
			subject.Kind(),
			subject.Namespace(),
			subject.Key(),
		)
	}
	item, present, err := contract.ManagedAggregateContribution()
	if err != nil {
		return topology.SubjectID{}, aggregate.ManagedContribution{}, err
	}
	if !present {
		return topology.SubjectID{}, aggregate.ManagedContribution{}, fmt.Errorf(
			"MCP projection subject %q is missing a managed aggregate realization",
			subject.Key(),
		)
	}
	return subject, item.Contribution(), nil
}

func projectionObservationFromAggregate(
	input AggregateProjectionObservationInput,
	contribution aggregate.ManagedContribution,
	ownership OwnershipObservation,
) ProjectionObservation {
	observation := ProjectionObservation{
		State:       ProjectionMissing,
		ConfigPath:  contribution.AggregateRoot(),
		ContentPath: contribution.ContentPath(),
	}
	if input.UnsupportedAlternateConfig {
		observation.State = ProjectionUnsupported
		observation.Reason = ReasonUnsupportedAlternateConfig
		return observation
	}
	if input.FailureReason != "" {
		observation.State = ProjectionUnsupported
		observation.Reason = reasonFromCodecFailure(input.FailureReason)
		switch input.FailureReason {
		case aggregate.CodecFailureDocumentMalformed, aggregate.CodecFailureDuplicateKey:
			observation.State = ProjectionMalformed
			if observation.Reason == ReasonNone {
				observation.Reason = ReasonConfigMalformed
			}
		default:
			if observation.Reason == ReasonNone {
				observation.Reason = ReasonProjectionEquivalenceUndefined
			}
		}
		return observation
	}
	observation.Present = input.Projection.Present()
	if !observation.Present {
		return observation
	}
	observation.Equivalent =
		input.Projection.CanonicalProjection() == contribution.CanonicalContribution()
	if ownership.State == OwnershipUnmanagedSameName {
		observation.State = ProjectionUnmanagedSameName
		observation.Reason = ReasonRoutePreexistingUnowned
		return observation
	}
	if observation.Equivalent {
		observation.State = ProjectionProjected
		return observation
	}
	observation.State = ProjectionDrifted
	return observation
}

func reasonFromCodecFailure(reason aggregate.CodecFailureReason) ReasonCode {
	switch reason {
	case aggregate.CodecFailureDocumentMalformed, aggregate.CodecFailureDuplicateKey:
		return ReasonConfigMalformed
	case aggregate.CodecFailureUnsupportedTransport:
		return ReasonUnsupportedTransport
	case aggregate.CodecFailureUnsupportedManagedField:
		return ReasonUnsupportedManagedField
	case aggregate.CodecFailureSecretLiteralForbidden:
		return ReasonSecretLiteralForbidden
	case aggregate.CodecFailureCanonicalInvalid:
		return ReasonStaleAdapterContract
	default:
		return ReasonProjectionEquivalenceUndefined
	}
}

func ownershipObservation(state OwnershipState) OwnershipObservation {
	switch state {
	case OwnershipManaged, OwnershipAdopted, OwnershipUnmanagedSameName, OwnershipUnknown:
	default:
		state = OwnershipUnknown
	}
	observation := OwnershipObservation{State: state}
	switch state {
	case OwnershipUnmanagedSameName:
		observation.Reason = ReasonRoutePreexistingUnowned
	case OwnershipUnknown:
		observation.Reason = ReasonOwnershipStateUnobserved
	}
	return observation
}

func shadowObservation(state ShadowState) ShadowObservation {
	switch state {
	case ShadowUnshadowed, ShadowShadowedByLocal, ShadowLowerPrecedenceUserConflict, ShadowCarrierCollision, ShadowUnknown:
	default:
		state = ShadowUnknown
	}
	observation := ShadowObservation{State: state}
	switch state {
	case ShadowShadowedByLocal, ShadowLowerPrecedenceUserConflict, ShadowCarrierCollision:
		observation.Reason = ReasonConfigShadowed
	case ShadowUnknown:
		observation.Reason = ReasonEffectiveStateUnobserved
	}
	return observation
}
