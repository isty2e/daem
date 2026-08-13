package lockfile

import (
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	mcpdelegate "github.com/isty2e/daem/internal/realization/delegate/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestMarshalAndLoadClaudeProjectMCPSubjectLockfile(t *testing.T) {
	contract := claudeProjectMCPSubjectContract(t)
	file := lockfileWithSubjects(t, contract)
	content, err := Marshal(file)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	rendered := string(content)
	assertInOrder(t, rendered, []string{
		"[[locked.subject]]",
		`entity_id = "mcp_server:context7"`,
		`subject_id = "projection/claude-code.project.mcp-server/context7"`,
		`mcp_environment_sources = ["CONTEXT7_API_TOKEN"]`,
		`ownership = "manifest"`,
		`on_absent = "remove_binding"`,
		"[locked.subject.realization]",
		"[locked.subject.realization.managed_aggregate]",
		`placement_id = "claude-code.project.project-config"`,
		`aggregate_root = ".mcp.json"`,
		`content_path = "/mcpServers/context7"`,
		`contribution_cardinality = "exclusive"`,
		`sibling_retention = "preserve_unmanaged_siblings"`,
		`sibling_preservation = "canonical_semantic"`,
		`equivalence = "canonical_semantic"`,
		`canonical_contribution = `,
		`codec_contract = "claude-project-mcp-stdio-v1"`,
		"[locked.subject.delegate_plan]",
		`identity_key = "delegate:v3:`,
		`runner_kind = "npx"`,
		`command = "npx"`,
		`pin_policy = "floating"`,
		"[[locked.subject.delegate_plan.env]]",
		`name = "API_TOKEN"`,
		`source_name = "CONTEXT7_API_TOKEN"`,
		"[[locked.subject.delegate_plan.package]]",
		`ecosystem = "npm"`,
		`name = "@upstash/context7-mcp"`,
	})
	for _, operation := range []string{`operation = "observe"`, `operation = "remove_projection"`, `operation = "write_projection"`} {
		if !strings.Contains(rendered, operation) {
			t.Fatalf("rendered lockfile is missing %q:\n%s", operation, rendered)
		}
	}
	for _, forbidden := range []string{
		"literal-secret", "CONTEXT7_API_TOKEN_VALUE", "access_token", "refresh_token",
		"mcp-needs-auth-cache", "Status: Connected", "[locked.subject.claim]", "config_path =",
		"mcp_provider_contribution",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered lockfile leaked forbidden or legacy value %q:\n%s", forbidden, rendered)
		}
	}

	loaded, err := Load(t.Context(), writeLockfileText(t, rendered))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	assertLockedSubjectsEqual(t, loaded.Locked.Subjects(), file.Locked.Subjects())
	loadedDelegate, ok := loaded.Locked.Subjects()[0].DelegatePlan()
	if !ok {
		t.Fatal("loaded contract is missing delegate plan")
	}
	loadedEnv := loadedDelegate.Env().Bindings()
	if len(loadedEnv) != 1 ||
		loadedEnv[0].Name() != "API_TOKEN" ||
		loadedEnv[0].SourceName() != "CONTEXT7_API_TOKEN" {
		t.Fatalf("loaded delegate env = %#v, want API_TOKEN <- CONTEXT7_API_TOKEN", loadedEnv)
	}
	originalDelegate, _ := contract.DelegatePlan()
	if loadedDelegate.IdentityKey() != originalDelegate.IdentityKey() {
		t.Fatalf("loaded delegate identity key = %q, want %q", loadedDelegate.IdentityKey(), originalDelegate.IdentityKey())
	}
	if got := loaded.Locked.Subjects()[0].MCPEnvironmentSources(); len(got) != 1 || got[0] != "CONTEXT7_API_TOKEN" {
		t.Fatalf("loaded MCP environment sources = %#v", got)
	}
}

