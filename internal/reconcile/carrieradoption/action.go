package carrieradoption

import (
	"cmp"
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// Result identifies one mutually exclusive carrier-adoption outcome.
type Result string

const (
	ResultEligibleExactRelation      Result = "eligible_exact_relation"
	ResultAlreadyClaimedCurrent      Result = "already_claimed_current"
	ResultPresentUnclaimed           Result = "present_unclaimed"
	ResultPresentUnclaimedIneligible Result = "present_unclaimed_ineligible"
	ResultMissingRelation            Result = "missing_relation"
	ResultInexactRelation            Result = "inexact_relation"
	ResultObservationBlocked         Result = "observation_blocked"
	ResultClaimConflict              Result = "claim_conflict"
)

// PlanIdentity is the immutable semantic identity of one adoption decision.
type PlanIdentity string

// ActionInput contains the canonical facts required to classify one selected
// locked carrier relation.
type ActionInput struct {
	Locked         lock.LockedSubjectContract
	Observation    observerelation.CorrelationResult
	CurrentOwner   durablecarrier.StateAuthority
	Claims         []durablecarrier.ManagedCarrierClaim
	Lifecycle      Lifecycle
	ManageExisting bool
}

// Action is one immutable state-only adoption decision. It never grants host
// route execution and contains a proposed claim only for an eligible explicit
// request.
type Action struct {
	locked            lock.LockedSubjectContract
	identity          durablecarrier.ManagedCarrierIdentity
	acquisition       realizationdelegate.Request
	observation       observerelation.CorrelationResult
	owner             durablecarrier.StateAuthority
	claims            []durablecarrier.ManagedCarrierClaim
	occupancy         durablecarrier.CarrierOccupancy
	lifecycle         Lifecycle
	manageExisting    bool
	result            Result
	currentClaim      durablecarrier.ManagedCarrierClaim
	hasCurrentClaim   bool
	conflictingClaims []durablecarrier.ManagedCarrierClaim
	proposedClaim     durablecarrier.ManagedCarrierClaim
	hasProposedClaim  bool
	planIdentity      PlanIdentity
}

// NewAction validates the complete decision basis and classifies it in the
// normative observation -> claim -> lifecycle -> intent order.
func NewAction(input ActionInput) (Action, error) {
	if err := input.CurrentOwner.Validate(); err != nil {
		return Action{}, fmt.Errorf("carrier adoption owner: %w", err)
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(input.Locked)
	if err != nil {
		return Action{}, fmt.Errorf("carrier adoption identity: %w", err)
	}
	if !admitted {
		return Action{}, fmt.Errorf("carrier adoption requires a locked carrier relation")
	}
	acquisition, err := lock.DelegatedOperationRequest(input.Locked, lock.OperationInstall)
	if err != nil {
		return Action{}, fmt.Errorf("carrier adoption acquisition request: %w", err)
	}
	if err := validateCorrelation(identity, input.Observation); err != nil {
		return Action{}, fmt.Errorf("carrier adoption observation: %w", err)
	}
	if err := input.Lifecycle.Validate(); err != nil {
		return Action{}, fmt.Errorf("carrier adoption lifecycle: %w", err)
	}
	if !lifecycleMatchesLocked(input.Lifecycle, input.Locked) {
		return Action{}, fmt.Errorf("carrier adoption lifecycle belongs to another locked relation")
	}
	claims, err := canonicalClaims(input.Claims)
	if err != nil {
		return Action{}, err
	}
	relevantClaims := relevantCarrierAdoptionClaims(input.CurrentOwner, identity, claims)
	occupancy, err := durablecarrier.NewCarrierOccupancy(identity.Carrier(), relevantClaims)
	if err != nil {
		return Action{}, fmt.Errorf("carrier adoption occupancy: %w", err)
	}
	assessment := assessClaims(input.CurrentOwner, identity, input.Locked, relevantClaims)

	action := Action{
		locked:            input.Locked,
		identity:          identity,
		acquisition:       acquisition,
		observation:       input.Observation,
		owner:             input.CurrentOwner,
		claims:            relevantClaims,
		occupancy:         occupancy,
		lifecycle:         input.Lifecycle,
		manageExisting:    input.ManageExisting,
		currentClaim:      assessment.current,
		hasCurrentClaim:   assessment.hasCurrent,
		conflictingClaims: assessment.conflicts,
	}
	if result, terminal, err := classifyObservation(identity, input.Observation); err != nil {
		return Action{}, err
	} else if terminal {
		action.result = result
	} else {
		switch {
		case len(action.conflictingClaims) != 0:
			action.result = ResultClaimConflict
		case action.hasCurrentClaim:
			action.result = ResultAlreadyClaimedCurrent
		case !input.Lifecycle.Eligible():
			action.result = ResultPresentUnclaimedIneligible
		case !input.ManageExisting:
			action.result = ResultPresentUnclaimed
		default:
			proposed, err := durablecarrier.ClaimFromObservedAdoption(
				input.CurrentOwner,
				input.Locked,
				input.Observation,
			)
			if err != nil {
				return Action{}, fmt.Errorf("construct proposed adopted claim: %w", err)
			}
			action.result = ResultEligibleExactRelation
			action.proposedClaim = proposed
			action.hasProposedClaim = true
		}
	}
	identityValue, err := planIdentityFor(action)
	if err != nil {
		return Action{}, err
	}
	action.planIdentity = identityValue
	return action, nil
}

// Validate rejects forged or non-canonical actions.
func (action Action) Validate() error {
	rebuilt, err := NewAction(ActionInput{
		Locked:         action.locked,
		Observation:    action.observation,
		CurrentOwner:   action.owner,
		Claims:         action.claims,
		Lifecycle:      action.lifecycle,
		ManageExisting: action.manageExisting,
	})
	if err != nil {
		return err
	}
	if action.result != rebuilt.result ||
		action.hasCurrentClaim != rebuilt.hasCurrentClaim ||
		action.hasProposedClaim != rebuilt.hasProposedClaim ||
		action.planIdentity != rebuilt.planIdentity {
		return fmt.Errorf("carrier adoption action does not match canonical classification")
	}
	if !action.identity.ExactEqual(rebuilt.identity) ||
		!action.acquisition.Equal(rebuilt.acquisition) {
		return fmt.Errorf("carrier adoption action identity is not canonical")
	}
	if !equalClaims(action.claims, rebuilt.claims) ||
		!equalOccupancy(action.occupancy, rebuilt.occupancy) {
		return fmt.Errorf("carrier adoption action claim basis is not canonical")
	}
	if action.hasCurrentClaim && !action.currentClaim.ExactEqual(rebuilt.currentClaim) {
		return fmt.Errorf("carrier adoption current claim is not canonical")
	}
	if action.hasProposedClaim && !action.proposedClaim.ExactEqual(rebuilt.proposedClaim) {
		return fmt.Errorf("carrier adoption proposed claim is not canonical")
	}
	if !equalClaims(action.conflictingClaims, rebuilt.conflictingClaims) {
		return fmt.Errorf("carrier adoption conflicting claims are not canonical")
	}
	return nil
}

// Compare returns canonical target/scope/relation ordering.
func (action Action) Compare(other Action) int {
	if order := cmp.Compare(action.Target(), other.Target()); order != 0 {
		return order
	}
	if order := cmp.Compare(action.Scope(), other.Scope()); order != 0 {
		return order
	}
	if order := topology.CompareSubjectID(action.Subject(), other.Subject()); order != 0 {
		return order
	}
	if order := cmp.Compare(
		action.identity.ExpectedRelation().SubjectKey(),
		other.identity.ExpectedRelation().SubjectKey(),
	); order != 0 {
		return order
	}
	return cmp.Compare(
		action.identity.ExpectedRelation().ManagedInstanceKey(),
		other.identity.ExpectedRelation().ManagedInstanceKey(),
	)
}

// Result returns the closed adoption outcome.
func (action Action) Result() Result { return action.result }

// Subject returns the declaration-local host relation subject.
func (action Action) Subject() topology.SubjectID { return action.identity.RelationSubject() }

// Target returns the selected host.
func (action Action) Target() target.Target { return action.identity.Target() }

// Scope returns the selected host locality.
func (action Action) Scope() target.Scope { return action.identity.Scope() }

// CarrierIdentity returns the exact structural carrier and relation identity.
func (action Action) CarrierIdentity() durablecarrier.ManagedCarrierIdentity { return action.identity }

// AcquisitionRequest returns the locked install-request identity inherited by a claim.
func (action Action) AcquisitionRequest() realizationdelegate.Request { return action.acquisition }

// Observation returns the unchanged passive correlation fact.
func (action Action) Observation() observerelation.CorrelationResult { return action.observation }

// CurrentOwner returns the selected durable state authority.
func (action Action) CurrentOwner() durablecarrier.StateAuthority { return action.owner }

// Occupancy returns the daem-known consumers of the exact structural carrier.
func (action Action) Occupancy() durablecarrier.CarrierOccupancy { return action.occupancy }

// Lifecycle returns the complete future-lifecycle decision basis.
func (action Action) Lifecycle() Lifecycle { return action.lifecycle }

// ManageExisting reports whether explicit claim acquisition was requested.
func (action Action) ManageExisting() bool { return action.manageExisting }

// CurrentClaim returns the exact selected-owner claim when already managed.
func (action Action) CurrentClaim() (durablecarrier.ManagedCarrierClaim, bool) {
	return action.currentClaim, action.hasCurrentClaim
}

// ConflictingClaims returns durable facts that prevent claim acquisition.
func (action Action) ConflictingClaims() []durablecarrier.ManagedCarrierClaim {
	return append([]durablecarrier.ManagedCarrierClaim(nil), action.conflictingClaims...)
}

// ProposedClaim returns the exact state-only claim for an eligible explicit action.
func (action Action) ProposedClaim() (durablecarrier.ManagedCarrierClaim, bool) {
	return action.proposedClaim, action.hasProposedClaim
}

// PlanIdentity returns the semantic hash confirmed by a later mutating workflow.
func (action Action) PlanIdentity() PlanIdentity { return action.planIdentity }

// StateOnly reports whether this action requests one durable claim write.
func (action Action) StateOnly() bool { return action.result == ResultEligibleExactRelation }

// InvokesHostRoute is always false: adoption itself cannot execute install or removal.
func (action Action) InvokesHostRoute() bool { return false }

// BlocksOrdinaryApply reports whether this decision refuses unmanaged or
// contradictory exact carrier state. Missing relations remain install planning.
func (action Action) BlocksOrdinaryApply() bool {
	switch action.result {
	case ResultEligibleExactRelation,
		ResultAlreadyClaimedCurrent,
		ResultMissingRelation:
		return false
	default:
		return true
	}
}

func lifecycleMatchesLocked(lifecycle Lifecycle, locked lock.LockedSubjectContract) bool {
	return lifecycle.locked.Equal(locked)
}
