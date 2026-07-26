package lock

import (
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type carrierContractCase struct {
	name        string
	carrier     desiredextension.Carrier
	target      target.Target
	scope       target.Scope
	namespace   string
	declaration string
	sourceKind  desiredextension.SourceKind
	sourceRef   string
	subjectKey  string
}

func implementedCarrierContractCases() []carrierContractCase {
	return []carrierContractCase{
		{name: "claude project", carrier: desiredextension.CarrierClaudeCodePlugin, target: target.TargetClaudeCode, scope: target.ScopeProject, namespace: "claude-code.plugin-carrier", declaration: "context7-project", sourceKind: desiredextension.SourceKindMarketplace, sourceRef: "team/context7:beta@official", subjectKey: "context7"},
		{name: "claude global", carrier: desiredextension.CarrierClaudeCodePlugin, target: target.TargetClaudeCode, scope: target.ScopeGlobal, namespace: "claude-code.plugin-carrier", declaration: "context7-global", sourceKind: desiredextension.SourceKindMarketplace, sourceRef: "team/context7:beta@official", subjectKey: "context7"},
		{name: "codex", carrier: desiredextension.CarrierCodexPlugin, target: target.TargetCodex, scope: target.ScopeGlobal, namespace: "codex.plugin-carrier", declaration: "documents", sourceKind: desiredextension.SourceKindMarketplace, sourceRef: "documents@openai-primary-runtime", subjectKey: "documents@openai-primary-runtime"},
		{name: "opencode project", carrier: desiredextension.CarrierOpenCodePlugin, target: target.TargetOpenCode, scope: target.ScopeProject, namespace: "opencode.plugin-carrier", declaration: "formatter-project", sourceKind: desiredextension.SourceKindHostSource, sourceRef: "github:acme/opencode-formatter", subjectKey: "@acme/opencode-formatter"},
		{name: "opencode global", carrier: desiredextension.CarrierOpenCodePlugin, target: target.TargetOpenCode, scope: target.ScopeGlobal, namespace: "opencode.plugin-carrier", declaration: "formatter-global", sourceKind: desiredextension.SourceKindHostSource, sourceRef: "github:acme/opencode-formatter", subjectKey: "@acme/opencode-formatter"},
		{name: "pi project", carrier: desiredextension.CarrierPiPackage, target: target.TargetPi, scope: target.ScopeProject, namespace: "pi.package-carrier", declaration: "tools-project", sourceKind: desiredextension.SourceKindHostSource, sourceRef: "github:acme/pi-tools", subjectKey: "github:acme/pi-tools"},
		{name: "pi global", carrier: desiredextension.CarrierPiPackage, target: target.TargetPi, scope: target.ScopeGlobal, namespace: "pi.package-carrier", declaration: "tools-global", sourceKind: desiredextension.SourceKindHostSource, sourceRef: "github:acme/pi-tools", subjectKey: "github:acme/pi-tools"},
		{name: "antigravity", carrier: desiredextension.CarrierAntigravityCLIPlugin, target: target.TargetAntigravityCLI, scope: target.ScopeGlobal, namespace: "antigravity-cli.plugin-carrier", declaration: "guidance", sourceKind: desiredextension.SourceKindHostSource, sourceRef: "modern-web-guidance@google", subjectKey: "modern-web-guidance"},
	}
}

func operationContractInputForTest(
	t *testing.T,
	contracts []OperationContractInput,
	operation OperationKind,
) OperationContractInput {
	t.Helper()
	contract, ok := operationContractInputForTestOptional(contracts, operation)
	if !ok {
		t.Fatalf("operation %q is missing from %#v", operation, contracts)
	}
	return contract
}

func operationContractInputForTestOptional(
	contracts []OperationContractInput,
	operation OperationKind,
) (OperationContractInput, bool) {
	for _, contract := range contracts {
		if contract.Operation == operation {
			return contract, true
		}
	}
	return OperationContractInput{}, false
}

func replaySpecHasExclusion(spec delegatedRelationReplaySpec, reason ReplayExclusionReason) bool {
	for _, exclusion := range spec.Exclusions {
		if exclusion.Reason == reason {
			return true
		}
	}
	return false
}

type delegatedCarrierRelationOverride struct {
	managedInstanceKey   string
	subjectKey           string
	routeID              string
	routeContractVersion string
	canonicalRequestHash string
}

func mustDelegatedRelationForCarrierTest(
	t *testing.T,
	base realization.DelegatedRelation,
	override delegatedCarrierRelationOverride,
) realization.DelegatedRelation {
	t.Helper()
	expected := base.ExpectedRelation()
	if override.subjectKey != "" {
		subjectKey, err := hostrelation.NewSubjectKey(override.subjectKey)
		if err != nil {
			t.Fatal(err)
		}
		expected, err = hostrelation.NewExpectedRelation(subjectKey, expected.ManagedInstanceKey())
		if err != nil {
			t.Fatal(err)
		}
	}
	if override.managedInstanceKey != "" {
		managedKey, err := hostrelation.NewManagedInstanceKey(override.managedInstanceKey)
		if err != nil {
			t.Fatal(err)
		}
		expected, err = hostrelation.NewExpectedRelation(expected.SubjectKey(), managedKey)
		if err != nil {
			t.Fatal(err)
		}
	}
	routeID := base.RouteID()
	if override.routeID != "" {
		routeID = override.routeID
	}
	routeContractVersion := base.RouteContractVersion()
	if override.routeContractVersion != "" {
		routeContractVersion = override.routeContractVersion
	}
	canonicalRequestHash := base.CanonicalRequestHash()
	if override.canonicalRequestHash != "" {
		canonicalRequestHash = override.canonicalRequestHash
	}
	realization, err := realization.NewDelegatedRelation(realization.DelegatedRelationInput{
		PlacementID:            base.PlacementID(),
		Target:                 base.Target(),
		Scope:                  base.Scope(),
		SourceNamespace:        base.SourceNamespace(),
		ExpectedRelation:       expected,
		RouteID:                routeID,
		RouteContractVersion:   routeContractVersion,
		CanonicalRequestHash:   canonicalRequestHash,
		VerifiedRelationFields: base.VerifiedRelationFields(),
	})
	if err != nil {
		t.Fatal(err)
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		t.Fatal("replacement realization is not delegated relation")
	}
	return relation
}

func carrierOperationsExcept(contract LockedSubjectContract, excluded OperationKind) []OperationContract {
	operations := make([]OperationContract, 0, len(contract.OperationKinds()))
	for _, kind := range contract.OperationKinds() {
		if kind == excluded {
			continue
		}
		operation, _ := contract.OperationContract(kind)
		operations = append(operations, operation)
	}
	return operations
}

func carrierOperationsForRelation(
	t *testing.T,
	contract LockedSubjectContract,
	relation realization.DelegatedRelation,
) []OperationContract {
	t.Helper()
	operations := make([]OperationContract, 0, len(contract.OperationKinds()))
	for _, kind := range contract.OperationKinds() {
		operation, _ := contract.OperationContract(kind)
		route := operation.Route()
		if operation.Actuation() != ActuationNoMutation {
			route = RouteContractRef{
				RouteID:                relation.RouteID(),
				AdapterContractVersion: relation.RouteContractVersion(),
			}
		}
		cloned, err := NewOperationContract(OperationContractInput{
			Operation:            operation.Operation(),
			Actuation:            operation.Actuation(),
			Authority:            operation.Authority(),
			Route:                route,
			HostCompatibility:    operation.HostCompatibility(),
			Preconditions:        operation.Preconditions(),
			EffectEnvelope:       operation.EffectEnvelope(),
			EffectPostconditions: operation.EffectPostconditions().Requirements(),
			Idempotency:          operation.Idempotency(),
			Verification:         operation.Verification(),
			TrustActivation:      operation.TrustActivation(),
			Recovery:             operation.Recovery(),
		})
		if err != nil {
			t.Fatal(err)
		}
		operations = append(operations, cloned)
	}
	return operations
}

func rebuildCarrierContractForTest(
	t *testing.T,
	base LockedSubjectContract,
	subjectID topology.SubjectID,
	relation realization.DelegatedRelation,
	operations []OperationContract,
) LockedSubjectContract {
	t.Helper()
	realization, err := realization.NewDelegatedRelation(realization.DelegatedRelationInput{
		PlacementID:            relation.PlacementID(),
		Target:                 relation.Target(),
		Scope:                  relation.Scope(),
		SourceNamespace:        relation.SourceNamespace(),
		ExpectedRelation:       relation.ExpectedRelation(),
		RouteID:                relation.RouteID(),
		RouteContractVersion:   relation.RouteContractVersion(),
		CanonicalRequestHash:   relation.CanonicalRequestHash(),
		VerifiedRelationFields: relation.VerifiedRelationFields(),
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := NewLockedSubjectContract(LockedSubjectContractInput{
		EntityID:           base.EntityID(),
		SubjectID:          subjectID,
		Realization:        &realization,
		Ownership:          base.Ownership(),
		OnAbsent:           base.OnAbsent(),
		Replay:             base.ReplayCoverage(),
		OperationContracts: operations,
	})
	if err != nil {
		t.Fatalf("NewLockedSubjectContract: %v", err)
	}
	return contract
}

func mustCarrierFacts(
	t *testing.T,
	test carrierContractCase,
) (
	desiredextension.CarrierKey,
	topology.SubjectID,
	hostrelation.SubjectKey,
	hostrelation.ExpectedRelation,
) {
	t.Helper()
	source, err := desiredextension.NewSourceRef(test.sourceKind, test.sourceRef)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := desiredextension.NewCarrierKey(test.carrier, test.target, test.scope, source)
	if err != nil {
		t.Fatal(err)
	}
	subject := mustTopologySubjectID(t, topology.SubjectHostRelation, test.namespace, test.declaration)
	subjectKey, err := hostrelation.NewSubjectKey(test.subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.Derive(carrier, subject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	return carrier, subject, subjectKey, expected
}

func mustResolveCurrentDelegatedRemoval(
	t *testing.T,
	carrier desiredextension.CarrierKey,
	subject topology.SubjectID,
	expected hostrelation.ExpectedRelation,
	contract LockedSubjectContract,
) (DelegatedRemovalContract, bool) {
	t.Helper()
	installRequest, err := DelegatedOperationRequest(contract, OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	removal, admitted, err := ResolveDelegatedCarrierRemoval(
		carrier,
		subject,
		expected,
		installRequest,
	)
	if err != nil {
		t.Fatal(err)
	}
	return removal, admitted
}
