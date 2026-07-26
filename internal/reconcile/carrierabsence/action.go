package carrierabsence

import (
	"cmp"
	"fmt"
	"sort"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// DesiredRelationState describes how the current desired lock relates to one
// exact retained managed claim.
type DesiredRelationState string

const (
	DesiredRetained           DesiredRelationState = "retained"
	DesiredAbsent             DesiredRelationState = "absent"
	DesiredTransitionConflict DesiredRelationState = "transition_conflict"
)

// Decision is the closed carrier-absence reconciliation outcome.
type Decision string

const (
	DecisionRetain               Decision = "retain"
	DecisionRemove               Decision = "remove"
	DecisionRetireAlreadyAbsent  Decision = "retire_already_absent"
	DecisionVerifyPendingRemoval Decision = "verify_pending_removal"
	DecisionBlockAmbiguous       Decision = "block_ambiguous"
	DecisionBlockPending         Decision = "block_pending_removal"
	DecisionBlockStale           Decision = "block_stale"
	DecisionBlockShared          Decision = "block_shared"
	DecisionBlockUnobserved      Decision = "block_unobserved"
	DecisionBlockRoute           Decision = "block_route"
	DecisionBlockTransition      Decision = "block_transition"
)

const nonClaimAmbientConsumersNotObservable = "ambient_consumers_not_observable"

// ActionInput contains canonical facts for one exact claim owned by the
// selected state authority. Occupancy may include claims owned by other
// manifests, but it must contain this exact claim as a consumer.
type ActionInput struct {
	Claim       durablecarrier.ManagedCarrierClaim
	Desired     DesiredRelationState
	Observation observerelation.Correlation
	Occupancy   durablecarrier.CarrierOccupancy
	Route       RouteAdmission
	Pending     *durablecarrier.PendingCarrierRemoval
}

// Action is one immutable, pure carrier-absence decision. It owns
// classification only; observation, route realization, execution, persistence,
// confirmation, and presentation remain outside this model.
type Action struct {
	claim          durablecarrier.ManagedCarrierClaim
	desired        DesiredRelationState
	observation    observerelation.Correlation
	hasObservation bool
	occupancy      durablecarrier.CarrierOccupancy
	route          RouteAdmission
	pending        durablecarrier.PendingCarrierRemoval
	hasPending     bool
	decision       Decision
}

// NewAction validates canonical inputs and classifies one managed claim.
func NewAction(input ActionInput) (Action, error) {
	if err := input.Claim.Validate(); err != nil {
		return Action{}, fmt.Errorf("carrier absence claim: %w", err)
	}
	if err := input.Occupancy.Validate(); err != nil {
		return Action{}, fmt.Errorf("carrier absence occupancy: %w", err)
	}
	if input.Occupancy.Carrier() != input.Claim.Identity().Carrier() {
		return Action{}, fmt.Errorf("carrier absence occupancy belongs to another carrier")
	}
	if !occupancyContainsClaim(input.Occupancy, input.Claim) {
		return Action{}, fmt.Errorf("carrier absence occupancy does not contain the exact candidate claim")
	}
	if err := input.Route.Validate(); err != nil {
		return Action{}, fmt.Errorf("carrier absence route: %w", err)
	}
	if input.Pending != nil {
		if err := input.Pending.Validate(); err != nil {
			return Action{}, fmt.Errorf("carrier absence pending removal: %w", err)
		}
		if !input.Pending.Claim().ExactEqual(input.Claim) {
			return Action{}, fmt.Errorf("carrier absence pending removal does not match the exact claim")
		}
	}

	action := Action{
		claim:     input.Claim,
		desired:   input.Desired,
		occupancy: input.Occupancy,
		route:     input.Route,
	}
	if input.Pending != nil {
		action.pending = *input.Pending
		action.hasPending = true
	}
	switch input.Desired {
	case DesiredRetained:
		if observationProvided(input.Observation) || input.Route.Status() != RouteUnavailable {
			return Action{}, fmt.Errorf("retained carrier action must not consume absence observation or route facts")
		}
		if action.hasPending {
			action.decision = DecisionBlockPending
		} else {
			action.decision = DecisionRetain
		}
	case DesiredTransitionConflict:
		if observationProvided(input.Observation) || input.Route.Status() != RouteUnavailable {
			return Action{}, fmt.Errorf("carrier transition action must not consume absence observation or route facts")
		}
		action.decision = DecisionBlockTransition
	case DesiredAbsent:
		if err := validateObservation(input.Claim.Identity(), input.Observation); err != nil {
			return Action{}, err
		}
		action.observation = input.Observation
		action.hasObservation = true
		action.decision = classifyAbsence(
			input.Claim.Identity(),
			input.Observation.Result,
			input.Occupancy,
			input.Claim,
			input.Route,
			action.pending,
			action.hasPending,
		)
	default:
		return Action{}, fmt.Errorf("carrier desired relation state %q is unsupported", input.Desired)
	}
	return action, nil
}

// Validate rejects zero or forged actions.
func (action Action) Validate() error {
	rebuilt, err := NewAction(ActionInput{
		Claim:       action.claim,
		Desired:     action.desired,
		Observation: action.observation,
		Occupancy:   action.occupancy,
		Route:       action.route,
		Pending:     pendingPointer(action.pending, action.hasPending),
	})
	if err != nil {
		return err
	}
	if action.hasObservation != rebuilt.hasObservation ||
		action.hasPending != rebuilt.hasPending ||
		(action.hasPending && !action.pending.ExactEqual(rebuilt.pending)) ||
		action.decision != rebuilt.decision {
		return fmt.Errorf("carrier absence action does not match canonical classification")
	}
	return nil
}

// Compare returns the canonical ordering for two carrier-absence actions.
func (action Action) Compare(other Action) int {
	left := action.claim
	right := other.claim
	if order := cmp.Compare(left.Owner().StatefileKey(), right.Owner().StatefileKey()); order != 0 {
		return order
	}
	if order := cmp.Compare(left.Identity().Target(), right.Identity().Target()); order != 0 {
		return order
	}
	if order := cmp.Compare(left.Identity().Scope(), right.Identity().Scope()); order != 0 {
		return order
	}
	if order := topology.CompareSubjectID(left.Identity().RelationSubject(), right.Identity().RelationSubject()); order != 0 {
		return order
	}
	return cmp.Compare(
		left.Identity().ExpectedRelation().ManagedInstanceKey(),
		right.Identity().ExpectedRelation().ManagedInstanceKey(),
	)
}

// Claim returns the exact durable authority consumed by this action.
func (action Action) Claim() durablecarrier.ManagedCarrierClaim { return action.claim }

// Subject returns the declaration-local relation subject.
func (action Action) Subject() topology.SubjectID {
	return action.claim.Identity().RelationSubject()
}

// Target returns the exact host target.
func (action Action) Target() target.Target { return action.claim.Identity().Target() }

// Scope returns the exact host scope.
func (action Action) Scope() target.Scope { return action.claim.Identity().Scope() }

// Desired returns the relation between current desired state and the claim.
func (action Action) Desired() DesiredRelationState { return action.desired }

// Decision returns the closed reconciliation outcome.
func (action Action) Decision() Decision { return action.decision }

// Observation returns the exact keyed evidence consumed for desired absence.
// Retain and transition decisions do not consume an observation.
func (action Action) Observation() (observerelation.Correlation, bool) {
	return action.observation, action.hasObservation
}

// Occupancy returns the immutable daem-known consumer view used by this action.
func (action Action) Occupancy() durablecarrier.CarrierOccupancy { return action.occupancy }

// RemainingDaemKnownConsumers returns consumers other than this exact claim.
func (action Action) RemainingDaemKnownConsumers() []durablecarrier.CarrierConsumer {
	consumers := action.occupancy.DaemKnownConsumers()
	remaining := make([]durablecarrier.CarrierConsumer, 0, len(consumers)-1)
	for _, consumer := range consumers {
		if consumerMatchesClaim(consumer, action.claim) {
			continue
		}
		remaining = append(remaining, consumer)
	}
	return remaining
}

// RouteAdmission returns the route facts consumed by this decision.
func (action Action) RouteAdmission() RouteAdmission { return action.route }

// PendingRemoval returns the exact write-ahead fact this action must settle or
// reuse. Fresh already-absent actions have no pending removal.
func (action Action) PendingRemoval() (durablecarrier.PendingCarrierRemoval, bool) {
	return action.pending, action.hasPending
}

// BlocksOrdinaryApply reports whether this action refuses convergence.
func (action Action) BlocksOrdinaryApply() bool {
	switch action.decision {
	case DecisionBlockAmbiguous,
		DecisionBlockPending,
		DecisionBlockStale,
		DecisionBlockShared,
		DecisionBlockUnobserved,
		DecisionBlockRoute,
		DecisionBlockTransition:
		return true
	default:
		return false
	}
}

// InvokesHostRoute reports whether apply may invoke the admitted removal route
// after stale revalidation and confirmation.
func (action Action) InvokesHostRoute() bool {
	return action.decision == DecisionRemove && action.route.InvokesHostRoute()
}

// MutatesDirectProjection reports whether apply may directly remove the exact
// managed host-config relation after stale revalidation and confirmation.
func (action Action) MutatesDirectProjection() bool {
	return action.decision == DecisionRemove && action.route.MutatesDirectProjection()
}

// HostRouteRequest returns the exact host route this decision may invoke.
func (action Action) HostRouteRequest() (realizationdelegate.Request, bool) {
	if action.InvokesHostRoute() {
		return action.route.Request(), true
	}
	return realizationdelegate.Request{}, false
}

// DirectProjectionRequest returns the exact direct-removal route this decision
// may execute.
func (action Action) DirectProjectionRequest() (realizationdelegate.Request, bool) {
	if action.MutatesDirectProjection() {
		return action.route.Request(), true
	}
	return realizationdelegate.Request{}, false
}

// RetiresClaim reports whether convergence ends by retiring the exact claim.
func (action Action) RetiresClaim() bool {
	return action.decision == DecisionRemove ||
		action.decision == DecisionRetireAlreadyAbsent ||
		action.decision == DecisionVerifyPendingRemoval
}

// StateOnly reports whether claim retirement requires no host invocation.
func (action Action) StateOnly() bool {
	return action.decision == DecisionRetireAlreadyAbsent
}

// VerifiesPendingRemoval reports whether apply must settle a prior removal
// exclusively from fresh current postcondition evidence.
func (action Action) VerifiesPendingRemoval() bool {
	return action.decision == DecisionVerifyPendingRemoval
}

// NonClaims returns the canonical operation and occupancy limits that must
// survive into immutable disclosure and confirmation.
func (action Action) NonClaims() []string {
	nonClaims := append([]string{}, action.route.NonClaims()...)
	if action.desired != DesiredAbsent || action.Scope() != target.ScopeGlobal {
		return nonClaims
	}
	index := sort.SearchStrings(nonClaims, nonClaimAmbientConsumersNotObservable)
	if index < len(nonClaims) && nonClaims[index] == nonClaimAmbientConsumersNotObservable {
		return nonClaims
	}
	nonClaims = append(nonClaims, "")
	copy(nonClaims[index+1:], nonClaims[index:])
	nonClaims[index] = nonClaimAmbientConsumersNotObservable
	return nonClaims
}

func classifyAbsence(
	identity durablecarrier.ManagedCarrierIdentity,
	correlation observerelation.CorrelationResult,
	occupancy durablecarrier.CarrierOccupancy,
	claim durablecarrier.ManagedCarrierClaim,
	route RouteAdmission,
	pending durablecarrier.PendingCarrierRemoval,
	hasPending bool,
) Decision {
	switch correlation.State() {
	case observerelation.StateMissing:
		if hasPending {
			return DecisionVerifyPendingRemoval
		}
		return DecisionRetireAlreadyAbsent
	case observerelation.StateExactCorrelation,
		observerelation.StateUnkeyedSameSubject:
		if !ObservationAdmitsRouteResolution(identity, correlation) {
			return DecisionBlockAmbiguous
		}
		return classifyPresentRemoval(occupancy, claim, route, pending, hasPending)
	case observerelation.StateStaleEvidence:
		return DecisionBlockStale
	case observerelation.StateUnsupported, observerelation.StateUnavailableEvidence:
		return DecisionBlockUnobserved
	case observerelation.StateSameSubjectShadow,
		observerelation.StateManagedKeyDrift,
		observerelation.StateAmbiguous:
		return DecisionBlockAmbiguous
	default:
		panic(fmt.Sprintf("validated carrier absence correlation has unsupported state %q", correlation.State()))
	}
}

func classifyPresentRemoval(
	occupancy durablecarrier.CarrierOccupancy,
	claim durablecarrier.ManagedCarrierClaim,
	route RouteAdmission,
	pending durablecarrier.PendingCarrierRemoval,
	hasPending bool,
) Decision {
	if route.Status() != RouteAdmitted {
		return DecisionBlockRoute
	}
	if hasPending &&
		(!pending.RemoveRequest().Equal(route.Request()) ||
			!pending.EffectPostconditions().Equal(route.Operation().EffectPostconditions())) {
		return DecisionBlockRoute
	}
	if len(remainingConsumers(occupancy, claim)) != 0 && !route.PreservesSharedCarrier() {
		return DecisionBlockShared
	}
	return DecisionRemove
}

func validateObservation(
	identity durablecarrier.ManagedCarrierIdentity,
	observation observerelation.Correlation,
) error {
	if err := observation.Key.Validate(); err != nil {
		return fmt.Errorf("carrier absence observation key: %w", err)
	}
	if observation.Key.Subject() != identity.RelationSubject() ||
		!observation.Key.ExpectedRelation().Equal(identity.ExpectedRelation()) {
		return fmt.Errorf("carrier absence observation key does not match claim")
	}
	correlation := observation.Result
	if correlation.EvidenceAvailability() == "" || correlation.EvidenceFreshness() == "" {
		return fmt.Errorf("carrier absence observation result is invalid")
	}
	switch correlation.State() {
	case observerelation.StateMissing,
		observerelation.StateExactCorrelation,
		observerelation.StateStaleEvidence,
		observerelation.StateUnsupported,
		observerelation.StateUnavailableEvidence,
		observerelation.StateUnkeyedSameSubject,
		observerelation.StateSameSubjectShadow,
		observerelation.StateManagedKeyDrift,
		observerelation.StateAmbiguous:
	default:
		return fmt.Errorf("carrier absence correlation state %q is unsupported", correlation.State())
	}
	if correlation.State() != observerelation.StateExactCorrelation {
		return nil
	}
	sameSubject := correlation.SameSubjectRows()
	managed := correlation.ManagedKeyRows()
	if len(sameSubject) != 1 || len(managed) != 1 {
		return fmt.Errorf("exact carrier absence correlation is not singular")
	}
	if sameSubject[0].SubjectKey() != identity.ExpectedRelation().SubjectKey() {
		return fmt.Errorf("exact carrier absence correlation subject does not match claim")
	}
	managedKey, ok := managed[0].ManagedInstanceKey()
	if !ok || managedKey != identity.ExpectedRelation().ManagedInstanceKey() {
		return fmt.Errorf("exact carrier absence correlation managed key does not match claim")
	}
	return nil
}

func observationProvided(observation observerelation.Correlation) bool {
	return observation.Key.Subject().Kind() != "" || observation.Result.State() != ""
}

func pendingPointer(
	pending durablecarrier.PendingCarrierRemoval,
	present bool,
) *durablecarrier.PendingCarrierRemoval {
	if !present {
		return nil
	}
	return &pending
}

func occupancyContainsClaim(occupancy durablecarrier.CarrierOccupancy, claim durablecarrier.ManagedCarrierClaim) bool {
	for _, consumer := range occupancy.DaemKnownConsumers() {
		if consumerMatchesClaim(consumer, claim) {
			return true
		}
	}
	return false
}

func remainingConsumers(
	occupancy durablecarrier.CarrierOccupancy,
	claim durablecarrier.ManagedCarrierClaim,
) []durablecarrier.CarrierConsumer {
	consumers := occupancy.DaemKnownConsumers()
	remaining := make([]durablecarrier.CarrierConsumer, 0, len(consumers)-1)
	for _, consumer := range consumers {
		if !consumerMatchesClaim(consumer, claim) {
			remaining = append(remaining, consumer)
		}
	}
	return remaining
}

func consumerMatchesClaim(
	consumer durablecarrier.CarrierConsumer,
	claim durablecarrier.ManagedCarrierClaim,
) bool {
	return consumer.Owner().ExactEqual(claim.Owner()) &&
		consumer.RelationSubject() == claim.Identity().RelationSubject() &&
		consumer.ManagedInstanceKey() == claim.Identity().ExpectedRelation().ManagedInstanceKey()
}
