package diagnose

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestMCPExecutableRequirementChecksReportMissingRunnerFamilies(t *testing.T) {
	selection := mustPrerequisiteSelection(t, "claude-code")
	setMCPExecutableLookupPath(t)

	checks := MCPExecutableRequirementChecks([]desiredmcp.Server{
		prerequisiteMCPServer(t, "npm-server", "npx", []string{"-y", "@upstash/context7-mcp"}),
		prerequisiteMCPServer(t, "python-server", "uvx", []string{"mcp-server"}),
		prerequisiteMCPServer(t, "container-server", "docker", []string{"run", "ghcr.io/acme/server:latest"}),
		prerequisiteMCPServer(t, "plain-server", "node", []string{"server.js"}),
	}, selection)

	assertPrerequisiteWarnContains(t, checks, "target=claude-code scope=project mcp_server=npm-server executable_requirement=command", `command "npx" is not discoverable`)
	assertPrerequisiteWarnContains(t, checks, "target=claude-code scope=project mcp_server=python-server executable_requirement=command", `command "uvx" is not discoverable`)
	assertPrerequisiteWarnContains(t, checks, "target=claude-code scope=project mcp_server=container-server executable_requirement=command", `command "docker" is not discoverable`)
	check := assertPrerequisiteWarnContains(t, checks, "target=claude-code scope=project mcp_server=plain-server executable_requirement=command", `command "node" is not discoverable`)
	assertPrerequisiteTerminology(t, check)
}

func TestMCPExecutableRequirementChecksReportMissingEnvRefsWithoutValues(t *testing.T) {
	selection := mustPrerequisiteSelection(t, "claude-code")
	setMCPExecutableLookupPath(t, "node")
	t.Setenv("PRESENT_TOKEN", "secret-value")
	unsetMCPRequirementEnv(t, "MISSING_TOKEN")

	server := prerequisiteMCPServerWith(t, "env-server", target.TargetClaudeCode, target.ScopeProject, "node", []string{"server.js"}, map[string]string{
		"API_TOKEN": "MISSING_TOKEN", "DISPLAY_TOKEN": "PRESENT_TOKEN",
	})
	checks := MCPExecutableRequirementChecks([]desiredmcp.Server{server}, selection)

	check := assertPrerequisiteWarnContains(t, checks, "target=claude-code scope=project mcp_server=env-server executable_requirement=env_refs", "MISSING_TOKEN")
	assertPrerequisiteTerminology(t, check)
	if strings.Contains(check.Detail, "secret-value") || strings.Contains(check.Detail, "PRESENT_TOKEN") {
		t.Fatalf("detail = %q, want missing env names only without values or present refs", check.Detail)
	}
}

func TestMCPExecutableRequirementChecksFilterBySelection(t *testing.T) {
	selection := mustPrerequisiteSelection(t, "codex")
	checks := MCPExecutableRequirementChecks([]desiredmcp.Server{prerequisiteMCPServer(t, "context7", "npx", []string{"@upstash/context7-mcp"})}, selection)
	if len(checks) != 0 {
		t.Fatalf("checks = %#v, want no checks for unselected target", checks)
	}
}

func TestMCPExecutableRequirementChecksCoverCurrentAdmittedRows(t *testing.T) {
	selection := mustPrerequisiteSelection(t, "codex", "opencode", "antigravity-cli")
	seen := make([]string, 0)
	environment := mcpExecutableEnvironment{
		lookPath: func(command string) (string, error) {
			seen = append(seen, command)
			return "", errors.New("not found")
		},
		lookupEnv: func(string) (string, bool) {
			t.Fatal("lookupEnv must not run for rows with no env references")
			return "", false
		},
	}

	servers := []desiredmcp.Server{
		prerequisiteMCPServerWith(t, "codex-context7", target.TargetCodex, target.ScopeProject, "npx", []string{"-y", "@upstash/context7-mcp"}, nil),
		prerequisiteMCPServerWith(t, "opencode-context7", target.TargetOpenCode, target.ScopeProject, "npx", []string{"-y", "@upstash/context7-mcp"}, nil),
		prerequisiteMCPServerWith(t, "antigravity-context7", target.TargetAntigravityCLI, target.ScopeGlobal, "npx", []string{"-y", "@upstash/context7-mcp"}, nil),
	}
	checks := mcpExecutableRequirementChecks(servers, selection, environment)

	assertPrerequisiteWarnContains(t, checks, "target=codex scope=project mcp_server=codex-context7 executable_requirement=command", `command "npx" is not discoverable`)
	assertPrerequisiteWarnContains(t, checks, "target=opencode scope=project mcp_server=opencode-context7 executable_requirement=command", `command "npx" is not discoverable`)
	assertPrerequisiteWarnContains(t, checks, "target=antigravity-cli scope=global mcp_server=antigravity-context7 executable_requirement=command", `command "npx" is not discoverable`)
	if len(seen) != 3 {
		t.Fatalf("lookPath calls = %d, want one per admitted row", len(seen))
	}
}

