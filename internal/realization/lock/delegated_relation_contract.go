package lock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type delegatedRelationCarrierSpec struct {
	Label              string
	Profile            profile.DelegatedRouteProfile
	Ownership          OwnershipBasis
	OnAbsent           OnAbsentPolicy
	Replay             delegatedRelationReplaySpec
	OperationContracts []OperationContractInput
}

type delegatedRelationReplaySpec struct {
	Invocation ReplayClass
	Outcome    ReplayClass
	Derivation ReplayClass
	Exclusions []ReplayExclusion
}

var codexPluginCarrierSpec = delegatedRelationCarrierSpec{
	Label:     "Codex plugin carrier",
	Profile:   mustProfileDelegatedRoute(target.TargetCodex, desiredextension.CarrierCodexPlugin),
	Ownership: OwnershipManifest,
	OnAbsent:  OnAbsentBlock,
	Replay: delegatedRelationReplaySpec{
		Invocation: ReplayPartial,
		Outcome:    ReplayUnavailable,
		Derivation: ReplayNotApplicable,
		Exclusions: []ReplayExclusion{
			{Component: "host-selected plugin artifact or version", Reason: ReplayExclusionHostSelectedArtifact},
			{Component: "Codex marketplace/source selected outcome", Reason: ReplayExclusionHostMarketplace},
			{Component: "Codex plugin cache and dependency state", Reason: ReplayExclusionRuntimeDependency},
			{Component: "Codex trust, auth, or session state", Reason: ReplayExclusionHostApproval},
			{Component: "plugin activation and runtime readiness", Reason: ReplayExclusionRuntimeReadiness},
			{Component: "plugin-bundled MCP, app, skill, hook, or tool inventory", Reason: ReplayExclusionToolInventory},
		},
	},
	OperationContracts: []OperationContractInput{
		{
			Operation:       OperationObserve,
			Actuation:       ActuationNoMutation,
			Authority:       AuthorityObserve,
			EffectEnvelope:  EffectEnvelopeNotApplicable,
			Idempotency:     IdempotencyNotApplicable,
			Verification:    VerificationHostRelation,
			TrustActivation: TrustActivationNotRequired,
			Recovery:        OperationRecoveryNotApplicable,
		},
		{
			Operation: OperationInstall,
			Actuation: ActuationDelegatedHostRoute,
			Authority: AuthorityManage,
			Preconditions: []string{
				"passive_inventory_fresh",
				"managed_instance_correlation_known",
				"same_name_unmanaged_absent",
				"route_dossier_admitted",
			},
			EffectEnvelope:  EffectEnvelopeIncomplete,
			Idempotency:     IdempotencyUnknown,
			Verification:    VerificationHostRelation,
			TrustActivation: TrustActivationUnknown,
			Recovery:        OperationRecoveryUnknown,
		},
		{
			Operation: OperationRefresh,
			Actuation: ActuationDelegatedHostRoute,
			Authority: AuthorityNone,
			Preconditions: []string{
				"exact_relation_present",
				"explicit_refresh_intent",
				"passive_inventory_fresh",
				"route_dossier_admitted",
			},
			EffectEnvelope:  EffectEnvelopeIncomplete,
			Idempotency:     IdempotencyUnknown,
			Verification:    VerificationHostRelation,
			TrustActivation: TrustActivationUnknown,
			Recovery:        OperationRecoveryUnknown,
		},
	},
}

