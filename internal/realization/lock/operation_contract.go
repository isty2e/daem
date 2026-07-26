package lock

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/realization/effectpostcondition"
)

// OperationKind identifies one locked operation contract.
type OperationKind string

const (
	OperationWriteProjection  OperationKind = "write_projection"
	OperationRemoveProjection OperationKind = "remove_projection"
	OperationInstall          OperationKind = "install"
	OperationEnable           OperationKind = "enable"
	OperationDisable          OperationKind = "disable"
	OperationRemove           OperationKind = "remove"
	OperationRefresh          OperationKind = "refresh"
	OperationPrune            OperationKind = "prune"
	OperationObserve          OperationKind = "observe"
	OperationAdopt            OperationKind = "adopt"
)

// ParseOperationKind validates and canonicalizes one operation kind.
func ParseOperationKind(value string) (OperationKind, error) {
	kind := OperationKind(value)
	switch kind {
	case OperationWriteProjection, OperationRemoveProjection, OperationInstall, OperationEnable,
		OperationDisable, OperationRemove, OperationRefresh, OperationPrune, OperationObserve, OperationAdopt:
		return kind, nil
	default:
		return "", fmt.Errorf("operation kind %q is unsupported", kind)
	}
}

// ActuationKind identifies how an operation may be attempted.
type ActuationKind string

const (
	ActuationDirectProjection   ActuationKind = "direct_projection"
	ActuationDelegatedHostRoute ActuationKind = "delegated_host_route"
	ActuationAssistedRoute      ActuationKind = "assisted_route"
	ActuationNoMutation         ActuationKind = "no_mutation"
)

// AuthorityKind describes the authority carried by one operation contract.
type AuthorityKind string

const (
	AuthorityNone    AuthorityKind = "none"
	AuthorityObserve AuthorityKind = "observe"
	AuthorityManage  AuthorityKind = "manage"
	AuthorityRemove  AuthorityKind = "remove"
	AuthorityAdopt   AuthorityKind = "adopt"
)

// EffectEnvelopeClass describes locked knowledge of operation effects.
type EffectEnvelopeClass string

const (
	EffectEnvelopeComplete      EffectEnvelopeClass = "complete"
	EffectEnvelopeIncomplete    EffectEnvelopeClass = "incomplete"
	EffectEnvelopeUnknown       EffectEnvelopeClass = "unknown"
	EffectEnvelopeNotApplicable EffectEnvelopeClass = "not_applicable"
)

// IdempotencyContract describes operation retry safety.
type IdempotencyContract string

const (
	Idempotent               IdempotencyContract = "idempotent"
	ConditionallyIdempotent  IdempotencyContract = "conditionally_idempotent"
	IdempotencyUnknown       IdempotencyContract = "unknown"
	IdempotencyNotApplicable IdempotencyContract = "not_applicable"
)

// VerificationContract describes the postcondition required for one operation.
type VerificationContract string

const (
	VerificationNone            VerificationContract = "none"
	VerificationExactArtifact   VerificationContract = "exact_artifact"
	VerificationExactProjection VerificationContract = "exact_projection"
	VerificationHostRelation    VerificationContract = "host_relation"
	VerificationInsufficient    VerificationContract = "insufficient"
)

// TrustActivationRequirement describes durable trust or activation gates for one operation.
type TrustActivationRequirement string

const (
	TrustActivationNotRequired TrustActivationRequirement = "not_required"
	TrustActivationRequired    TrustActivationRequirement = "required"
	TrustActivationUnknown     TrustActivationRequirement = "unknown"
)

// OperationRecoveryClass describes durable recovery expectations for one operation.
type OperationRecoveryClass string

const (
	OperationRecoveryAtomic              OperationRecoveryClass = "atomic"
	OperationRecoverySafeRetry           OperationRecoveryClass = "safe_retry"
	OperationRecoveryBoundedCompensation OperationRecoveryClass = "bounded_compensation"
	OperationRecoveryInsufficient        OperationRecoveryClass = "insufficient"
	OperationRecoveryUnknown             OperationRecoveryClass = "unknown"
	OperationRecoveryNotApplicable       OperationRecoveryClass = "not_applicable"
)

// RouteContractRef identifies the durable route/adapter contract used to interpret an operation.
type RouteContractRef struct {
	RouteID                string
	AdapterContractVersion string
}

// HostCompatibilityConstraint records durable host compatibility constraints, not current host state.
type HostCompatibilityConstraint struct {
	HostVersionConstraint string
	ConfigFormatVersion   string
}

// OperationContractInput is typed constructor input for OperationContract.
type OperationContractInput struct {
	Operation            OperationKind
	Actuation            ActuationKind
	Authority            AuthorityKind
	Route                RouteContractRef
	HostCompatibility    HostCompatibilityConstraint
	Preconditions        []string
	EffectEnvelope       EffectEnvelopeClass
	EffectPostconditions []effectpostcondition.Requirement
	Idempotency          IdempotencyContract
	Verification         VerificationContract
	TrustActivation      TrustActivationRequirement
	Recovery             OperationRecoveryClass
}

