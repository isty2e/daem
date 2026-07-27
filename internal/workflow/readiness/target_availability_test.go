package readiness

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	mcpdelegate "github.com/isty2e/daem/internal/realization/delegate/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestFromManifestLockAndStateIncludesLockedMCPProjectionTarget(t *testing.T) {
	got, err := FromManifestLockAndState(emptyEnvironment(t), snapshottest.File(t, testLockedMCPSubject(t, "context7")), durable.EmptySnapshot(), durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetClaudeCode}) {
		t.Fatalf("targets = %#v, want Claude Code from locked MCP projection", got)
	}
}

func TestFromManifestLockAndStateIncludesLockedAntigravityMCPProjectionTarget(t *testing.T) {
	got, err := FromManifestLockAndState(emptyEnvironment(t), snapshottest.File(t, testLockedAntigravityMCPSubject(t, "context7")), durable.EmptySnapshot(), durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetAntigravityCLI}) {
		t.Fatalf("targets = %#v, want Antigravity CLI from locked MCP projection", got)
	}
}

func TestFromManifestLockAndStateDoesNotRequireMCPResourceKind(t *testing.T) {
	got, err := FromManifestLockAndState(mcpEnvironment(t, "context7", target.TargetClaudeCode, target.ScopeProject), lock.File{}, durable.EmptySnapshot(), durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetClaudeCode}) {
		t.Fatalf("targets = %#v, want Claude Code from manifest MCP declaration", got)
	}
}

func TestFromManifestLockAndStateIncludesAntigravityManifestMCPDeclarationTarget(t *testing.T) {
	got, err := FromManifestLockAndState(mcpEnvironment(t, "context7", target.TargetAntigravityCLI, target.ScopeGlobal), lock.File{}, durable.EmptySnapshot(), durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetAntigravityCLI}) {
		t.Fatalf("targets = %#v, want Antigravity CLI from manifest MCP declaration", got)
	}
}

func TestFromManifestLockAndStateIncludesLockedClaudePluginCarrierTarget(t *testing.T) {
	got, err := FromManifestLockAndState(emptyEnvironment(t), snapshottest.File(t, testLockedClaudePluginCarrierSubject(t, "context7@market")), durable.EmptySnapshot(), durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetClaudeCode}) {
		t.Fatalf("targets = %#v, want Claude Code from locked plugin carrier", got)
	}
}

func TestFromManifestLockAndStateIncludesLockedCodexPluginCarrierTarget(t *testing.T) {
	got, err := FromManifestLockAndState(
		emptyEnvironment(t),
		snapshottest.File(t, testLockedCodexPluginCarrierSubject(t, "documents-managed", "documents@openai-primary-runtime")),
		durable.EmptySnapshot(),
		durablecarrier.EmptyGlobalCarrierClaims(),
	)
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetCodex}) {
		t.Fatalf("targets = %#v, want Codex from locked plugin carrier", got)
	}
}

func TestFromManifestLockAndStateIncludesRetainedCarrierFactTarget(t *testing.T) {
	contract := testLockedClaudePluginCarrierSubject(t, "context7@market")
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	owner, err := durablecarrier.NewStateAuthority(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
	})
	if err != nil {
		t.Fatalf("NewSnapshot returned error: %v", err)
	}
	got, err := FromManifestLockAndState(emptyEnvironment(t), lock.File{}, state, durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetClaudeCode}) {
		t.Fatalf("targets = %#v, want Claude Code from retained carrier authority", got)
	}
}

func TestFromManifestLockAndStateIncludesManagedCarrierClaimTarget(t *testing.T) {
	claim := testManagedCarrierClaim(
		t,
		testLockedClaudePluginCarrierSubject(t, "context7@market"),
	)
	state, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
	})
	if err != nil {
		t.Fatalf("NewSnapshot returned error: %v", err)
	}

	got, err := FromManifestLockAndState(
		emptyEnvironment(t),
		lock.File{},
		state,
		durablecarrier.EmptyGlobalCarrierClaims(),
	)
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetClaudeCode}) {
		t.Fatalf("targets = %#v, want Claude Code from managed carrier claim", got)
	}
}

func TestFromManifestLockAndStateIncludesGlobalCarrierClaimTarget(t *testing.T) {
	claim := testManagedCarrierClaim(
		t,
		testLockedCodexPluginCarrierSubject(
			t,
			"documents-managed",
			"documents@openai-primary-runtime",
		),
	)
	claims, err := durablecarrier.NewGlobalCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		t.Fatalf("NewGlobalCarrierClaims returned error: %v", err)
	}

	got, err := FromManifestLockAndState(
		emptyEnvironment(t),
		lock.File{},
		durable.EmptySnapshot(),
		claims,
	)
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetCodex}) {
		t.Fatalf("targets = %#v, want Codex from global carrier claim", got)
	}
}

