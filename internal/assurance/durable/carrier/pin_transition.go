package carrier

import (
	"fmt"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
)

// PinTransition retains the exact old management and the intended new relation.
// It is neither current observation nor permission to invoke the host.
type PinTransition struct {
	before ManagedCarrierClaim
	after  pinTarget
}

type pinTarget struct {
	identity ManagedCarrierIdentity
	request  realizationdelegate.Request
}

// NewPinTransition admits only same-subject, same-scope Pi Git commit changes.
func NewPinTransition(before ManagedCarrierClaim, identity ManagedCarrierIdentity, request realizationdelegate.Request) (PinTransition, error) {
	transition := PinTransition{before: before, after: pinTarget{identity: identity, request: request}}
	if err := transition.Validate(); err != nil {
		return PinTransition{}, err
	}
	return transition, nil
}

// PinTransitionTo derives a transition from the exact current locked target.
func (claim ManagedCarrierClaim) PinTransitionTo(locked lock.LockedSubjectContract) (PinTransition, error) {
	identity, admitted, err := ManagedCarrierIdentityFromLockedRecord(locked)
	if err != nil || !admitted {
		return PinTransition{}, fmt.Errorf("pin transition requires a locked carrier relation")
	}
	request, err := lock.DelegatedOperationRequest(locked, lock.OperationInstall)
	if err != nil {
		return PinTransition{}, err
	}
	before := claim
	before.pendingPin = nil
	transition, err := NewPinTransition(before, identity, request)
	if err != nil {
		return PinTransition{}, err
	}
	if pending, present := claim.PendingPinTransition(); present && !pending.ExactEqual(transition) {
		return PinTransition{}, fmt.Errorf("pending pin transition requires its original target")
	}
	return transition, nil
}

func (transition PinTransition) Validate() error {
	if transition.before.pendingPin != nil {
		return fmt.Errorf("pin transition requires stable prior management")
	}
	if err := transition.before.Validate(); err != nil {
		return err
	}
	if err := transition.after.identity.Validate(); err != nil {
		return err
	}
	if err := transition.after.request.Validate(); err != nil {
		return err
	}
	before, after := transition.before.Identity(), transition.after.identity
	if !before.Carrier().IsPiGitCommitPinChangeTo(after.Carrier()) ||
		before.RelationSubject() != after.RelationSubject() ||
		transition.before.InstallRequest().RouteID() != transition.after.request.RouteID() ||
		transition.before.InstallRequest().ContractVersion() != transition.after.request.ContractVersion() {
		return fmt.Errorf("pin transition requires only a Pi Git commit pin change at the same owner relation and scope")
	}
	for _, facts := range []pinTarget{{identity: before, request: transition.before.InstallRequest()}, transition.after} {
		contract, err := piPinContract(facts.identity)
		if err != nil || !facts.identity.MatchesLockedRecord(contract, facts.request) {
			return fmt.Errorf("pin transition requires exact canonical Pi install contracts")
		}
	}
	return nil
}

func piPinContract(identity ManagedCarrierIdentity) (lock.LockedSubjectContract, error) {
	if string(identity.ExpectedRelation().SubjectKey()) != identity.Carrier().Source().Ref() {
		return lock.LockedSubjectContract{}, fmt.Errorf("pin transition relation must name its exact source")
	}
	return lock.ReconstructDelegatedRelationCarrierContract(identity.Carrier().Key(), identity.RelationSubject(), identity.ExpectedRelation().SubjectKey())
}

func (transition PinTransition) Before() ManagedCarrierClaim { return transition.before }

func (transition PinTransition) Identity() ManagedCarrierIdentity { return transition.after.identity }

func (transition PinTransition) Request() realizationdelegate.Request {
	return transition.after.request
}

func (transition PinTransition) ExactEqual(other PinTransition) bool {
	return transition.before.ExactEqual(other.before) && transition.after.equal(other.after)
}

func (target pinTarget) equal(other pinTarget) bool {
	return target.identity.ExactEqual(other.identity) && target.request.Equal(other.request)
}

// PendingClaim retains old acquisition facts and reserves this exact target.
func (transition PinTransition) PendingClaim() (ManagedCarrierClaim, error) {
	if err := transition.Validate(); err != nil {
		return ManagedCarrierClaim{}, err
	}
	claim := transition.before
	target := transition.after
	claim.pendingPin = &target
	return claim, nil
}

