package apply

import (
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
)

type preparedGlobalCarrierPromotion struct {
	ref    string
	action reconciliation.RelationAction
}

func consumeApplyRoutePreflight(
	execution *applyContinuationExecution,
	route applyRouteScheduleFact,
) error {
	if execution == nil || !route.work.InvokesHost {
		return nil
	}
	if err := execution.consume(
		route.ref+"/preflight",
		operationplan.EffectStepObservation,
	); err != nil {
		return err
	}
	alternative := 0
	stepID := route.ref + "/preflight-accepted"
	switch route.preflight.kind {
	case applyRoutePreflightAccepted:
	case applyRoutePreflightRejected, applyRoutePreflightOperationalFailure:
		alternative = 1
		stepID = route.ref + "/preflight-rejected"
	default:
		return fmt.Errorf(
			"final host route %q has invalid preflight outcome %d",
			route.ref,
			route.preflight.kind,
		)
	}
	if err := execution.selectAlternative(route.ref+"/preflight-outcome", alternative); err != nil {
		return err
	}
	return execution.consume(stepID, operationplan.EffectStepNoOp)
}
