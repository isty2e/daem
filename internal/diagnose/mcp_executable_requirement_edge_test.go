package diagnose

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/findings"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
)

func TestMCPExecutableRequirementEdgeHuntRoundOneSecretsAndDefaults(t *testing.T) {
	selection := mustPrerequisiteSelection(t, "claude-code")

	lookupDirectory := setMCPExecutableLookupPath(t, "node")
	t.Setenv("HOST_SECRET", "secret-value")

	server := prerequisiteMCPServerWith(t, "secret-safe", "claude-code", "project", "node", []string{"server.js"}, map[string]string{"SERVER_SECRET": "HOST_SECRET"})
	checks := MCPExecutableRequirementChecks([]desiredmcp.Server{server}, selection)
	runnerCheck := assertPrerequisiteCheck(t, checks, "target=claude-code scope=project mcp_server=secret-safe executable_requirement=command")
	if strings.Contains(runnerCheck.Detail, lookupDirectory) {
		t.Fatalf("runner detail leaks lookup path: %q", runnerCheck.Detail)
	}
	envCheck := assertPrerequisiteCheck(t, checks, "target=claude-code scope=project mcp_server=secret-safe executable_requirement=env_refs")
	if strings.Contains(envCheck.Detail, "secret-value") || strings.Contains(envCheck.Detail, "SERVER_SECRET") {
		t.Fatalf("env detail leaks value or projection key: %q", envCheck.Detail)
	}

	setMCPExecutableLookupPath(t)
	checks = MCPExecutableRequirementChecks([]desiredmcp.Server{prerequisiteMCPServer(t, "missing", "node", []string{"server.js"})}, selection)
	missingCheck := assertPrerequisiteCheck(t, checks, "target=claude-code scope=project mcp_server=missing executable_requirement=command")
	if strings.Contains(missingCheck.Detail, lookupDirectory) {
		t.Fatalf("missing runner detail leaks prior lookup path: %q", missingCheck.Detail)
	}
	if got := MCPExecutableRequirementChecks(nil, selection); len(got) != 0 {
		t.Fatalf("checks = %#v, want none", got)
	}
	defaultSelection, err := targetselection.ForDiagnostics(nil)
	if err != nil {
		t.Fatalf("ForDiagnostics(nil) returned error: %v", err)
	}
	checks = MCPExecutableRequirementChecks([]desiredmcp.Server{prerequisiteMCPServer(t, "default-selection", "node", []string{"server.js"})}, defaultSelection)
	assertPrerequisiteCheck(t, checks, "target=claude-code scope=project mcp_server=default-selection executable_requirement=command")
}