func TestFromManifestLockAndStateDeduplicatesOneTargetAcrossAllSources(t *testing.T) {
	contract := testLockedCodexPluginCarrierSubject(
		t,
		"documents-managed",
		"documents@openai-primary-runtime",
	)
	claim := testManagedCarrierClaim(t, contract)
	claims, err := durablecarrier.NewGlobalCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		t.Fatalf("NewGlobalCarrierClaims returned error: %v", err)
	}
	state := managedPathSnapshot(
		t,
		entity.KindInstructions,
		"project-guidance",
		"instructions.project.agents",
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		mustAvailabilityDestination(t, "AGENTS.md"),
		realization.PathProjectionFile,
		realization.PathPermissionsExecutableClass,
	)

	got, err := FromManifestLockAndState(
		mcpEnvironment(t, "context7", target.TargetCodex, target.ScopeGlobal),
		snapshottest.File(t, contract),
		state,
		claims,
	)
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetCodex}) {
		t.Fatalf("targets = %#v, want one deduplicated Codex target", got)
	}
}

func TestFromManifestLockAndStateIncludesManagedMCPStateOnlyTarget(t *testing.T) {
	state := aggregateSnapshot(t, testLockedMCPSubject(t, "context7"))
	got, err := FromManifestLockAndState(emptyEnvironment(t), lock.File{}, state, durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetClaudeCode}) {
		t.Fatalf("targets = %#v, want Claude Code from managed MCP state", got)
	}
}

func TestFromManifestLockAndStateIncludesManagedAntigravityMCPStateOnlyTarget(t *testing.T) {
	state := aggregateSnapshot(t, testLockedAntigravityMCPSubject(t, "context7"))
	got, err := FromManifestLockAndState(emptyEnvironment(t), lock.File{}, state, durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetAntigravityCLI}) {
		t.Fatalf("targets = %#v, want Antigravity CLI from managed MCP state", got)
	}
}

func TestFromManifestLockAndStateIncludesManagedClaudeGlobalMCPStateOnlyTarget(t *testing.T) {
	state := aggregateSnapshot(t, testLockedMCPSubjectFor(t, "context7", target.TargetClaudeCode, target.ScopeGlobal))
	got, err := FromManifestLockAndState(emptyEnvironment(t), lock.File{}, state, durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetClaudeCode}) {
		t.Fatalf("targets = %#v, want Claude Code from managed Claude global MCP state", got)
	}
}

func TestFromManifestLockAndStateIncludesManagedOrdinaryStateOnlyTarget(t *testing.T) {
	state := managedPathSnapshot(
		t,
		entity.KindInstructions,
		"project-guidance",
		"instructions.project.agents",
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		mustAvailabilityDestination(t, "AGENTS.md"),
		realization.PathProjectionFile,
		realization.PathPermissionsExecutableClass,
	)
	got, err := FromManifestLockAndState(emptyEnvironment(t), lock.File{}, state, durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []target.Target{target.TargetCodex}) {
		t.Fatalf("targets = %#v, want Codex from managed ordinary state", got)
	}
}

func TestFromManifestLockAndStateIncludesAllSharedStateConsumersInStableOrder(t *testing.T) {
	first := managedPathState(
		t,
		entity.KindInstructions,
		"shared-guidance",
		"instructions.project.agents",
		[]target.Target{target.TargetOpenCode, target.TargetPi},
		target.ScopeProject,
		mustAvailabilityDestination(t, "AGENTS.md"),
		realization.PathProjectionFile,
		realization.PathPermissionsExecutableClass,
	)
	second := managedPathState(
		t,
		entity.KindSkill,
		"duplicate-opencode",
		"skill.project.opencode",
		[]target.Target{target.TargetOpenCode},
		target.ScopeProject,
		mustAvailabilityDestination(t, ".opencode/skills/duplicate-opencode"),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
	)
	state, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{first, second}})
	if err != nil {
		t.Fatalf("NewSnapshot returned error: %v", err)
	}
	got, err := FromManifestLockAndState(emptyEnvironment(t), lock.File{}, state, durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	want := []target.Target{target.TargetOpenCode, target.TargetPi}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %#v, want stable deduplicated order %#v", got, want)
	}
}

