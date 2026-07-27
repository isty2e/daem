package build

import (
	"context"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestBuildLocksClaudeExtensionsAsPluginCarrierSubjects(t *testing.T) {
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			MCPServers: []mcp.Server{
				testMCPServer(t, "alpha-mcp", "node", []string{"server.js"}, nil),
			},
			Extensions: []extension.Extension{
				testClaudeExtension(t, "zeta-managed", "zeta@official"),
				testClaudeExtension(t, "context7-managed", "context7@official"),
			},
		}),
		nil,
		Options{},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 3 {
		t.Fatalf("locked subjects = %#v, want two plugin carriers and one MCP projection", file.Locked.Subjects())
	}

	wantOrder := []topology.SubjectID{
		mustBuildSubjectID(t, topology.SubjectHostRelation, "claude-code.plugin-carrier", "context7-managed"),
		mustBuildSubjectID(t, topology.SubjectHostRelation, "claude-code.plugin-carrier", "zeta-managed"),
		mustBuildSubjectID(t, topology.SubjectProjection, "claude-code.project.mcp-server", "alpha-mcp"),
	}
	for index, want := range wantOrder {
		if got := file.Locked.Subjects()[index].SubjectID(); got != want {
			t.Fatalf("subject[%d] = %#v, want %#v", index, got, want)
		}
	}

	record := file.Locked.Subjects()[0]
	assertClaudeExtensionSubjectRecord(t, record, "context7-managed", "context7@official")
	if _, ok := record.DelegatePlan(); ok {
		t.Fatal("Claude extension carrier lock must not carry delegate plan")
	}
}

func TestBuildLocksClaudeProjectAndGlobalExtensionsAsDistinctCarrierSubjects(t *testing.T) {
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			Extensions: []extension.Extension{
				testClaudeExtensionWithScope(t, "context7-project", "context7@official", target.ScopeProject),
				testClaudeExtensionWithScope(t, "context7-global", "context7@official", target.ScopeGlobal),
			},
		}),
		nil,
		Options{},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 2 {
		t.Fatalf("locked subjects = %#v, want project and global Claude plugin carriers", file.Locked.Subjects())
	}

	records := map[string]lock.LockedSubjectContract{}
	for _, record := range file.Locked.Subjects() {
		records[record.SubjectID().Key()] = record
	}
	project := records["context7-project"]
	global := records["context7-global"]
	assertClaudeExtensionSubjectRecordWithScope(t, project, "context7-project", "context7@official", target.ScopeProject)
	assertClaudeExtensionSubjectRecordWithScope(t, global, "context7-global", "context7@official", target.ScopeGlobal)

	projectRelation := delegatedRelationFromContract(t, project)
	globalRelation := delegatedRelationFromContract(t, global)
	projectExpected := projectRelation.ExpectedRelation()
	globalExpected := globalRelation.ExpectedRelation()
	if projectExpected.SubjectKey() != globalExpected.SubjectKey() {
		t.Fatalf("subject keys = %q/%q, want same host-visible plugin key", projectExpected.SubjectKey(), globalExpected.SubjectKey())
	}
	if projectExpected.ManagedInstanceKey() == globalExpected.ManagedInstanceKey() {
		t.Fatalf("managed keys collided for project/global Claude plugin rows: %q", projectExpected.ManagedInstanceKey())
	}
}

func TestBuildLocksCodexExtensionsAsPluginCarrierSubjects(t *testing.T) {
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			Extensions: []extension.Extension{
				testCodexExtension(t, "documents-managed", "documents@openai-primary-runtime"),
			},
		}),
		nil,
		Options{},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Codex plugin carrier", file.Locked.Subjects())
	}

	assertCodexExtensionSubjectRecord(t, file.Locked.Subjects()[0], "documents-managed", "documents@openai-primary-runtime")
}

func TestBuildLocksHostSourceExtensionsAsCarrierSubjects(t *testing.T) {
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			Extensions: []extension.Extension{
				testOpenCodeExtension(t, "formatter-managed", "@acme/opencode-formatter", target.ScopeGlobal),
				testPiPackageExtension(t, "tools-managed", "github:acme/pi-tools", target.ScopeProject),
				testAntigravityCLIPluginExtension(t, "guidance-managed", "modern-web-guidance@google"),
			},
		}),
		nil,
		Options{},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 3 {
		t.Fatalf("locked subjects = %#v, want OpenCode, Pi, and Antigravity carrier subjects", file.Locked.Subjects())
	}

	byName := make(map[string]lock.LockedSubjectContract, len(file.Locked.Subjects()))
	for _, contract := range file.Locked.Subjects() {
		byName[contract.EntityID().Name()] = contract
	}
	assertAntigravityCLIExtensionSubjectRecord(t, byName["guidance-managed"], "guidance-managed", "modern-web-guidance@google")
	assertOpenCodeExtensionSubjectRecord(t, byName["formatter-managed"], "formatter-managed", "@acme/opencode-formatter", target.ScopeGlobal)
	assertPiPackageExtensionSubjectRecord(t, byName["tools-managed"], "tools-managed", "github:acme/pi-tools", target.ScopeProject)
}