func TestMCPExecutableRequirementChecksPreserveAbsolutePathSemantics(t *testing.T) {
	selection := mustPrerequisiteSelection(t, "antigravity-cli")
	absolutePath := filepath.Join(t.TempDir(), "bin with spaces", "codegraph")
	command, err := desiredmcp.NewAbsolutePathCommand(absolutePath)
	if err != nil {
		t.Fatalf("NewAbsolutePathCommand returned error: %v", err)
	}
	transport, err := desiredmcp.NewStdioTransport(command, []string{"serve", "--mcp"}, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport returned error: %v", err)
	}
	binding, err := desiredmcp.NewBinding(
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	if err != nil {
		t.Fatalf("NewBinding returned error: %v", err)
	}
	server, err := desiredmcp.New(desiredmcp.Spec{
		Name:     "codegraph",
		Bindings: []desiredmcp.Binding{binding},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	var lookedUp string
	checks := mcpExecutableRequirementChecks(
		[]desiredmcp.Server{server},
		selection,
		mcpExecutableEnvironment{
			lookPath: func(executable string) (string, error) {
				lookedUp = executable
				return "", errors.New("missing")
			},
			lookupEnv: func(string) (string, bool) {
				t.Fatal("lookupEnv must not run without env references")
				return "", false
			},
		},
	)
	check := assertPrerequisiteWarnContains(
		t,
		checks,
		"target=antigravity-cli scope=global mcp_server=codegraph executable_requirement=command",
		`exact path "`+absolutePath+`" is not currently executable`,
	)
	if lookedUp != absolutePath {
		t.Fatalf("lookPath command = %q, want exact path %q", lookedUp, absolutePath)
	}
	if strings.Contains(check.Detail, "PATH") {
		t.Fatalf("detail = %q, absolute path must not be described as PATH resolution", check.Detail)
	}
	assertPrerequisiteTerminology(t, check)
}

func prerequisiteMCPServer(t *testing.T, id string, command string, args []string) desiredmcp.Server {
	t.Helper()
	return prerequisiteMCPServerWith(t, id, target.TargetClaudeCode, target.ScopeProject, command, args, nil)
}

func prerequisiteMCPServerWith(
	t *testing.T,
	id string,
	selected target.Target,
	scope target.Scope,
	command string,
	args []string,
	env map[string]string,
) desiredmcp.Server {
	t.Helper()
	references := make(map[string]desiredmcp.EnvReference, len(env))
	for name, fromEnv := range env {
		references[name] = testfixture.MCPEnvReference(t, fromEnv)
	}
	transport := testfixture.MCPStdio(t, testfixture.MCPCommand(t, command), args, references)
	binding := testfixture.MCPBinding(t, selected, scope, transport, desiredmcp.OnAbsentRemoveBinding)
	return testfixture.MCPServer(t, desiredmcp.Spec{Name: id, Bindings: []desiredmcp.Binding{binding}})
}

func mustPrerequisiteSelection(t *testing.T, values ...string) targetselection.Selection {
	t.Helper()
	selection, err := targetselection.ForDiagnostics(values)
	if err != nil {
		t.Fatalf("ForDiagnostics returned error: %v", err)
	}
	return selection
}

func assertPrerequisiteWarnContains(t *testing.T, checks []findings.Check, name string, detail string) findings.Check {
	t.Helper()
	check := assertPrerequisiteCheck(t, checks, name)
	if check.Status != findings.CheckWarn {
		t.Fatalf("severity for %q = %s, want warn; detail = %q", name, check.Status, check.Detail)
	}
	if !strings.Contains(check.Detail, detail) {
		t.Fatalf("detail for %q = %q, want substring %q", name, check.Detail, detail)
	}
	return check
}

func assertPrerequisiteCheck(t *testing.T, checks []findings.Check, name string) findings.Check {
	t.Helper()
	for _, check := range checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing check %q in %#v", name, checks)
	return findings.Check{}
}

func assertPrerequisiteTerminology(t *testing.T, check findings.Check) {
	t.Helper()
	if !strings.Contains(check.Detail, "prerequisite") {
		t.Fatalf("detail = %q, want MCP executable prerequisite terminology", check.Detail)
	}
	if strings.Contains(check.Detail, "readiness") {
		t.Fatalf("detail = %q, want no runtime-readiness wording", check.Detail)
	}
}