func TestFromManifestLockAndStateHasNoUnmanagedDurableFamily(t *testing.T) {
	got, err := FromManifestLockAndState(emptyEnvironment(t), lock.File{}, durable.EmptySnapshot(), durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatalf("FromManifestLockAndState returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("targets = %#v, want empty durable state excluded", got)
	}
}

func TestManagedPathStateRejectsInvalidConsumerTargetsBeforeAvailability(t *testing.T) {
	for _, consumers := range [][]target.Target{nil, {target.TargetCodex, "not-a-target"}} {
		subject := projectionSubject(t, entity.KindInstructions, "invalid", "instructions.project.agents")
		if _, err := durable.NewManagedPathState(
			subject,
			consumers,
			target.ScopeProject,
			mustAvailabilityDestination(t, "AGENTS.md"),
			"sha256:invalid",
			realization.PathProjectionFile,
			realization.PathPermissionsExecutableClass,
			0,
		); err == nil {
			t.Fatalf("NewManagedPathState returned nil error for consumers %#v", consumers)
		}
	}
}

func aggregateSnapshot(t *testing.T, contract lock.LockedSubjectContract) durable.Snapshot {
	t.Helper()
	item, present, err := contract.ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatalf("ManagedAggregateContribution = (%#v, %t, %v)", item, present, err)
	}
	state, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
	if err != nil {
		t.Fatalf("NewManagedAggregateState returned error: %v", err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedAggregates: []durable.ManagedAggregateState{state},
	})
	if err != nil {
		t.Fatalf("NewSnapshot returned error: %v", err)
	}
	return snapshot
}

func managedPathSnapshot(
	t *testing.T,
	kind entity.Kind,
	name string,
	namespace string,
	consumers []target.Target,
	scope target.Scope,
	destination output.Destination,
	contentKind realization.PathProjectionContentKind,
	permissionPolicy realization.PathPermissionPolicy,
) durable.Snapshot {
	t.Helper()
	state := managedPathState(t, kind, name, namespace, consumers, scope, destination, contentKind, permissionPolicy)
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{state},
	})
	if err != nil {
		t.Fatalf("NewSnapshot returned error: %v", err)
	}
	return snapshot
}

func mustAvailabilityDestination(t testing.TB, value string) output.Destination {
	t.Helper()
	destination, err := output.Parse(value)
	if err != nil {
		t.Fatalf("output.Parse(%q) returned error: %v", value, err)
	}
	return destination
}

func managedPathState(
	t *testing.T,
	kind entity.Kind,
	name string,
	namespace string,
	consumers []target.Target,
	scope target.Scope,
	destination output.Destination,
	contentKind realization.PathProjectionContentKind,
	permissionPolicy realization.PathPermissionPolicy,
) durable.ManagedPathState {
	t.Helper()
	state, err := durable.NewManagedPathState(
		projectionSubject(t, kind, name, namespace),
		consumers,
		scope,
		destination,
		artifact.HashFileContent([]byte(name)),
		contentKind,
		permissionPolicy,
		0,
	)
	if err != nil {
		t.Fatalf("NewManagedPathState returned error: %v", err)
	}
	return state
}