func TestBuildLocksClaudeExtensionMarketplaceRefWithColon(t *testing.T) {
	file, err := buildWithTestOptions(
		context.Background(),
		lockEnvironment(t, desired.Spec{
			Extensions: []extension.Extension{
				testClaudeExtension(t, "context7-managed", "team/context7:beta@official"),
			},
		}),
		nil,
		Options{},
	)
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one plugin carrier", file.Locked.Subjects())
	}
	assertClaudeExtensionSubjectRecord(t, file.Locked.Subjects()[0], "context7-managed", "team/context7:beta@official")
}

func assertOpenCodeExtensionSubjectRecord(
	t *testing.T,
	record lock.LockedSubjectContract,
	declarationID string,
	sourceRef string,
	scope target.Scope,
) {
	t.Helper()
	carrier, ok, err := lock.DelegatedRelationCarrier(record)
	if err != nil {
		t.Fatalf("OpenCodePluginCarrierSubjectFromLockedRecord returned error: %v", err)
	}
	if !ok {
		t.Fatal("OpenCodePluginCarrierSubjectFromLockedRecord returned ok=false")
	}
	route := mustExtensionOperationRoute(t, target.TargetOpenCode, extension.CarrierOpenCodePlugin, profile.OperationInstall)
	assertHostSourceExtensionSubjectRecord(t, record, carrier, extension.CarrierOpenCodePlugin, declarationID, sourceRef, target.TargetOpenCode, scope, route.RouteID(), route.AdapterContractVersion())
}

func assertPiPackageExtensionSubjectRecord(
	t *testing.T,
	record lock.LockedSubjectContract,
	declarationID string,
	sourceRef string,
	scope target.Scope,
) {
	t.Helper()
	carrier, ok, err := lock.DelegatedRelationCarrier(record)
	if err != nil {
		t.Fatalf("PiPackageCarrierSubjectFromLockedRecord returned error: %v", err)
	}
	if !ok {
		t.Fatal("PiPackageCarrierSubjectFromLockedRecord returned ok=false")
	}
	route := mustExtensionOperationRoute(t, target.TargetPi, extension.CarrierPiPackage, profile.OperationInstall)
	assertHostSourceExtensionSubjectRecord(t, record, carrier, extension.CarrierPiPackage, declarationID, sourceRef, target.TargetPi, scope, route.RouteID(), route.AdapterContractVersion())
}

func assertAntigravityCLIExtensionSubjectRecord(
	t *testing.T,
	record lock.LockedSubjectContract,
	declarationID string,
	sourceRef string,
) {
	t.Helper()
	carrier, ok, err := lock.DelegatedRelationCarrier(record)
	if err != nil {
		t.Fatalf("AntigravityCLIPluginCarrierSubjectFromLockedRecord returned error: %v", err)
	}
	if !ok {
		t.Fatal("AntigravityCLIPluginCarrierSubjectFromLockedRecord returned ok=false")
	}
	route := mustExtensionOperationRoute(t, target.TargetAntigravityCLI, extension.CarrierAntigravityCLIPlugin, profile.OperationInstall)
	assertHostSourceExtensionSubjectRecordWithRelationKey(
		t,
		record,
		carrier,
		extension.CarrierAntigravityCLIPlugin,
		declarationID,
		sourceRef,
		strings.SplitN(sourceRef, "@", 2)[0],
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		route.RouteID(),
		route.AdapterContractVersion(),
	)
}

func assertHostSourceExtensionSubjectRecord(
	t *testing.T,
	record lock.LockedSubjectContract,
	carrier extension.Carrier,
	wantCarrier extension.Carrier,
	declarationID string,
	sourceRef string,
	wantTarget target.Target,
	wantScope target.Scope,
	wantRouteID string,
	wantContractVersion string,
) {
	t.Helper()
	assertHostSourceExtensionSubjectRecordWithRelationKey(
		t,
		record,
		carrier,
		wantCarrier,
		declarationID,
		sourceRef,
		sourceRef,
		wantTarget,
		wantScope,
		wantRouteID,
		wantContractVersion,
	)
}

