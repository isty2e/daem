package lock

import (
	"fmt"
	"sort"
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

type delegatedRemovalSpec struct {
	operation              OperationContractInput
	preservesSharedCarrier bool
	removedEffects         []string
	retainedEffects        []string
	nonClaims              []string
}

// DelegatedRemovalContract is the current operation-specific removal profile
// resolved for one exact locked carrier. It is not durable ownership or
// current host evidence.
type DelegatedRemovalContract struct {
	operation              OperationContract
	request                realizationdelegate.Request
	preservesSharedCarrier bool
	removedEffects         []string
	retainedEffects        []string
	nonClaims              []string
}

// ResolveDelegatedCarrierRemoval derives current removal capability from exact
// structural carrier facts plus acquisition provenance. It does not require a
// current desired lock record and does not persist the derived route.
func ResolveDelegatedCarrierRemoval(
	carrier desiredextension.CarrierKey,
	subject topology.SubjectID,
	expected hostrelation.ExpectedRelation,
	installRequest realizationdelegate.Request,
) (DelegatedRemovalContract, bool, error) {
	if err := installRequest.Validate(); err != nil {
		return DelegatedRemovalContract{}, false, fmt.Errorf(
			"delegated removal install request: %w",
			err,
		)
	}
	spec, ok := delegatedRelationCarrierSpecFor(carrier.Carrier())
	if !ok {
		return DelegatedRemovalContract{}, false, nil
	}
	if err := validateDelegatedRelationCarrierSpec(spec); err != nil {
		return DelegatedRemovalContract{}, true, err
	}
	if err := validateDelegatedRelationCarrierIdentity(carrier, subject, spec); err != nil {
		return DelegatedRemovalContract{}, true, err
	}
	canonicalExpected, err := hostrelation.Derive(carrier, subject, expected.SubjectKey())
	if err != nil {
		return DelegatedRemovalContract{}, true, err
	}
	if !expected.Equal(canonicalExpected) {
		return DelegatedRemovalContract{}, true, fmt.Errorf(
			"delegated removal expected relation does not match carrier identity",
		)
	}
	realizationSpec, err := delegatedRelationCarrierRealization(
		carrier,
		subject,
		canonicalExpected,
		spec,
	)
	if err != nil {
		return DelegatedRemovalContract{}, true, err
	}
	relationSpec, ok := realizationSpec.DelegatedRelation()
	if !ok {
		return DelegatedRemovalContract{}, true, fmt.Errorf(
			"delegated removal current profile has no relation realization",
		)
	}
	operationInputs, err := delegatedRelationOperationInputs(spec, carrier)
	if err != nil {
		return DelegatedRemovalContract{}, true, err
	}
	operationContracts, err := delegatedRelationOperationContracts(spec.Profile, operationInputs)
	if err != nil {
		return DelegatedRemovalContract{}, true, err
	}
	var install OperationContract
	var remove OperationContract
	for _, operation := range operationContracts {
		switch operation.Operation() {
		case OperationInstall:
			install = operation
		case OperationRemove:
			remove = operation
		}
	}
	currentInstallRequest, err := delegatedOperationRequest(
		subject,
		relationSpec,
		install,
		OperationInstall,
	)
	if err != nil {
		return DelegatedRemovalContract{}, true, err
	}
	if !installRequest.Equal(currentInstallRequest) {
		return DelegatedRemovalContract{}, false, nil
	}
	if remove.Operation() == "" {
		return DelegatedRemovalContract{}, false, nil
	}
	removeRequest, err := delegatedOperationRequest(
		subject,
		relationSpec,
		remove,
		OperationRemove,
	)
	if err != nil {
		return DelegatedRemovalContract{}, true, err
	}
	dossier, admitted, err := spec.Profile.RemovalDossier(carrier)
	if err != nil || !admitted {
		return DelegatedRemovalContract{}, admitted, err
	}
	resolved, err := newDelegatedRemovalContract(
		remove,
		removeRequest,
		delegatedRemovalSpecFromDossier(dossier),
	)
	if err != nil {
		return DelegatedRemovalContract{}, true, err
	}
	return resolved, true, nil
}

func newDelegatedRemovalContract(
	operation OperationContract,
	request realizationdelegate.Request,
	spec delegatedRemovalSpec,
) (DelegatedRemovalContract, error) {
	if operation.Operation() != OperationRemove ||
		(operation.Actuation() != ActuationDelegatedHostRoute &&
			operation.Actuation() != ActuationDirectProjection) ||
		operation.Authority() != AuthorityRemove ||
		!operation.OrdinaryMutationEligible() {
		return DelegatedRemovalContract{}, fmt.Errorf(
			"delegated removal requires an ordinary-mutation-eligible remove operation",
		)
	}
	if err := request.Validate(); err != nil {
		return DelegatedRemovalContract{}, fmt.Errorf("delegated removal request: %w", err)
	}
	route := operation.Route()
	if request.RouteID() != route.RouteID ||
		request.ContractVersion() != route.AdapterContractVersion {
		return DelegatedRemovalContract{}, fmt.Errorf(
			"delegated removal request does not match its operation route",
		)
	}
	removed, err := canonicalRemovalTokens("removed effect", spec.removedEffects, true)
	if err != nil {
		return DelegatedRemovalContract{}, err
	}
	retained, err := canonicalRemovalTokens("retained effect", spec.retainedEffects, false)
	if err != nil {
		return DelegatedRemovalContract{}, err
	}
	nonClaims, err := canonicalRemovalTokens("removal non-claim", spec.nonClaims, false)
	if err != nil {
		return DelegatedRemovalContract{}, err
	}
	return DelegatedRemovalContract{
		operation:              operation,
		request:                request,
		preservesSharedCarrier: spec.preservesSharedCarrier,
		removedEffects:         removed,
		retainedEffects:        retained,
		nonClaims:              nonClaims,
	}, nil
}

// Operation returns the exact current remove operation contract.
func (contract DelegatedRemovalContract) Operation() OperationContract {
	return contract.operation
}

// Request returns the operation-indexed delegated route identity.
func (contract DelegatedRemovalContract) Request() realizationdelegate.Request {
	return contract.request
}

// PreservesSharedCarrier reports whether the route is safe with another
// daem-known consumer of the exact structural carrier.
func (contract DelegatedRemovalContract) PreservesSharedCarrier() bool {
	return contract.preservesSharedCarrier
}

// RemovedEffects returns the complete bounded deletion disclosure.
func (contract DelegatedRemovalContract) RemovedEffects() []string {
	return append([]string(nil), contract.removedEffects...)
}

// RetainedEffects returns the route effects deliberately left in place.
func (contract DelegatedRemovalContract) RetainedEffects() []string {
	return append([]string(nil), contract.retainedEffects...)
}

// NonClaims returns authority deliberately excluded from this route.
func (contract DelegatedRemovalContract) NonClaims() []string {
	return append([]string(nil), contract.nonClaims...)
}

func delegatedRemovalSpecFromDossier(dossier profile.DelegatedRemovalDossier) delegatedRemovalSpec {
	preconditions := []string{
		"daem_managed_claim_exact",
		"exact_relation_present",
		"managed_instance_correlation_known",
		"passive_inventory_fresh",
		"route_dossier_admitted",
	}
	if !dossier.PreservesSharedCarrier() {
		preconditions = append(preconditions, "no_remaining_daem_known_consumers")
	}
	trust := TrustActivationNotRequired
	if dossier.RequiresExistingTrust() {
		preconditions = append(preconditions, "project_trust_already_granted")
		trust = TrustActivationRequired
	}
	actuation := ActuationDelegatedHostRoute
	switch dossier.Actuation() {
	case profile.RemovalActuationHostRoute:
	case profile.RemovalActuationDirectProjection:
		actuation = ActuationDirectProjection
	default:
		panic(fmt.Sprintf("unsupported delegated removal actuation %q", dossier.Actuation()))
	}
	return delegatedRemovalSpec{
		operation: OperationContractInput{
			Operation:            OperationRemove,
			Actuation:            actuation,
			Authority:            AuthorityRemove,
			Preconditions:        preconditions,
			EffectEnvelope:       EffectEnvelopeComplete,
			EffectPostconditions: dossier.EffectPostconditions().Requirements(),
			Idempotency:          ConditionallyIdempotent,
			Verification:         VerificationHostRelation,
			TrustActivation:      trust,
			Recovery:             OperationRecoverySafeRetry,
		},
		preservesSharedCarrier: dossier.PreservesSharedCarrier(),
		removedEffects:         dossier.RemovedEffects(),
		retainedEffects:        dossier.RetainedEffects(),
		nonClaims:              dossier.NonClaims(),
	}
}

func delegatedRelationOperationInputs(
	spec delegatedRelationCarrierSpec,
	carrier desiredextension.CarrierKey,
) ([]OperationContractInput, error) {
	inputs := append([]OperationContractInput(nil), spec.OperationContracts...)
	dossier, admitted, err := spec.Profile.RemovalDossier(carrier)
	if err != nil {
		return nil, fmt.Errorf("%s removal contract: %w", spec.Label, err)
	}
	if admitted {
		inputs, err = ensureRemovalObservationInputs(inputs)
		if err != nil {
			return nil, fmt.Errorf("%s removal observation contract: %w", spec.Label, err)
		}
		inputs = append(inputs, delegatedRemovalSpecFromDossier(dossier).operation)
	}
	return inputs, nil
}

func ensureRemovalObservationInputs(
	inputs []OperationContractInput,
) ([]OperationContractInput, error) {
	result := append([]OperationContractInput(nil), inputs...)
	installIndex := -1
	hasObserve := false
	for index := range result {
		result[index].Preconditions = append(
			[]string(nil),
			result[index].Preconditions...,
		)
		switch result[index].Operation {
		case OperationInstall:
			if installIndex >= 0 {
				return nil, fmt.Errorf("install operation appears more than once")
			}
			installIndex = index
		case OperationObserve:
			if hasObserve {
				return nil, fmt.Errorf("observe operation appears more than once")
			}
			hasObserve = true
			if result[index].Verification != VerificationHostRelation {
				return nil, fmt.Errorf(
					"removal observer verification is %q, want %q",
					result[index].Verification,
					VerificationHostRelation,
				)
			}
		}
	}
	if installIndex < 0 {
		return nil, fmt.Errorf("install operation is required")
	}
	install := &result[installIndex]
	switch {
	case hasObserve && install.Verification == VerificationHostRelation:
		return result, nil
	case hasObserve:
		return nil, fmt.Errorf(
			"removal-observed install verification is %q, want %q",
			install.Verification,
			VerificationHostRelation,
		)
	case install.Verification != VerificationInsufficient:
		return nil, fmt.Errorf(
			"unobserved install verification is %q, want %q before removal admission",
			install.Verification,
			VerificationInsufficient,
		)
	}

	install.Verification = VerificationHostRelation
	install.Preconditions = canonicalOperationPreconditions(
		install.Preconditions,
		"managed_instance_correlation_known",
		"passive_inventory_fresh",
		"same_name_unmanaged_absent",
	)
	result = append(result, OperationContractInput{
		Operation:       OperationObserve,
		Actuation:       ActuationNoMutation,
		Authority:       AuthorityObserve,
		EffectEnvelope:  EffectEnvelopeNotApplicable,
		Idempotency:     IdempotencyNotApplicable,
		Verification:    VerificationHostRelation,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoveryNotApplicable,
	})
	return result, nil
}

func canonicalOperationPreconditions(values []string, additional ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additional))
	for _, value := range append(append([]string(nil), values...), additional...) {
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func canonicalRemovalTokens(
	label string,
	values []string,
	requireOne bool,
) ([]string, error) {
	tokens := append([]string(nil), values...)
	for index, value := range tokens {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return nil, fmt.Errorf("%s[%d] must be a non-empty trimmed token", label, index)
		}
		for _, character := range value {
			if (character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') ||
				character == '_' ||
				character == '-' ||
				character == '.' {
				continue
			}
			return nil, fmt.Errorf("%s[%d] %q must be a stable token", label, index, value)
		}
	}
	sort.Strings(tokens)
	for index := 1; index < len(tokens); index++ {
		if tokens[index-1] == tokens[index] {
			return nil, fmt.Errorf("%s %q is duplicated", label, tokens[index])
		}
	}
	if requireOne && len(tokens) == 0 {
		return nil, fmt.Errorf("delegated removal requires at least one removed effect")
	}
	return tokens, nil
}