// OperationContract records durable constraints for one operation only.
type OperationContract struct {
	operation            OperationKind
	actuation            ActuationKind
	authority            AuthorityKind
	route                RouteContractRef
	hostCompatibility    HostCompatibilityConstraint
	preconditions        []string
	effectEnvelope       EffectEnvelopeClass
	effectPostconditions effectpostcondition.Set
	idempotency          IdempotencyContract
	verification         VerificationContract
	trustActivation      TrustActivationRequirement
	recovery             OperationRecoveryClass
}

// NewOperationContract constructs a single-operation lock contract.
func NewOperationContract(input OperationContractInput) (OperationContract, error) {
	effectPostconditions, err := effectpostcondition.NewSet(input.EffectPostconditions)
	if err != nil {
		return OperationContract{}, err
	}
	contract := OperationContract{
		operation: input.Operation,
		actuation: input.Actuation,
		authority: input.Authority,
		route: RouteContractRef{
			RouteID:                strings.TrimSpace(input.Route.RouteID),
			AdapterContractVersion: strings.TrimSpace(input.Route.AdapterContractVersion),
		},
		hostCompatibility: HostCompatibilityConstraint{
			HostVersionConstraint: strings.TrimSpace(input.HostCompatibility.HostVersionConstraint),
			ConfigFormatVersion:   strings.TrimSpace(input.HostCompatibility.ConfigFormatVersion),
		},
		preconditions:        normalizeStrings(input.Preconditions),
		effectEnvelope:       input.EffectEnvelope,
		effectPostconditions: effectPostconditions,
		idempotency:          input.Idempotency,
		verification:         input.Verification,
		trustActivation:      input.TrustActivation,
		recovery:             input.Recovery,
	}
	if err := contract.validate(); err != nil {
		return OperationContract{}, err
	}
	return contract, nil
}

// Operation returns the operation this contract covers.
func (contract OperationContract) Operation() OperationKind {
	return contract.operation
}

// Actuation returns the actuation kind for this operation.
func (contract OperationContract) Actuation() ActuationKind {
	return contract.actuation
}

// Authority returns the locked authority for this operation.
func (contract OperationContract) Authority() AuthorityKind {
	return contract.authority
}

// Route returns the durable route or adapter contract reference.
func (contract OperationContract) Route() RouteContractRef {
	return contract.route
}

// HostCompatibility returns durable host compatibility constraints.
func (contract OperationContract) HostCompatibility() HostCompatibilityConstraint {
	return contract.hostCompatibility
}

// Preconditions returns deterministic operation preconditions.
func (contract OperationContract) Preconditions() []string {
	return append([]string(nil), contract.preconditions...)
}

// EffectEnvelope returns the locked effect envelope class.
func (contract OperationContract) EffectEnvelope() EffectEnvelopeClass {
	return contract.effectEnvelope
}

// EffectPostconditions returns the route-coupled effect facts that require
// fresh post-attempt evidence in addition to the primary verification class.
func (contract OperationContract) EffectPostconditions() effectpostcondition.Set {
	return contract.effectPostconditions
}

// Idempotency returns the locked idempotency contract.
func (contract OperationContract) Idempotency() IdempotencyContract {
	return contract.idempotency
}

// Verification returns the locked verification contract.
func (contract OperationContract) Verification() VerificationContract {
	return contract.verification
}

// TrustActivation returns the locked trust or activation requirement.
func (contract OperationContract) TrustActivation() TrustActivationRequirement {
	return contract.trustActivation
}

// Recovery returns the locked operation recovery class.
func (contract OperationContract) Recovery() OperationRecoveryClass {
	return contract.recovery
}

// OrdinaryMutationEligible reports whether this locked operation contract is sufficient for ordinary mutation before current evidence is considered.
func (contract OperationContract) OrdinaryMutationEligible() bool {
	if contract.actuation == ActuationNoMutation || contract.actuation == ActuationAssistedRoute {
		return false
	}
	if contract.authority == AuthorityNone || contract.authority == AuthorityObserve {
		return false
	}
	if contract.effectEnvelope != EffectEnvelopeComplete {
		return false
	}
	if contract.idempotency == IdempotencyUnknown || contract.idempotency == IdempotencyNotApplicable {
		return false
	}
	if contract.verification == VerificationNone || contract.verification == VerificationInsufficient {
		return false
	}
	if contract.trustActivation == TrustActivationUnknown {
		return false
	}
	if contract.recovery == OperationRecoveryUnknown || contract.recovery == OperationRecoveryInsufficient ||
		contract.recovery == OperationRecoveryNotApplicable {
		return false
	}
	return true
}