// NewDelegatedRelationCarrierContract constructs one canonical locked
// relation from orthogonal Desired, Topology, and host-relation identity.
func NewDelegatedRelationCarrierContract(
	entityID entity.ID,
	carrier desiredextension.CarrierKey,
	subject topology.SubjectID,
	subjectKey hostrelation.SubjectKey,
) (LockedSubjectContract, error) {
	if entityID.Kind() != entity.KindExtension {
		return LockedSubjectContract{}, fmt.Errorf("delegated carrier requires Extension entity, got %q", entityID.Kind())
	}
	if entityID.Name() != subject.Key() {
		return LockedSubjectContract{}, fmt.Errorf(
			"extension entity %q does not match relation subject %q",
			entityID,
			subject,
		)
	}
	spec, ok := delegatedRelationCarrierSpecFor(carrier.Carrier())
	if !ok {
		return LockedSubjectContract{}, fmt.Errorf("unsupported extension carrier %q", carrier.Carrier())
	}
	if err := validateDelegatedRelationCarrierIdentity(carrier, subject, spec); err != nil {
		return LockedSubjectContract{}, err
	}
	expected, err := hostrelation.Derive(carrier, subject, subjectKey)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	realization, err := delegatedRelationCarrierRealization(carrier, subject, expected, spec)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	replay, err := delegatedRelationReplayCoverage(spec.Replay)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	operationInputs, err := delegatedRelationOperationInputs(spec, carrier)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	contracts, err := delegatedRelationOperationContracts(spec.Profile, operationInputs)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	return NewLockedSubjectContract(LockedSubjectContractInput{
		EntityID:           entityID,
		SubjectID:          subject,
		Realization:        &realization,
		Ownership:          spec.Ownership,
		OnAbsent:           spec.OnAbsent,
		Replay:             replay,
		OperationContracts: contracts,
	})
}

func delegatedRelationCarrierSpecFor(carrier desiredextension.Carrier) (delegatedRelationCarrierSpec, bool) {
	for _, spec := range implementedDelegatedRelationCarrierSpecs() {
		if spec.Profile.Carrier() == carrier {
			return spec, true
		}
	}
	return delegatedRelationCarrierSpec{}, false
}

// DelegatedRelationCarrier returns the admitted carrier family after
// reconstructing and validating every canonical delegated-relation fact.
func DelegatedRelationCarrier(
	contract LockedSubjectContract,
) (desiredextension.Carrier, bool, error) {
	key, admitted, err := DelegatedRelationCarrierKey(contract)
	if err != nil || !admitted {
		return "", admitted, err
	}
	return key.Carrier(), true, nil
}

// DelegatedRelationCarrierKey returns the complete canonical carrier identity
// after reconstructing and validating every locked relation fact.
func DelegatedRelationCarrierKey(
	contract LockedSubjectContract,
) (desiredextension.CarrierKey, bool, error) {
	for _, spec := range implementedDelegatedRelationCarrierSpecs() {
		carrier, ok, err := delegatedRelationCarrierKey(contract, spec)
		if err != nil || ok {
			return carrier, ok, err
		}
	}
	return desiredextension.CarrierKey{}, false, nil
}

// DelegatedOperationRequest derives the exact operation-indexed route request
// from one admitted locked carrier contract. Install preserves the realization
// request identity; other operations derive a distinct hash from that identity
// plus the locked operation and route contract.
func DelegatedOperationRequest(
	contract LockedSubjectContract,
	operation OperationKind,
) (realizationdelegate.Request, error) {
	request, err := carrierOperationRequest(contract, operation)
	if err != nil {
		return realizationdelegate.Request{}, err
	}
	operationContract, ok := contract.OperationContract(operation)
	if !ok {
		return realizationdelegate.Request{}, fmt.Errorf(
			"delegated carrier subject %q has no %q operation contract",
			contract.SubjectID().Key(),
			operation,
		)
	}
	if operationContract.Actuation() != ActuationDelegatedHostRoute {
		return realizationdelegate.Request{}, fmt.Errorf(
			"delegated carrier subject %q operation %q does not use a delegated host route",
			contract.SubjectID().Key(),
			operation,
		)
	}
	return request, nil
}

func carrierOperationRequest(
	contract LockedSubjectContract,
	operation OperationKind,
) (realizationdelegate.Request, error) {
	if _, admitted, err := DelegatedRelationCarrier(contract); err != nil {
		return realizationdelegate.Request{}, err
	} else if !admitted {
		return realizationdelegate.Request{}, fmt.Errorf(
			"subject %q is not an admitted delegated carrier contract",
			contract.SubjectID().Key(),
		)
	}
	realizationSpec, _ := contract.Realization()
	relationSpec, _ := realizationSpec.DelegatedRelation()
	operationContract, ok := contract.OperationContract(operation)
	if !ok {
		return realizationdelegate.Request{}, fmt.Errorf(
			"delegated carrier subject %q has no %q operation contract",
			contract.SubjectID().Key(),
			operation,
		)
	}
	switch operationContract.Actuation() {
	case ActuationDelegatedHostRoute, ActuationDirectProjection:
	default:
		return realizationdelegate.Request{}, fmt.Errorf(
			"delegated carrier subject %q operation %q has no executable route identity",
			contract.SubjectID().Key(),
			operation,
		)
	}
	return delegatedOperationRequest(
		contract.SubjectID(),
		relationSpec,
		operationContract,
		operation,
	)
}