// CompletedClaim requires fresh exact target evidence. The effect owner must
// additionally require a successful newly authorized native attempt.
func (transition PinTransition) CompletedClaim(observation observerelation.CorrelationResult) (ManagedCarrierClaim, error) {
	if err := transition.Validate(); err != nil {
		return ManagedCarrierClaim{}, err
	}
	if err := validateFreshExactRelationEvidence(transition.Identity(), observation); err != nil {
		return ManagedCarrierClaim{}, err
	}
	return NewManagedCarrierClaim(transition.before.Owner(), transition.Identity(), transition.Request(), ClaimProvenancePinTransitionObserved)
}

func (claim ManagedCarrierClaim) PendingPinTransition() (PinTransition, bool) {
	if claim.pendingPin == nil {
		return PinTransition{}, false
	}
	before := claim
	before.pendingPin = nil
	return PinTransition{before: before, after: *claim.pendingPin}, true
}

// WithOwner preserves the complete claim, including a pending transition.
func (claim ManagedCarrierClaim) WithOwner(owner stateauthority.Authority) (ManagedCarrierClaim, error) {
	claim.owner = owner
	if err := claim.Validate(); err != nil {
		return ManagedCarrierClaim{}, err
	}
	return claim, nil
}

// RequireStable prevents ordinary claim retirement from erasing uncertain effects.
func (claim ManagedCarrierClaim) RequireStable() error {
	if claim.pendingPin != nil {
		return fmt.Errorf("pending Pi pin transition requires the original manifest target and a newly authorized apply")
	}
	return nil
}

func (claim ManagedCarrierClaim) pendingPinEqual(other ManagedCarrierClaim) bool {
	if claim.pendingPin == nil || other.pendingPin == nil {
		return claim.pendingPin == nil && other.pendingPin == nil
	}
	return claim.pendingPin.equal(*other.pendingPin)
}

// SharesPiGitFootprint is conflict evidence only, never exact relation equality.
// Callers supply claims from the selected project and shared global registry.
func SharesPiGitFootprint(left, right ManagedCarrierIdentity) bool {
	return left.Scope() == right.Scope() && SharesPiGitRepository(left, right)
}

// SharesPiGitRepository covers Pi update's repository selector across both scopes.
// It is conflict evidence, not authority to change either exact source.
func SharesPiGitRepository(left, right ManagedCarrierIdentity) bool {
	first, firstPi := left.Carrier().PiGitIdentity()
	second, secondPi := right.Carrier().PiGitIdentity()
	return firstPi && secondPi && first == second
}

// Reserve replaces only the exact old claim. An identical reservation is reusable.
func (transition PinTransition) Reserve(claims []ManagedCarrierClaim) ([]ManagedCarrierClaim, error) {
	pending, err := transition.PendingClaim()
	if err != nil {
		return nil, err
	}
	for _, claim := range claims {
		if claim.FactKey() != transition.before.FactKey() && SharesPiGitFootprint(claim.Identity(), transition.Identity()) {
			return nil, fmt.Errorf("Pi pin transition has another managed checkout consumer")
		}
	}
	return replacePinClaim(claims, transition.before, pending, true)
}

// Complete replaces a retained reservation only after exact new observation.
func (transition PinTransition) Complete(claims []ManagedCarrierClaim, observation observerelation.CorrelationResult) ([]ManagedCarrierClaim, error) {
	pending, err := transition.PendingClaim()
	if err != nil {
		return nil, err
	}
	completed, err := transition.CompletedClaim(observation)
	if err != nil {
		return nil, err
	}
	return replacePinClaim(claims, pending, completed, false)
}

func replacePinClaim(claims []ManagedCarrierClaim, before, after ManagedCarrierClaim, repeat bool) ([]ManagedCarrierClaim, error) {
	next := append([]ManagedCarrierClaim(nil), claims...)
	found := false
	for index, claim := range next {
		if claim.FactKey() != before.FactKey() {
			continue
		}
		if found || (!claim.ExactEqual(before) && !(repeat && claim.ExactEqual(after))) {
			return nil, fmt.Errorf("Pi pin transition conflicts with retained management")
		}
		found = true
		next[index] = after
	}
	if !found {
		return nil, fmt.Errorf("Pi pin transition has no exact retained management")
	}
	return next, nil
}

func (registry GlobalCarrierClaims) WithReservedPinTransition(transition PinTransition) (GlobalCarrierClaims, error) {
	claims, err := transition.Reserve(registry.claims)
	if err != nil {
		return GlobalCarrierClaims{}, err
	}
	return NewGlobalCarrierClaims(claims)
}

func (registry GlobalCarrierClaims) WithCompletedPinTransition(transition PinTransition, observation observerelation.CorrelationResult) (GlobalCarrierClaims, error) {
	claims, err := transition.Complete(registry.claims, observation)
	if err != nil {
		return GlobalCarrierClaims{}, err
	}
	return NewGlobalCarrierClaims(claims)
}
