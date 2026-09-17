package reconcile

import (
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
)

const (
	ActionChangePin             RelationActionKind = "change_pin"
	ReasonPinTransitionConflict RelationReasonCode = "pin_transition_conflict"
)

type PinTransitionActionInput struct {
	Transition        durablecarrier.PinTransition
	CurrentClaim      durablecarrier.ManagedCarrierClaim
	BeforeCorrelation observerelation.CorrelationResult
	AfterCorrelation  observerelation.CorrelationResult
	RouteAdmission    RelationRouteAdmissionDecision
	Exclusive         bool
}

type pinChangeFacts struct {
	transition durablecarrier.PinTransition
	before     observerelation.CorrelationResult
	resuming   bool
}

func NewPinTransitionAction(input PinTransitionActionInput) (RelationAction, error) {
	if err := input.Transition.Validate(); err != nil {
		return RelationAction{}, err
	}
	if err := validateCorrelationMatchesRelation(input.Transition.Before().Identity().ExpectedRelation(), input.BeforeCorrelation); err != nil {
		return RelationAction{}, err
	}
	pending, resuming := input.CurrentClaim.PendingPinTransition()
	if resuming && !pending.ExactEqual(input.Transition) || !resuming && !input.CurrentClaim.ExactEqual(input.Transition.Before()) {
		return RelationAction{}, fmt.Errorf("pin action requires exact prior management or its exact reservation")
	}
	action, err := NewRelationAction(RelationActionInput{
		CarrierIdentity: input.Transition.Identity(),
		RouteRequest:    input.Transition.Request(),
		Correlation:     input.AfterCorrelation,
		RouteAdmission:  input.RouteAdmission,
	})
	if err != nil {
		return RelationAction{}, err
	}
	action.pinChange = &pinChangeFacts{transition: input.Transition, before: input.BeforeCorrelation, resuming: resuming}
	action = action.BlockForPinTransition()
	oldExact := freshPinCorrelation(input.BeforeCorrelation, observerelation.StateExactCorrelation) &&
		freshPinCorrelation(input.AfterCorrelation, observerelation.StateUnkeyedSameSubject)
	newExact := freshPinCorrelation(input.AfterCorrelation, observerelation.StateExactCorrelation) &&
		freshPinCorrelation(input.BeforeCorrelation, observerelation.StateUnkeyedSameSubject)
	if input.Exclusive && (oldExact || resuming && newExact) &&
		input.RouteAdmission.AllowsHostRouteInvocation() && input.RouteAdmission.ObservationPolicy() == ObservationRequireCurrent {
		action.kind = ActionChangePin
		action.execution = ExecutionHostRoute
		action.reason = ReasonNone
	}
	return action, nil
}

func freshPinCorrelation(value observerelation.CorrelationResult, state observerelation.CorrelationState) bool {
	return value.State() == state && value.EvidenceAvailability() == observerelation.InventorySupported && value.EvidenceFreshness() == observerelation.EvidenceFresh
}

func (action RelationAction) PinTransition() (durablecarrier.PinTransition, bool) {
	if action.pinChange == nil {
		return durablecarrier.PinTransition{}, false
	}
	return action.pinChange.transition, true
}

func (action RelationAction) PinTransitionBeforeCorrelation() (observerelation.CorrelationResult, bool) {
	if action.pinChange == nil {
		return observerelation.CorrelationResult{}, false
	}
	return action.pinChange.before, true
}

func (action RelationAction) ResumesPinTransition() bool {
	return action.pinChange != nil && action.pinChange.resuming
}

func (action RelationAction) BlockForPinTransition() RelationAction {
	action.kind = ActionBlock
	action.execution = ExecutionBlocked
	action.reason = ReasonPinTransitionConflict
	return action
}
