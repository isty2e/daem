package operationplan

import (
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
)

// CarrierHandoffRemainderInput contains owner projections for the plan after retirement.
type CarrierHandoffRemainderInput struct {
	Managed, Aggregates, Relations, Absences, Adoptions, Orders, Delegates json.RawMessage
}

func (input CarrierHandoffRemainderInput) Fingerprint() (mutation.OperationFingerprint, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	return mutation.NewOperationFingerprint(payload), nil
}

type FinalRouteIdentity struct {
	Reference string          `json:"reference"`
	Action    json.RawMessage `json:"action"`
	Work      RouteWork       `json:"work"`
	Preflight json.RawMessage `json:"preflight"`
}

type FinalRoutePlanIdentityInput struct {
	PhaseEstablished        bool                 `json:"phase_established"`
	StatefileInitiallyBound bool                 `json:"statefile_initially_bound"`
	Routes                  []FinalRouteIdentity `json:"routes"`
}

func (input FinalRoutePlanIdentityInput) canonicalBytes() ([]byte, error) {
	if input.Routes == nil {
		input.Routes = []FinalRouteIdentity{}
	}
	return json.Marshal(input)
}

func (input FinalRoutePlanIdentityInput) Fingerprint() (mutation.OperationFingerprint, error) {
	payload, err := input.canonicalBytes()
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("marshal final host route plan: %w", err)
	}
	return mutation.NewOperationFingerprint(payload), nil
}