func TestMCPExecutableRequirementEdgeHuntRoundTwoEnvIdentity(t *testing.T) {
	selection := mustPrerequisiteSelection(t, "claude-code")
	setMCPExecutableLookupPath(t, "node")
	unsetMCPRequirementEnv(t, "SHARED_TOKEN")
	unsetMCPRequirementEnv(t, "A_TOKEN")
	unsetMCPRequirementEnv(t, "Z_TOKEN")

	server := prerequisiteMCPServerWith(t, "dedupe", "claude-code", "project", "node", []string{"server.js"}, map[string]string{
		"FIRST_SECRET": "SHARED_TOKEN", "SECOND_SECRET": "SHARED_TOKEN",
	})
	checks := MCPExecutableRequirementChecks([]desiredmcp.Server{server}, selection)
	check := assertPrerequisiteCheck(t, checks, "target=claude-code scope=project mcp_server=dedupe executable_requirement=env_refs")
	if strings.Count(check.Detail, "SHARED_TOKEN") != 1 || strings.Contains(check.Detail, "FIRST_SECRET") || strings.Contains(check.Detail, "SECOND_SECRET") {
		t.Fatalf("detail = %q, want one deduped host env ref only", check.Detail)
	}

	server = prerequisiteMCPServerWith(t, "sorted", "claude-code", "project", "node", []string{"server.js"}, map[string]string{
		"Z_SECRET": "Z_TOKEN", "A_SECRET": "A_TOKEN",
	})
	checks = MCPExecutableRequirementChecks([]desiredmcp.Server{server}, selection)
	check = assertPrerequisiteCheck(t, checks, "target=claude-code scope=project mcp_server=sorted executable_requirement=env_refs")
	if !strings.Contains(check.Detail, "A_TOKEN, Z_TOKEN") {
		t.Fatalf("detail = %q, want deterministic missing env order", check.Detail)
	}

	checks = MCPExecutableRequirementChecks([]desiredmcp.Server{prerequisiteMCPServer(t, "no-env", "node", []string{"server.js"})}, selection)
	check = assertPrerequisiteCheck(t, checks, "target=claude-code scope=project mcp_server=no-env executable_requirement=env_refs")
	if check.Status != findings.CheckOK || !strings.Contains(check.Detail, "requires no environment references") {
		t.Fatalf("check = %#v, want explicit ok no-env prerequisite", check)
	}

	codexSelection := mustPrerequisiteSelection(t, "codex")
	if got := mcpExecutableRequirementChecks(
		[]desiredmcp.Server{prerequisiteMCPServer(t, "filtered", "node", []string{"server.js"})},
		codexSelection,
		mcpExecutableEnvironment{
			lookPath: func(string) (string, error) {
				t.Fatal("lookPath must not run for unselected MCP target")
				return "", nil
			},
			lookupEnv: func(string) (string, bool) {
				t.Fatal("lookupEnv must not run for unselected MCP target")
				return "", false
			},
		},
	); len(got) != 0 {
		t.Fatalf("checks = %#v, want none for unselected target", got)
	}
}

func TestMCPExecutableRequirementEdgeHuntRoundThreeRunnerIdentity(t *testing.T) {
	selection := mustPrerequisiteSelection(t, "claude-code")
	seen := make([]string, 0)
	environment := mcpExecutableEnvironment{
		lookPath: func(command string) (string, error) {
			seen = append(seen, command)
			return "/lookup/" + command, nil
		},
		lookupEnv: func(string) (string, bool) {
			return "", true
		},
	}

	servers := []desiredmcp.Server{
		prerequisiteMCPServer(t, "npx-package", "npx", []string{"--package=server@1.0.0", "server-bin"}),
		prerequisiteMCPServer(t, "uvx-from", "uvx", []string{"--from=mcp-server==0.4.0", "mcp-server"}),
		prerequisiteMCPServer(t, "docker-options", "docker", []string{"run", "--name", "daemon", "ghcr.io/acme/server:1.0.0"}),
		prerequisiteMCPServer(t, "plain", "python3", []string{"server.py"}),
	}
	checks := mcpExecutableRequirementChecks(servers, selection, environment)
	wantSeen := []string{"npx", "uvx", "docker", "python3"}
	if strings.Join(seen, ",") != strings.Join(wantSeen, ",") {
		t.Fatalf("lookPath commands = %#v, want %#v", seen, wantSeen)
	}
	for _, server := range servers {
		check := assertPrerequisiteCheck(t, checks, "target=claude-code scope=project mcp_server="+server.ID().Name()+" executable_requirement=command")
		if check.Status != findings.CheckOK {
			t.Fatalf("check = %#v, want discoverable command", check)
		}
	}

	setMCPExecutableLookupPath(t)
	unsetMCPRequirementEnv(t, "HOST_SECRET")
	server := prerequisiteMCPServerWith(t, "warning-only", "claude-code", "project", "node", []string{"server.js"}, map[string]string{"SERVER_SECRET": "HOST_SECRET"})
	checks = MCPExecutableRequirementChecks([]desiredmcp.Server{server}, selection)
	for _, check := range checks {
		if check.Status == findings.CheckError {
			t.Fatalf("check = %#v, want passive prerequisite warnings not errors", check)
		}
	}
}