func (contract OperationContract) validate() error {
	if err := validateOperationKind(contract.operation); err != nil {
		return err
	}
	if err := validateActuationKind(contract.actuation); err != nil {
		return err
	}
	if err := validateAuthorityKind(contract.authority); err != nil {
		return err
	}
	if err := validateRouteContractRef(contract.route, contract.actuation); err != nil {
		return err
	}
	if err := validateStringSet(contract.preconditions, "operation precondition"); err != nil {
		return err
	}
	if err := validateEffectEnvelope(contract.effectEnvelope); err != nil {
		return err
	}
	if err := contract.effectPostconditions.Validate(); err != nil {
		return err
	}
	if !contract.effectPostconditions.Empty() {
		if contract.operation != OperationRemove {
			return fmt.Errorf("effect postconditions require remove operation")
		}
		if contract.actuation != ActuationDelegatedHostRoute {
			return fmt.Errorf("effect postconditions require delegated host actuation")
		}
		if contract.authority != AuthorityRemove {
			return fmt.Errorf("effect postconditions require remove authority")
		}
		if contract.effectEnvelope != EffectEnvelopeComplete {
			return fmt.Errorf("effect postconditions require a complete effect envelope")
		}
		if contract.verification != VerificationHostRelation {
			return fmt.Errorf("effect postconditions require host-relation verification")
		}
	}
	if err := validateIdempotencyContract(contract.idempotency); err != nil {
		return err
	}
	if err := validateVerificationContract(contract.verification); err != nil {
		return err
	}
	if err := validateTrustActivationRequirement(contract.trustActivation); err != nil {
		return err
	}
	if err := validateOperationRecoveryClass(contract.recovery); err != nil {
		return err
	}
	if contract.operation == OperationObserve && contract.actuation != ActuationNoMutation {
		return fmt.Errorf("observe operation must use no-mutation actuation")
	}
	if contract.actuation == ActuationNoMutation && contract.authority != AuthorityNone && contract.authority != AuthorityObserve {
		return fmt.Errorf("no-mutation operation must not grant mutation authority")
	}
	return nil
}

func validateOperationKind(kind OperationKind) error {
	_, err := ParseOperationKind(string(kind))
	return err
}

func validateActuationKind(kind ActuationKind) error {
	switch kind {
	case ActuationDirectProjection, ActuationDelegatedHostRoute, ActuationAssistedRoute, ActuationNoMutation:
		return nil
	default:
		return fmt.Errorf("actuation kind %q is unsupported", kind)
	}
}

func validateAuthorityKind(kind AuthorityKind) error {
	switch kind {
	case AuthorityNone, AuthorityObserve, AuthorityManage, AuthorityRemove, AuthorityAdopt:
		return nil
	default:
		return fmt.Errorf("authority kind %q is unsupported", kind)
	}
}

func validateRouteContractRef(route RouteContractRef, actuation ActuationKind) error {
	if actuation == ActuationNoMutation {
		if route.RouteID != "" || route.AdapterContractVersion != "" {
			return fmt.Errorf("no-mutation operation must not carry route contract")
		}
		return nil
	}
	if strings.TrimSpace(route.RouteID) == "" {
		return fmt.Errorf("operation route id is required")
	}
	if strings.TrimSpace(route.AdapterContractVersion) == "" {
		return fmt.Errorf("operation adapter contract version is required")
	}
	return nil
}

func validateEffectEnvelope(value EffectEnvelopeClass) error {
	switch value {
	case EffectEnvelopeComplete, EffectEnvelopeIncomplete, EffectEnvelopeUnknown, EffectEnvelopeNotApplicable:
		return nil
	default:
		return fmt.Errorf("effect envelope %q is unsupported", value)
	}
}

func validateIdempotencyContract(value IdempotencyContract) error {
	switch value {
	case Idempotent, ConditionallyIdempotent, IdempotencyUnknown, IdempotencyNotApplicable:
		return nil
	default:
		return fmt.Errorf("idempotency contract %q is unsupported", value)
	}
}

func validateVerificationContract(value VerificationContract) error {
	switch value {
	case VerificationNone, VerificationExactArtifact, VerificationExactProjection, VerificationHostRelation, VerificationInsufficient:
		return nil
	default:
		return fmt.Errorf("verification contract %q is unsupported", value)
	}
}

func validateTrustActivationRequirement(value TrustActivationRequirement) error {
	switch value {
	case TrustActivationNotRequired, TrustActivationRequired, TrustActivationUnknown:
		return nil
	default:
		return fmt.Errorf("trust activation requirement %q is unsupported", value)
	}
}

func validateOperationRecoveryClass(value OperationRecoveryClass) error {
	switch value {
	case OperationRecoveryAtomic, OperationRecoverySafeRetry, OperationRecoveryBoundedCompensation,
		OperationRecoveryInsufficient, OperationRecoveryUnknown, OperationRecoveryNotApplicable:
		return nil
	default:
		return fmt.Errorf("operation recovery class %q is unsupported", value)
	}
}

func cloneOperationContract(contract OperationContract) OperationContract {
	cloned := contract
	cloned.route = RouteContractRef{
		RouteID:                strings.TrimSpace(contract.route.RouteID),
		AdapterContractVersion: strings.TrimSpace(contract.route.AdapterContractVersion),
	}
	cloned.hostCompatibility = HostCompatibilityConstraint{
		HostVersionConstraint: strings.TrimSpace(contract.hostCompatibility.HostVersionConstraint),
		ConfigFormatVersion:   strings.TrimSpace(contract.hostCompatibility.ConfigFormatVersion),
	}
	cloned.preconditions = normalizeStrings(contract.preconditions)
	return cloned
}
