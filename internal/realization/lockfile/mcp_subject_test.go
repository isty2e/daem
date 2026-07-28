package lockfile

import (
	"strings"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	mcpdelegate "github.com/isty2e/daem/internal/realization/delegate/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
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
		`identity_key = "delegate:v2:`,
		`runner_kind = "npx"`,
		`command = "npx"`,
		`pin_policy = "floating"`,
		"[[locked.subject.delegate_plan.env]]",
		`name = "API_TOKEN"`,
		`source_name = "CONTEXT7_API_TOKEN"`,
		"[locked.subject.delegate_plan.package]",
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
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered lockfile leaked forbidden or legacy value %q:\n%s", forbidden, rendered)
		}
	}

	loaded, err := Load(writeLockfileText(t, rendered))
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
	graph, err := topologymcp.Servers([]desiredmcp.Server{server})
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

	loaded, err := Load(writeLockfileText(t, rendered))
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
			_, err := Load(writeLockfileText(t, tampered))
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
	t.Helper()
	env := map[string]desiredmcp.EnvReference{
		"API_TOKEN": desiredtest.MCPEnvReference(t, "CONTEXT7_API_TOKEN"),
	}
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "npx"),
		[]string{"-y", "@upstash/context7-mcp"},
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
	graph, err := topologymcp.Servers([]desiredmcp.Server{server})
	if err != nil {
		t.Fatalf("Servers returned error: %v", err)
	}
	delegatePlan, err := mcpdelegate.MCPBindingDelegatePlan(server, binding)
	if err != nil {
		t.Fatalf("MCPBindingDelegatePlan returned error: %v", err)
	}
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID:        serverName,
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
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
		LauncherCommand:      "npx",
		LauncherArgs:         []string{"-y", "@upstash/context7-mcp"},
		CanonicalProjection:  string(canonical),
		DelegatePlan:         &delegatePlan,
		CredentialReferences: []string{"CONTEXT7_API_TOKEN"},
	})
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}
	return contract
}