func TestMCPExecutableRequirementEdgeHuntNoCommandExecutionCanary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell execution canary uses POSIX executable bits")
	}
	tempDir := t.TempDir()
	sentinel := filepath.Join(tempDir, "executed")
	commandPath := filepath.Join(tempDir, "must-not-run")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\n: > "+sentinel+"\n"), 0o755); err != nil {
		t.Fatalf("write command canary: %v", err)
	}
	t.Setenv("PATH", tempDir)
	checks := MCPExecutableRequirementChecks([]desiredmcp.Server{prerequisiteMCPServer(t, "canary", "must-not-run", nil)}, mustPrerequisiteSelection(t, "claude-code"))
	check := assertPrerequisiteCheck(t, checks, "target=claude-code scope=project mcp_server=canary executable_requirement=command")
	if check.Status != findings.CheckOK {
		t.Fatalf("check = %#v, want command lookup ok without execution", check)
	}
	if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sentinel stat err = %v, command canary appears to have executed", err)
	}
}

func TestMCPExecutableRequirementEdgeHuntRejectsAmbiguousOrForeignTopology(t *testing.T) {
	projection := requirementSubject(t, topology.SubjectProjection, "mcp", "one")
	otherProjection := requirementSubject(t, topology.SubjectProjection, "mcp", "two")
	launcher := requirementSubject(t, topology.SubjectRuntimeDependency, "executable", "node")
	foreignLauncher := requirementSubject(t, topology.SubjectRuntimeDependency, "other", "node")
	foreignCredential := requirementSubject(t, topology.SubjectCredentialReference, "other", "TOKEN")

	tests := []struct {
		name     string
		subjects []topology.SubjectID
		edges    []topology.Edge
		want     string
	}{
		{name: "missing projection", subjects: []topology.SubjectID{launcher}, want: "exactly one structural projection, got 0"},
		{
			name:     "ambiguous projections",
			subjects: []topology.SubjectID{projection, otherProjection, launcher},
			edges: []topology.Edge{
				topology.NewEdge(topology.EdgeLaunchesVia, projection, launcher),
				topology.NewEdge(topology.EdgeLaunchesVia, otherProjection, launcher),
			},
			want: "exactly one structural projection, got 2",
		},
		{
			name:     "foreign launcher namespace",
			subjects: []topology.SubjectID{projection, foreignLauncher},
			edges:    []topology.Edge{topology.NewEdge(topology.EdgeLaunchesVia, projection, foreignLauncher)},
			want:     "unsupported launcher dependency",
		},
		{
			name:     "foreign credential namespace",
			subjects: []topology.SubjectID{projection, launcher, foreignCredential},
			edges: []topology.Edge{
				topology.NewEdge(topology.EdgeLaunchesVia, projection, launcher),
				topology.NewEdge(topology.EdgeDependsOn, projection, foreignCredential),
			},
			want: "unsupported dependency",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph, err := topology.NewGraph(test.subjects, test.edges)
			if err != nil {
				t.Fatalf("NewGraph returned error: %v", err)
			}
			if _, err := mcpExecutableRequirementFactsForGraph(graph); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("facts error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func requirementSubject(t *testing.T, kind topology.SubjectKind, namespace string, key string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(kind, namespace, key)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	return subject
}

func setMCPExecutableLookupPath(t *testing.T, commands ...string) string {
	t.Helper()

	directory := t.TempDir()
	for _, command := range commands {
		name := command
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(directory, name), []byte("lookup-only fixture"), 0o755); err != nil {
			t.Fatalf("write lookup-only command %q: %v", command, err)
		}
	}
	t.Setenv("PATH", directory)
	return directory
}

func unsetMCPRequirementEnv(t *testing.T, name string) {
	t.Helper()

	value, present := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(name, value)
			return
		}
		_ = os.Unsetenv(name)
	})
}
