package apply

import (
	"context"

	"github.com/isty2e/daem/internal/effect/metadataconversion"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile"
)

func attemptMetadataConversion(ctx context.Context, planned commandPlan) error {
	providerActions, err := providerInstallActions(planned.assessment.MCPProviders)
	if err != nil {
		return err
	}
	envelope, err := metadataMaintenanceEnvelopeFor(planned, providerActions)
	if err != nil {
		return err
	}
	if envelope.Demand().EnsureCalls() == 0 {
		return nil
	}
	return metadataconversion.ConvertIfNeeded(ctx, planned.barrier)
}

func metadataMaintenanceEnvelopeFor(planned commandPlan, providerActions []reconcile.RelationAction) (operationplan.Envelope, error) {
	baseline := planned.assessment.CurrentState
	visibility := 0
	if planned.nativePlan != nil {
		if err := planned.nativePlan.state.Validate(); err != nil {
			return operationplan.Envelope{}, err
		}
		if planned.nativePlan.state.StateChanged() {
			visibility = 1
		}
	} else {
		input, err := applyEffectInput(planned, providerActions)
		if err != nil {
			return operationplan.Envelope{}, err
		}
		baseline = input.CurrentState
		visibility, err = maximumCommandForwardValidationCount(planned, input)
		if err != nil {
			return operationplan.Envelope{}, err
		}
	}
	work, err := applyEnvelopeWorkFor(planned, providerActions, baseline)
	if err != nil {
		return operationplan.Envelope{}, err
	}
	work.ExecuteGates = visibility
	return operationplan.CompileApply(work)
}