func TestMarshalDelegatePinPolicyReflectsSelectorAssurance(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name       string
		command    string
		args       []string
		wantPolicy string
	}{
		{name: "npm exact", command: "npx", args: []string{"-y", "@scope/server@1.2.3"}, wantPolicy: "pinned"},
		{name: "npm unsafe numeric", command: "npx", args: []string{"-y", "@scope/server@9007199254740992.0.0"}, wantPolicy: "floating"},
		{name: "npm range", command: "npx", args: []string{"-y", "@scope/server@^1.2.3"}, wantPolicy: "floating"},
		{name: "python exact", command: "uvx", args: []string{"server==1.2rc1"}, wantPolicy: "pinned"},
		{name: "python range", command: "uvx", args: []string{"server>=1.0,<2"}, wantPolicy: "floating"},
		{name: "python extras", command: "uvx", args: []string{"--from", "mypy[faster-cache,reports]==1.13.0", "mypy"}, wantPolicy: "floating"},
		{name: "python git", command: "uvx", args: []string{"--from", "git+https://github.com/httpie/cli", "http"}, wantPolicy: "floating"},
		{name: "container digest", command: "docker", args: []string{"run", "ghcr.io/acme/server@" + digest}, wantPolicy: "pinned"},
		{name: "container tag", command: "docker", args: []string{"run", "ghcr.io/acme/server:1.2.3"}, wantPolicy: "floating"},
		{name: "container boolean before image", command: "docker", args: []string{"run", "--sig-proxy", "ghcr.io/acme/server:latest", "helper@" + digest}, wantPolicy: "floating"},
		{name: "container malformed digest", command: "docker", args: []string{"run", "ghcr.io/acme/server@sha256:abc123"}, wantPolicy: "floating"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract := claudeProjectMCPSubjectContractForCommand(t, "context7", test.command, test.args)
			content, err := Marshal(lockfileWithSubjects(t, contract))
			if err != nil {
				t.Fatalf("Marshal returned error: %v", err)
			}
			fragment := `pin_policy = "` + test.wantPolicy + `"`
			if !strings.Contains(string(content), fragment) {
				t.Fatalf("rendered lockfile is missing %q:\n%s", fragment, content)
			}
			loaded, err := Load(t.Context(), writeLockfileText(t, string(content)))
			if err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
			plan, ok := loaded.Locked.Subjects()[0].DelegatePlan()
			if !ok || string(plan.PinPolicy()) != test.wantPolicy {
				t.Fatalf("loaded pin policy = %q, %t, want %q", plan.PinPolicy(), ok, test.wantPolicy)
			}
		})
	}
}

func TestMarshalAndLoadPreservesEveryDelegatedPackageInput(t *testing.T) {
	contract := claudeProjectMCPSubjectContractForCommand(t, "context7", "npx", []string{
		"--package=server@1.2.3",
		"--package=helper@latest",
		"server",
	})
	content, err := Marshal(lockfileWithSubjects(t, contract))
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if got := strings.Count(string(content), "[[locked.subject.delegate_plan.package]]"); got != 2 {
		t.Fatalf("delegated package tables = %d, want 2:\n%s", got, content)
	}
	if !strings.Contains(string(content), `pin_policy = "floating"`) {
		t.Fatalf("multi-package lockfile does not record aggregate floating assurance:\n%s", content)
	}

	loaded, err := Load(t.Context(), writeLockfileText(t, string(content)))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	plan, ok := loaded.Locked.Subjects()[0].DelegatePlan()
	if !ok {
		t.Fatal("loaded contract is missing delegate plan")
	}
	refs := plan.PackageRefs()
	if len(refs) != 2 || refs[0].Name() != "helper" || refs[1].Name() != "server" {
		t.Fatalf("loaded package refs = %#v, want canonical helper/server set", refs)
	}
}