func projectionSubject(t *testing.T, kind entity.Kind, name string, namespace string) topology.SubjectID {
	t.Helper()
	id, err := entity.New(kind, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	subject, err := topologyprojection.Subject(id, namespace)
	if err != nil {
		t.Fatalf("projection.Subject returned error: %v", err)
	}
	return subject
}

func testManagedCarrierClaim(
	t *testing.T,
	contract lock.LockedSubjectContract,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatalf("DelegatedOperationRequest returned error: %v", err)
	}
	root := t.TempDir()
	owner, err := durablecarrier.NewStateAuthority(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatalf("NewStateAuthority returned error: %v", err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		request,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatalf("NewManagedCarrierClaim returned error: %v", err)
	}
	return claim
}

func testLockedClaudePluginCarrierSubject(t *testing.T, pluginKey string) lock.LockedSubjectContract {
	t.Helper()
	value := testfixture.Extension(t, desiredextension.Spec{
		Name:    "context7",
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source:  testfixture.ExtensionSource(t, desiredextension.SourceKindMarketplace, pluginKey),
	})
	file, _ := snapshottest.ExtensionCarrierFile(t, value)
	return file.Locked.Subjects()[0]
}

func testLockedCodexPluginCarrierSubject(
	t *testing.T,
	declarationID string,
	pluginKey string,
) lock.LockedSubjectContract {
	t.Helper()
	value := testfixture.Extension(t, desiredextension.Spec{
		Name:    declarationID,
		Carrier: desiredextension.CarrierCodexPlugin,
		Target:  target.TargetCodex,
		Scope:   target.ScopeGlobal,
		Source:  testfixture.ExtensionSource(t, desiredextension.SourceKindMarketplace, pluginKey),
	})
	file, _ := snapshottest.ExtensionCarrierFile(t, value)
	return file.Locked.Subjects()[0]
}

func testLockedMCPSubject(t *testing.T, serverID string) lock.LockedSubjectContract {
	t.Helper()
	server := testMCPServer(t, serverID, target.TargetClaudeCode, target.ScopeProject)
	binding := server.Bindings()[0]
	graph, err := topologymcp.Servers([]desiredmcp.Server{server})
	if err != nil {
		t.Fatalf("MCPServer returned error: %v", err)
	}
	delegatePlan, err := mcpdelegate.MCPBindingDelegatePlan(server, binding)
	if err != nil {
		t.Fatalf("MCPServerDelegatePlan returned error: %v", err)
	}
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude project MCP placement is unavailable")
	}
	canonical, err := mcpcodec.CanonicalMCPBindingContribution(server, binding, placement)
	if err != nil {
		t.Fatalf("CanonicalMCPBindingContribution returned error: %v", err)
	}
	record, err := lock.NewMCPProjectionSubjectContract(lock.MCPProjectionSubjectInput{
		Graph:               graph,
		EntityID:            server.ID(),
		PlacementID:         placement.ID(),
		ServerID:            serverID,
		RequestedOnAbsent:   desiredmcp.OnAbsentRemoveBinding,
		LauncherCommand:     "node",
		CanonicalProjection: string(canonical),
		DelegatePlan:        &delegatePlan,
	})
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}
	return record
}

func testLockedAntigravityMCPSubject(t *testing.T, serverID string) lock.LockedSubjectContract {
	return testLockedMCPSubjectFor(t, serverID, target.TargetAntigravityCLI, target.ScopeGlobal)
}

func testLockedMCPSubjectFor(
	t *testing.T,
	serverID string,
	selected target.Target,
	scope target.Scope,
) lock.LockedSubjectContract {
	t.Helper()
	server := testMCPServer(t, serverID, selected, scope)
	graph, err := topologymcp.Servers([]desiredmcp.Server{server})
	if err != nil {
		t.Fatalf("MCPServer returned error: %v", err)
	}
	placement, ok := aggregate.ImplementedMCPPlacement(selected, scope)
	if !ok {
		t.Fatalf("%s %s MCP placement is unavailable", selected, scope)
	}
	canonical, err := mcpcodec.CanonicalMCPBindingContribution(server, server.Bindings()[0], placement)
	if err != nil {
		t.Fatalf("CanonicalMCPBindingContribution returned error: %v", err)
	}
	record, err := lock.NewMCPProjectionSubjectContract(lock.MCPProjectionSubjectInput{
		Graph:               graph,
		EntityID:            server.ID(),
		PlacementID:         placement.ID(),
		ServerID:            serverID,
		RequestedOnAbsent:   desiredmcp.OnAbsentRemoveBinding,
		LauncherCommand:     "node",
		CanonicalProjection: string(canonical),
	})
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}
	return record
}

func emptyEnvironment(t *testing.T) desired.Environment {
	t.Helper()
	return testfixture.Environment(t, desired.Spec{
		Targets:  []target.Target{target.TargetCodex},
		Defaults: testfixture.Defaults(t, target.ScopeProject, desiredskill.InstallModeCopy),
	})
}

func mcpEnvironment(t *testing.T, id string, selected target.Target, scope target.Scope) desired.Environment {
	t.Helper()
	return testfixture.Environment(t, desired.Spec{
		Targets:    []target.Target{selected},
		Defaults:   testfixture.Defaults(t, target.ScopeProject, desiredskill.InstallModeCopy),
		MCPServers: []desiredmcp.Server{testMCPServer(t, id, selected, scope)},
	})
}

func testMCPServer(t *testing.T, id string, selected target.Target, scope target.Scope) desiredmcp.Server {
	t.Helper()
	transport := testfixture.MCPStdio(t, testfixture.MCPCommand(t, "node"), nil, nil)
	binding := testfixture.MCPBinding(t, selected, scope, transport, desiredmcp.OnAbsentRemoveBinding)
	return testfixture.MCPServer(t, desiredmcp.Spec{Name: id, Bindings: []desiredmcp.Binding{binding}})
}