func assertHostSourceExtensionSubjectRecordWithRelationKey(
	t *testing.T,
	record lock.LockedSubjectContract,
	carrier extension.Carrier,
	wantCarrier extension.Carrier,
	declarationID string,
	sourceRef string,
	relationKey string,
	wantTarget target.Target,
	wantScope target.Scope,
	wantRouteID string,
	wantContractVersion string,
) {
	t.Helper()
	if carrier != wantCarrier || record.SubjectID().Key() != declarationID {
		t.Fatalf("carrier identity = %q/%q, want %q/%q", carrier, record.SubjectID(), wantCarrier, declarationID)
	}
	relation := delegatedRelationFromContract(t, record)
	if record.Ownership() != lock.OwnershipManifest ||
		record.OnAbsent() != lock.OnAbsentBlock ||
		relation.RouteContractVersion() != wantContractVersion {
		t.Fatalf("record metadata = ownership %q on_absent %q adapter %q",
			record.Ownership(), record.OnAbsent(), relation.RouteContractVersion())
	}
	if relation.Target() != wantTarget ||
		relation.Scope() != wantScope ||
		relation.SourceNamespace() != "host-source:"+sourceRef ||
		string(relation.ExpectedRelation().SubjectKey()) != relationKey ||
		!strings.HasPrefix(relation.CanonicalRequestHash(), "sha256:") ||
		relation.RouteID() != wantRouteID ||
		relation.RouteContractVersion() != wantContractVersion {
		t.Fatalf("relation = %#v, want host-source route relation", relation)
	}
}

func assertCodexExtensionSubjectRecord(
	t *testing.T,
	record lock.LockedSubjectContract,
	declarationID string,
	pluginKey string,
) {
	t.Helper()
	route := mustExtensionOperationRoute(t, target.TargetCodex, extension.CarrierCodexPlugin, profile.OperationInstall)
	carrier, ok, err := lock.DelegatedRelationCarrier(record)
	if err != nil {
		t.Fatalf("CodexPluginCarrierSubjectFromLockedRecord returned error: %v", err)
	}
	if !ok {
		t.Fatal("CodexPluginCarrierSubjectFromLockedRecord returned ok=false")
	}
	if carrier != extension.CarrierCodexPlugin || record.SubjectID().Key() != declarationID {
		t.Fatalf("carrier identity = %q/%q, want Codex carrier %q", carrier, record.SubjectID(), declarationID)
	}
	relation := delegatedRelationFromContract(t, record)
	if record.Ownership() != lock.OwnershipManifest ||
		record.OnAbsent() != lock.OnAbsentBlock ||
		relation.RouteContractVersion() != route.AdapterContractVersion() {
		t.Fatalf("record metadata = ownership %q on_absent %q adapter %q",
			record.Ownership(), record.OnAbsent(), relation.RouteContractVersion())
	}
	if relation.Target() != target.TargetCodex ||
		relation.Scope() != target.ScopeGlobal ||
		relation.SourceNamespace() != "marketplace:"+pluginKey ||
		string(relation.ExpectedRelation().SubjectKey()) != pluginKey ||
		!strings.HasPrefix(relation.CanonicalRequestHash(), "sha256:") ||
		relation.RouteID() != route.RouteID() ||
		relation.RouteContractVersion() != route.AdapterContractVersion() {
		t.Fatalf("relation = %#v, want Codex plugin carrier route relation", relation)
	}
}

func assertClaudeExtensionSubjectRecord(
	t *testing.T,
	record lock.LockedSubjectContract,
	declarationID string,
	pluginKey string,
) {
	t.Helper()
	assertClaudeExtensionSubjectRecordWithScope(t, record, declarationID, pluginKey, target.ScopeProject)
}