func TestMarshalAndLoadPiMCPProviderForeignKey(t *testing.T) {
	provider := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "pi-mcp-adapter-project",
		Carrier: desiredextension.CarrierPiPackage,
		Target:  target.TargetPi,
		Scope:   target.ScopeProject,
		Source: desiredtest.ExtensionSource(
			t,
			desiredextension.SourceKindHostSource,
			"npm:pi-mcp-adapter@^2.13.0",
		),
	})
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "node"),
		[]string{"server.js"},
		map[string]desiredmcp.EnvReference{
			"API_TOKEN": desiredtest.MCPEnvReference(t, "CONTEXT7_API_TOKEN"),
		},
	)
	binding := desiredtest.MCPBinding(
		t,
		target.TargetPi,
		target.ScopeProject,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{
		Name:     "context7",
		Bindings: []desiredmcp.Binding{binding},
	})
	providerContracts, err := lockrefine.Extensions([]desiredextension.Extension{provider})
	if err != nil {
		t.Fatalf("Extensions returned error: %v", err)
	}
	mcpContracts, err := lockrefine.MCPSubjects(
		[]desiredmcp.Server{server},
		[]desiredextension.Extension{provider},
		mcpcodec.CanonicalMCPBindingContribution,
	)
	if err != nil {
		t.Fatalf("MCPSubjects returned error: %v", err)
	}
	if len(providerContracts) != 1 || len(mcpContracts) != 1 {
		t.Fatalf(
			"refined contracts = provider %d MCP %d, want one each",
			len(providerContracts),
			len(mcpContracts),
		)
	}
	reference, present := mcpContracts[0].MCPProviderContribution()
	if !present {
		t.Fatal("Pi MCP contract is missing its provider contribution")
	}
	file := lockfileWithSubjects(t, providerContracts[0], mcpContracts[0])

	content, err := Marshal(file)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	rendered := string(content)
	for _, fragment := range []string{
		`mcp_provider_contribution = "` + reference.SubjectID().String() + `"`,
		`placement_id = "pi.project.pi-config"`,
		`codec_contract = "pi-mcp-adapter-stdio-v1"`,
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("rendered Pi lockfile is missing %q:\n%s", fragment, rendered)
		}
	}
	providerLine := `mcp_provider_contribution = "` + reference.SubjectID().String() + `"`
	if strings.Contains(providerLine, string(aggregate.MCPCodecPiAdapterStdio)) {
		t.Fatalf("provider foreign key copied codec identity: %s", providerLine)
	}

	loaded, err := Load(t.Context(), writeLockfileText(t, rendered))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	assertLockedSubjectsEqual(t, loaded.Locked.Subjects(), file.Locked.Subjects())
	var loadedReferenceFound bool
	for _, subject := range loaded.Locked.Subjects() {
		loadedReference, ok := subject.MCPProviderContribution()
		if !ok {
			continue
		}
		loadedReferenceFound = true
		if !loadedReference.Equal(reference) {
			t.Fatalf(
				"loaded provider contribution = %q, want %q",
				loadedReference.SubjectID(),
				reference.SubjectID(),
			)
		}
	}
	if !loadedReferenceFound {
		t.Fatal("loaded Pi MCP contract is missing its provider contribution")
	}
}

func TestMarshalAndLoadAntigravityAmbientEnvironmentSourcesWithoutNativeEnv(t *testing.T) {
	const sourceName = "CONTEXT7_API_TOKEN"
	env := map[string]desiredmcp.EnvReference{
		sourceName: desiredtest.MCPEnvReference(t, sourceName),
	}
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "npx"),
		[]string{"-y", "@upstash/context7-mcp"},
		env,
	)
	binding := desiredtest.MCPBinding(
		t,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{
		Name:     "context7",
		Bindings: []desiredmcp.Binding{binding},
	})
	graph, err := topologymcp.ServersWithProviderSelections([]desiredmcp.Server{server}, nil)
	if err != nil {
		t.Fatalf("Servers returned error: %v", err)
	}
	placement, ok := aggregate.MCPPlacementForID(aggregate.MCPPlacementAntigravityGlobal)
	if !ok {
		t.Fatal("Antigravity global MCP placement is missing")
	}
	canonical, err := mcpcodec.CanonicalMCPBindingContribution(server, binding, placement)
	if err != nil {
		t.Fatalf("CanonicalMCPBindingContribution returned error: %v", err)
	}
	contract, err := lock.NewMCPProjectionSubjectContract(lock.MCPProjectionSubjectInput{
		Graph:                graph,
		EntityID:             server.ID(),
		PlacementID:          aggregate.MCPPlacementAntigravityGlobal,
		ServerID:             server.ID().Name(),
		RequestedOnAbsent:    desiredmcp.OnAbsentRemoveBinding,
		LauncherCommand:      "npx",
		LauncherArgs:         []string{"-y", "@upstash/context7-mcp"},
		CanonicalProjection:  string(canonical),
		CredentialReferences: []string{sourceName},
	})
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}

	content, err := Marshal(lockfileWithSubjects(t, contract))
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	rendered := string(content)
	if !strings.Contains(rendered, `mcp_environment_sources = ["`+sourceName+`"]`) {
		t.Fatalf("rendered lockfile is missing ambient environment source:\n%s", rendered)
	}
	for _, forbidden := range []string{"delegate_plan", `"env"`, "${" + sourceName + "}"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered lockfile contains forbidden Antigravity field %q:\n%s", forbidden, rendered)
		}
	}

	loaded, err := Load(t.Context(), writeLockfileText(t, rendered))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := loaded.Locked.Subjects()[0].MCPEnvironmentSources(); len(got) != 1 || got[0] != sourceName {
		t.Fatalf("loaded Antigravity environment sources = %#v", got)
	}
	if _, present := loaded.Locked.Subjects()[0].DelegatePlan(); present {
		t.Fatal("loaded Antigravity ambient row unexpectedly has a delegate plan")
	}
}