func delegatedOperationRequest(
	subject topology.SubjectID,
	relationSpec realization.DelegatedRelation,
	operationContract OperationContract,
	operation OperationKind,
) (realizationdelegate.Request, error) {
	route := operationContract.Route()
	if operation == OperationInstall {
		request := relationSpec.RouteRequest()
		if request.RouteID() != route.RouteID ||
			request.ContractVersion() != route.AdapterContractVersion {
			return realizationdelegate.Request{}, fmt.Errorf(
				"delegated carrier subject %q install route does not match its realization",
				subject.Key(),
			)
		}
		return request, nil
	}

	canonical, err := json.Marshal(struct {
		BaseRequestHash        string        `json:"base_request_hash"`
		Operation              OperationKind `json:"operation"`
		RouteID                string        `json:"route_id"`
		AdapterContractVersion string        `json:"adapter_contract_version"`
	}{
		BaseRequestHash:        relationSpec.CanonicalRequestHash(),
		Operation:              operation,
		RouteID:                route.RouteID,
		AdapterContractVersion: route.AdapterContractVersion,
	})
	if err != nil {
		return realizationdelegate.Request{}, fmt.Errorf("encode delegated operation request: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return realizationdelegate.NewRequest(
		route.RouteID,
		route.AdapterContractVersion,
		"sha256:"+hex.EncodeToString(digest[:]),
	)
}

func delegatedRelationCarrierKey(
	contract LockedSubjectContract,
	spec delegatedRelationCarrierSpec,
) (desiredextension.CarrierKey, bool, error) {
	subjectID := contract.SubjectID()
	if !extensiontopology.IsCarrierRelation(spec.Profile.Carrier(), subjectID) {
		return desiredextension.CarrierKey{}, false, nil
	}
	if err := validateDelegatedRelationCarrierSpec(spec); err != nil {
		return desiredextension.CarrierKey{}, true, err
	}
	realization, ok := contract.Realization()
	if !ok {
		return desiredextension.CarrierKey{}, true, fmt.Errorf("%s %q requires delegated relation realization", spec.Label, subjectID.Key())
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		return desiredextension.CarrierKey{}, true, fmt.Errorf("%s %q requires delegated relation realization", spec.Label, subjectID.Key())
	}
	source, err := desiredextension.ParseSourceRef(relation.SourceNamespace())
	if err != nil {
		return desiredextension.CarrierKey{}, true, fmt.Errorf("%s %q source namespace: %w", spec.Label, subjectID.Key(), err)
	}
	carrier, err := desiredextension.NewCarrierKey(
		spec.Profile.Carrier(),
		relation.Target(),
		relation.Scope(),
		source,
	)
	if err != nil {
		return desiredextension.CarrierKey{}, true, err
	}
	expected, err := hostrelation.Derive(
		carrier,
		subjectID,
		relation.ExpectedRelation().SubjectKey(),
	)
	if err != nil {
		return desiredextension.CarrierKey{}, true, err
	}
	if !relation.ExpectedRelation().Equal(expected) {
		return desiredextension.CarrierKey{}, true, fmt.Errorf("%s %q managed instance key does not match locked realization", spec.Label, subjectID.Key())
	}
	expectedContract, err := NewDelegatedRelationCarrierContract(
		contract.EntityID(),
		carrier,
		subjectID,
		expected.SubjectKey(),
	)
	if err != nil {
		return desiredextension.CarrierKey{}, true, err
	}
	if !contract.Equal(expectedContract) {
		return desiredextension.CarrierKey{}, true, fmt.Errorf("%s %q does not match the admitted carrier contract", spec.Label, subjectID.Key())
	}
	return carrier, true, nil
}

func delegatedRelationSourceNamespace(source desiredextension.SourceRef) string {
	return source.String()
}

func delegatedRelationCarrierRealization(
	carrier desiredextension.CarrierKey,
	subject topology.SubjectID,
	expected hostrelation.ExpectedRelation,
	spec delegatedRelationCarrierSpec,
) (realization.RealizationSpec, error) {
	if err := validateDelegatedRelationCarrierIdentity(carrier, subject, spec); err != nil {
		return realization.RealizationSpec{}, err
	}
	payload, err := canonicalDelegatedRelationRoutePayload(carrier, subject, expected)
	if err != nil {
		return realization.RealizationSpec{}, err
	}
	digest := sha256.Sum256(payload)
	return spec.Profile.Realize(profile.DelegatedRelationProfileInput{
		Scope:                carrier.Scope(),
		SourceNamespace:      delegatedRelationSourceNamespace(carrier.Source()),
		ExpectedRelation:     expected,
		CanonicalRequestHash: "sha256:" + hex.EncodeToString(digest[:]),
	})
}

func canonicalDelegatedRelationRoutePayload(
	carrier desiredextension.CarrierKey,
	subject topology.SubjectID,
	expected hostrelation.ExpectedRelation,
) ([]byte, error) {
	fields := []struct {
		key   string
		value string
	}{
		{key: "family", value: string(carrier.Carrier())},
		{key: "target", value: string(carrier.Target())},
		{key: "scope", value: string(carrier.Scope())},
		{key: "declaration_id", value: subject.Key()},
		{key: "source_kind", value: string(carrier.Source().Kind())},
		{key: "source_ref", value: carrier.Source().Ref()},
		{key: "relation_subject_key", value: string(expected.SubjectKey())},
		{key: "managed_instance_key", value: string(expected.ManagedInstanceKey())},
	}

	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, field := range fields {
		if index > 0 {
			buffer.WriteByte(',')
		}
		key, err := json.Marshal(field.key)
		if err != nil {
			return nil, fmt.Errorf("canonical delegated route request key: %w", err)
		}
		value, err := json.Marshal(field.value)
		if err != nil {
			return nil, fmt.Errorf("canonical delegated route request value: %w", err)
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		buffer.Write(value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func delegatedRelationReplayCoverage(spec delegatedRelationReplaySpec) (ReplayCoverage, error) {
	return NewReplayCoverage(spec.Invocation, spec.Outcome, spec.Derivation, spec.Exclusions)
}

func delegatedRelationOperationContracts(
	profile profile.DelegatedRouteProfile,
	inputs []OperationContractInput,
) ([]OperationContract, error) {
	contracts := make([]OperationContract, 0, len(inputs))
	for _, input := range inputs {
		if input.Route.RouteID != "" || input.Route.AdapterContractVersion != "" {
			return nil, fmt.Errorf("delegated operation %q must take route identity from its target profile", input.Operation)
		}
		if input.Actuation != ActuationNoMutation {
			profileOperation, ok := delegatedProfileOperation(input.Operation)
			if !ok {
				return nil, fmt.Errorf(
					"delegated operation %q has no profile operation mapping",
					input.Operation,
				)
			}
			operationRoute, ok := profile.OperationRoute(profileOperation)
			if !ok {
				return nil, fmt.Errorf(
					"delegated operation %q has no unique target profile route",
					input.Operation,
				)
			}
			input.Route = RouteContractRef{
				RouteID:                operationRoute.RouteID(),
				AdapterContractVersion: operationRoute.AdapterContractVersion(),
			}
		}
		contract, err := NewOperationContract(input)
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, contract)
	}
	return contracts, nil
}

func delegatedProfileOperation(operation OperationKind) (profile.Operation, bool) {
	switch operation {
	case OperationInstall:
		return profile.OperationInstall, true
	case OperationRemove:
		return profile.OperationRemove, true
	case OperationRefresh:
		return profile.OperationRefresh, true
	default:
		return "", false
	}
}

func mustProfileDelegatedRoute(
	selectedTarget target.Target,
	carrier desiredextension.Carrier,
) profile.DelegatedRouteProfile {
	profile, ok := profile.Profile(selectedTarget).DelegatedRoute(carrier)
	if !ok {
		panic(fmt.Sprintf("target profile %q is missing delegated route %q", selectedTarget, carrier))
	}
	return profile
}