func assertClaudeExtensionSubjectRecordWithScope(
	t *testing.T,
	record lock.LockedSubjectContract,
	declarationID string,
	pluginKey string,
	scope target.Scope,
) {
	t.Helper()
	route := mustExtensionOperationRoute(t, target.TargetClaudeCode, extension.CarrierClaudeCodePlugin, profile.OperationInstall)
	carrier, ok, err := lock.DelegatedRelationCarrier(record)
	if err != nil {
		t.Fatalf("ClaudePluginCarrierSubjectFromLockedRecord returned error: %v", err)
	}
	if !ok {
		t.Fatal("ClaudePluginCarrierSubjectFromLockedRecord returned ok=false")
	}
	if carrier != extension.CarrierClaudeCodePlugin || record.SubjectID().Key() != declarationID {
		t.Fatalf("carrier identity = %q/%q, want Claude carrier %q", carrier, record.SubjectID(), declarationID)
	}
	relation := delegatedRelationFromContract(t, record)
	if record.Ownership() != lock.OwnershipManifest ||
		record.OnAbsent() != lock.OnAbsentBlock ||
		relation.RouteContractVersion() != route.AdapterContractVersion() {
		t.Fatalf("record metadata = ownership %q on_absent %q adapter %q",
			record.Ownership(), record.OnAbsent(), relation.RouteContractVersion())
	}
	if relation.Target() != target.TargetClaudeCode ||
		relation.Scope() != scope ||
		relation.SourceNamespace() != "marketplace:"+pluginKey ||
		string(relation.ExpectedRelation().SubjectKey()) != pluginKey ||
		!strings.HasPrefix(relation.CanonicalRequestHash(), "sha256:") ||
		relation.RouteID() == "" ||
		relation.RouteContractVersion() != route.AdapterContractVersion() {
		t.Fatalf("relation = %#v, want Claude plugin carrier route relation", relation)
	}
}

func delegatedRelationFromContract(t *testing.T, contract lock.LockedSubjectContract) realization.DelegatedRelation {
	t.Helper()
	realization, ok := contract.Realization()
	if !ok {
		t.Fatal("locked carrier contract is missing realization")
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		t.Fatalf("realization kind = %q, want delegated relation", realization.Kind())
	}
	return relation
}

func mustExtensionOperationRoute(
	t *testing.T,
	selectedTarget target.Target,
	carrier extension.Carrier,
	operation profile.Operation,
) profile.OperationRoute {
	t.Helper()
	routeProfile, ok := profile.Profile(selectedTarget).DelegatedRoute(carrier)
	if !ok {
		t.Fatalf("target profile %q is missing delegated route %q", selectedTarget, carrier)
	}
	route, ok := routeProfile.OperationRoute(operation)
	if !ok {
		t.Fatalf("delegated route %q is missing operation %q", carrier, operation)
	}
	return route
}

func testClaudeExtension(t *testing.T, declarationID string, marketplace string) extension.Extension {
	t.Helper()
	return testClaudeExtensionWithScope(t, declarationID, marketplace, target.ScopeProject)
}

func testClaudeExtensionWithScope(t *testing.T, declarationID string, marketplace string, scope target.Scope) extension.Extension {
	t.Helper()
	return testExtension(t, declarationID, extension.CarrierClaudeCodePlugin, target.TargetClaudeCode, scope, extension.SourceKindMarketplace, marketplace)
}

func testCodexExtension(t *testing.T, declarationID string, marketplaceSelector string) extension.Extension {
	t.Helper()
	return testExtension(t, declarationID, extension.CarrierCodexPlugin, target.TargetCodex, target.ScopeGlobal, extension.SourceKindMarketplace, marketplaceSelector)
}

func testOpenCodeExtension(t *testing.T, declarationID string, sourceRef string, scope target.Scope) extension.Extension {
	t.Helper()
	return testExtension(t, declarationID, extension.CarrierOpenCodePlugin, target.TargetOpenCode, scope, extension.SourceKindHostSource, sourceRef)
}

func testPiPackageExtension(t *testing.T, declarationID string, sourceRef string, scope target.Scope) extension.Extension {
	t.Helper()
	return testExtension(t, declarationID, extension.CarrierPiPackage, target.TargetPi, scope, extension.SourceKindHostSource, sourceRef)
}

func testAntigravityCLIPluginExtension(t *testing.T, declarationID string, sourceRef string) extension.Extension {
	t.Helper()
	return testExtension(t, declarationID, extension.CarrierAntigravityCLIPlugin, target.TargetAntigravityCLI, target.ScopeGlobal, extension.SourceKindHostSource, sourceRef)
}

func testExtension(
	t *testing.T,
	declarationID string,
	carrier extension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceKind extension.SourceKind,
	sourceRef string,
) extension.Extension {
	t.Helper()
	return desiredtest.Extension(t, extension.Spec{
		Name:    declarationID,
		Carrier: carrier,
		Target:  selectedTarget,
		Scope:   scope,
		Source:  desiredtest.ExtensionSource(t, sourceKind, sourceRef),
	})
}