func TestLoadRejectsNonCanonicalOrInvalidMCPEnvironmentSources(t *testing.T) {
	content, err := Marshal(lockfileWithSubjects(t, claudeProjectMCPSubjectContract(t)))
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	canonical := string(content)

	for _, test := range []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "duplicate source",
			content: duplicateFirstStringArrayFieldValue(t, canonical, "mcp_environment_sources"),
			want:    "duplicate MCP environment source",
		},
		{
			name: "invalid source name",
			content: strings.Replace(
				canonical,
				`mcp_environment_sources = ["CONTEXT7_API_TOKEN"]`,
				`mcp_environment_sources = ["9TOKEN"]`,
				1,
			),
			want: "must not start with a digit",
		},
		{
			name: "non-canonical whitespace",
			content: strings.Replace(
				canonical,
				`mcp_environment_sources = ["CONTEXT7_API_TOKEN"]`,
				`mcp_environment_sources = [" CONTEXT7_API_TOKEN"]`,
				1,
			),
			want: "non-canonical values",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.content == canonical {
				t.Fatal("tampered fixture matches canonical lockfile")
			}
			_, err := Load(t.Context(), writeLockfileText(t, test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadRejectsDelegateEnvironmentBindingTampering(t *testing.T) {
	content, err := Marshal(lockfileWithSubjects(t, claudeProjectMCPSubjectContract(t)))
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	for _, test := range []struct {
		name string
		old  string
		new  string
	}{
		{name: "child name", old: `name = "API_TOKEN"`, new: `name = "RENAMED_TOKEN"`},
		{name: "source name", old: `source_name = "CONTEXT7_API_TOKEN"`, new: `source_name = "OTHER_TOKEN"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			tampered := strings.Replace(string(content), test.old, test.new, 1)
			if tampered == string(content) {
				t.Fatalf("fixture does not contain %q", test.old)
			}
			_, err := Load(t.Context(), writeLockfileText(t, tampered))
			if err == nil || !strings.Contains(err.Error(), "identity key does not match") {
				t.Fatalf("Load error = %v, want delegate identity mismatch", err)
			}
		})
	}
}

func claudeProjectMCPSubjectContract(t *testing.T) lock.LockedSubjectContract {
	return claudeProjectMCPSubjectContractNamed(t, "context7")
}

func claudeProjectMCPSubjectContractNamed(t *testing.T, serverName string) lock.LockedSubjectContract {
	return claudeProjectMCPSubjectContractForCommand(
		t,
		serverName,
		"npx",
		[]string{"-y", "@upstash/context7-mcp"},
	)
}

func claudeProjectMCPSubjectContractForCommand(
	t *testing.T,
	serverName string,
	command string,
	args []string,
) lock.LockedSubjectContract {
	t.Helper()
	env := map[string]desiredmcp.EnvReference{
		"API_TOKEN": desiredtest.MCPEnvReference(t, "CONTEXT7_API_TOKEN"),
	}
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, command),
		args,
		env,
	)
	binding := desiredtest.MCPBinding(
		t,
		target.TargetClaudeCode,
		target.ScopeProject,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{Name: serverName, Bindings: []desiredmcp.Binding{binding}})
	graph, err := topologymcp.ServersWithProviderSelections([]desiredmcp.Server{server}, nil)
	if err != nil {
		t.Fatalf("Servers returned error: %v", err)
	}
	delegatePlan, err := mcpdelegate.MCPBindingDelegatePlan(server, binding)
	if err != nil {
		t.Fatalf("MCPBindingDelegatePlan returned error: %v", err)
	}
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID:        serverName,
		Command:         command,
		Args:            args,
		Env:             map[string]string{"API_TOKEN": "${CONTEXT7_API_TOKEN}"},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	contract, err := lock.NewMCPProjectionSubjectContract(lock.MCPProjectionSubjectInput{
		Graph:                graph,
		EntityID:             server.ID(),
		PlacementID:          aggregate.MCPPlacementClaudeProject,
		ServerID:             serverName,
		RequestedOnAbsent:    desiredmcp.OnAbsentRemoveBinding,
		LauncherCommand:      command,
		LauncherArgs:         args,
		CanonicalProjection:  string(canonical),
		DelegatePlan:         &delegatePlan,
		CredentialReferences: []string{"CONTEXT7_API_TOKEN"},
	})
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}
	return contract
}
