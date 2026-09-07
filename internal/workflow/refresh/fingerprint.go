package refresh

import (
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
)

func refreshFingerprint(planned plan) (mutation.OperationFingerprint, error) {
	return operationplan.RefreshOperationFingerprint(operationplan.RefreshIdentityInput{
		Mode:          string(planned.result.Mode),
		ManifestPath:  planned.result.ManifestPath,
		LockfilePath:  planned.result.LockfilePath,
		StatefilePath: planned.result.StatefilePath,
		Selection: operationplan.RefreshSelection{
			ID:      planned.result.Selection.ID,
			Target:  string(planned.result.Selection.Target),
			Scope:   string(planned.result.Selection.Scope),
			Carrier: planned.result.Selection.Carrier,
		},
		Route: operationplan.RefreshRoute{
			Operation:              string(planned.result.Route.Operation),
			RouteID:                planned.result.Route.RouteID,
			AdapterContractVersion: planned.result.Route.AdapterContractVersion,
			RequestHash:            planned.result.Route.RequestHash,
			ExecutionSubject:       planned.result.Route.ExecutionSubject,
			ObservationPosture:     string(planned.result.Route.ObservationPosture),
		},
		Disclosure: operationplan.RefreshDisclosure{
			Invocation: operationplan.RefreshInvocation{
				Kind:           planned.result.Disclosure.Invocation.Kind,
				Command:        planned.result.Disclosure.Invocation.Command,
				Args:           planned.result.Disclosure.Invocation.Args,
				EnvNames:       planned.result.Disclosure.Invocation.EnvNames,
				CWDPolicy:      planned.result.Disclosure.Invocation.CWDPolicy,
				TimeoutSeconds: planned.result.Disclosure.Invocation.TimeoutSeconds,
			},
			EffectClasses:         planned.result.Disclosure.EffectClasses,
			RetainedEffectClasses: planned.result.Disclosure.RetainedEffectClasses,
			NonClaims:             planned.result.Disclosure.NonClaims,
		},
		Observation: refreshOperationObservation(planned.result.Observation),
		Operation: operationplan.RefreshOperationContract{
			Actuation:       string(planned.operationContract.Actuation()),
			Authority:       string(planned.operationContract.Authority()),
			Preconditions:   planned.operationContract.Preconditions(),
			EffectEnvelope:  string(planned.operationContract.EffectEnvelope()),
			Idempotency:     string(planned.operationContract.Idempotency()),
			Verification:    string(planned.operationContract.Verification()),
			TrustActivation: string(planned.operationContract.TrustActivation()),
			Recovery:        string(planned.operationContract.Recovery()),
		},
	})
}

func refreshOperationObservation(observation *Observation) *operationplan.RefreshObservation {
	if observation == nil {
		return nil
	}
	return &operationplan.RefreshObservation{
		State:        string(observation.State),
		Reason:       string(observation.Reason),
		Availability: string(observation.Availability),
		Freshness:    string(observation.Freshness),
	}
}
