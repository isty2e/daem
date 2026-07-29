package mcp_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestServerLowersClaudeProjectStdioToStructuralTopology(t *testing.T) {
	server := ambientServer(
		t,
		"context7",
		target.TargetClaudeCode,
		target.ScopeProject,
		"npx",
		[]string{"-y", "@upstash/context7-mcp"},
		map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	)
	graph := mustGraph(t, server)
	projection := projectionSubject(t, server, onlyBinding(t, server))
	executable, _ := topologymcp.ExecutableSubject("npx")
	credential, _ := topologymcp.EnvironmentReferenceSubject("CONTEXT7_API_TOKEN")

	assertSubjectStrings(t, graph.Subjects(), []string{
		credential.String(),
		projection.String(),
		executable.String(),
	})
	if command, ok := topologymcp.ExecutableReference(executable); !ok ||
		command.Resolution() != desiredmcp.CommandResolutionAmbient ||
		command.Executable() != "npx" {
		t.Fatalf("ExecutableReference(%s) = (%#v, %t)", executable, command, ok)
	}
	if got := graph.DependenciesOf(projection); !reflect.DeepEqual(got, []topology.SubjectID{credential}) {
		t.Fatalf("DependenciesOf() = %v, want credential", got)
	}
	if got := graph.LauncherDependenciesOf(projection); !reflect.DeepEqual(got, []topology.SubjectID{executable}) {
		t.Fatalf("LauncherDependenciesOf() = %v, want executable", got)
	}
}

func TestProjectionSubjectOwnsCanonicalIdentityForEveryPlacement(t *testing.T) {
	tests := []struct {
		name      string
		selected  target.Target
		scope     target.Scope
		namespace string
	}{
		{name: "claude project", selected: target.TargetClaudeCode, scope: target.ScopeProject, namespace: "claude-code.project.mcp-server"},
		{name: "claude global", selected: target.TargetClaudeCode, scope: target.ScopeGlobal, namespace: "claude-code.global.mcp-server"},
		{name: "antigravity global", selected: target.TargetAntigravityCLI, scope: target.ScopeGlobal, namespace: "antigravity-cli.global.mcp-server"},
		{name: "opencode project", selected: target.TargetOpenCode, scope: target.ScopeProject, namespace: "opencode.project.mcp-server"},
		{name: "opencode global", selected: target.TargetOpenCode, scope: target.ScopeGlobal, namespace: "opencode.global.mcp-server"},
		{name: "codex project", selected: target.TargetCodex, scope: target.ScopeProject, namespace: "codex.project.mcp-server"},
		{name: "codex global", selected: target.TargetCodex, scope: target.ScopeGlobal, namespace: "codex.global.mcp-server"},
		{name: "pi project", selected: target.TargetPi, scope: target.ScopeProject, namespace: "pi.project.mcp-server"},
		{name: "pi global", selected: target.TargetPi, scope: target.ScopeGlobal, namespace: "pi.global.mcp-server"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := ambientServer(t, "context7", test.selected, test.scope, "npx", []string{"package-argv"}, nil)
			binding := onlyBinding(t, server)
			graph := mustGraph(t, server)
			wantProjection := projectionSubject(t, server, binding)
			if wantProjection.Namespace() != test.namespace {
				t.Fatalf("projection namespace = %q, want %q", wantProjection.Namespace(), test.namespace)
			}
			if !topologymcp.IsProjectionFor(test.selected, test.scope, wantProjection) {
				t.Fatalf("IsProjectionFor(%q, %q, %s) = false", test.selected, test.scope, wantProjection)
			}
			if !graph.Contains(wantProjection) {
				t.Fatalf("graph does not contain topology projection %s", wantProjection)
			}
			for _, subject := range graph.Subjects() {
				if subject.Kind() == topology.SubjectBinding {
					t.Fatalf("MCP projection minted parallel binding identity %s", subject)
				}
				if strings.Contains(subject.String(), "package-argv") {
					t.Fatalf("topology leaked projection argv in %s", subject)
				}
			}
		})
	}
}

func TestServerLoweringIsDeterministicForUnorderedAndSharedFacts(t *testing.T) {
	left := ambientServer(
		t,
		"server",
		target.TargetClaudeCode,
		target.ScopeProject,
		"node",
		nil,
		map[string]string{"Z_TOKEN": "HOST_Z", "A_TOKEN": "HOST_A"},
	)
	right := ambientServer(
		t,
		"server",
		target.TargetClaudeCode,
		target.ScopeProject,
		"node",
		nil,
		map[string]string{"A_TOKEN": "HOST_A", "Z_TOKEN": "HOST_Z"},
	)

	leftGraph := mustGraph(t, left)
	rightGraph := mustGraph(t, right)
	if !reflect.DeepEqual(subjectStrings(leftGraph.Subjects()), subjectStrings(rightGraph.Subjects())) {
		t.Fatalf("subject order differs:\nleft: %v\nright: %v", subjectStrings(leftGraph.Subjects()), subjectStrings(rightGraph.Subjects()))
	}
	projection := projectionSubject(t, left, onlyBinding(t, left))
	leftDependencies := leftGraph.DependenciesOf(projection)
	rightDependencies := rightGraph.DependenciesOf(projection)
	if !reflect.DeepEqual(leftDependencies, rightDependencies) {
		t.Fatalf("dependency order differs:\nleft: %v\nright: %v", leftDependencies, rightDependencies)
	}
	leftLaunchers := leftGraph.LauncherDependenciesOf(projection)
	rightLaunchers := rightGraph.LauncherDependenciesOf(projection)
	if !reflect.DeepEqual(leftLaunchers, rightLaunchers) {
		t.Fatalf("launcher order differs:\nleft: %v\nright: %v", leftLaunchers, rightLaunchers)
	}
	dependencies := leftDependencies
	if len(dependencies) != 2 {
		t.Fatalf("dependency count = %d, want two environment references: %v", len(dependencies), dependencies)
	}
}

func TestServersShareDependencyIdentityWithoutFlatteningProjections(t *testing.T) {
	first := ambientServer(t, "alpha", target.TargetClaudeCode, target.ScopeProject, "npx", nil, nil)
	second := ambientServer(t, "beta", target.TargetClaudeCode, target.ScopeProject, "npx", nil, nil)
	graph, err := topologymcp.Servers([]desiredmcp.Server{second, first})
	if err != nil {
		t.Fatalf("Servers returned error: %v", err)
	}

	executable, _ := topologymcp.ExecutableSubject("npx")
	if len(graph.Subjects()) != 3 || !graph.Contains(executable) {
		t.Fatalf("shared topology subjects = %v, want two projections and one executable", graph.Subjects())
	}
	if len(graph.LauncherDependenciesOf(projectionSubject(t, first, onlyBinding(t, first)))) != 1 ||
		len(graph.LauncherDependenciesOf(projectionSubject(t, second, onlyBinding(t, second)))) != 1 {
		t.Fatalf("shared executable did not retain both projection relations: %v", graph.Subjects())
	}
}

func TestServerRejectsUnsupportedPlacements(t *testing.T) {
	tests := []struct {
		name     string
		server   desiredmcp.Server
		wantText string
	}{
		{
			name:     "unsupported scope",
			server:   ambientServer(t, "server", target.TargetAntigravityCLI, target.ScopeProject, "npx", nil, nil),
			wantText: "unsupported MCP scope",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := topologymcp.Servers([]desiredmcp.Server{test.server})
			if err == nil || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("Server error = %v, want containing %q", err, test.wantText)
			}
		})
	}
}

func TestServerRepresentsEnvironmentReferenceIndependentlyOfRealizationCapabilities(t *testing.T) {
	server := ambientServer(
		t,
		"server",
		target.TargetCodex,
		target.ScopeProject,
		"npx",
		nil,
		map[string]string{"TOKEN": "HOST_TOKEN"},
	)
	graph := mustGraph(t, server)
	projection := projectionSubject(t, server, onlyBinding(t, server))
	want := mustEnvironmentReferenceSubject(t, "HOST_TOKEN")
	if dependencies := graph.DependenciesOf(projection); !reflect.DeepEqual(dependencies, []topology.SubjectID{want}) {
		t.Fatalf("DependenciesOf(%s) = %v, want [%s]", projection, dependencies, want)
	}
}

func TestProjectionSubjectRejectsUnsupportedRowsAndInvalidNames(t *testing.T) {
	context7 := mustMCPServerID(t, "context7")
	if _, err := topologymcp.ProjectionSubject(target.Target("future"), target.ScopeProject, context7.Name()); err == nil || !strings.Contains(err.Error(), "unknown target") {
		t.Fatalf("unsupported target error = %v", err)
	}
	if _, err := topologymcp.ProjectionSubject(target.TargetAntigravityCLI, target.ScopeProject, context7.Name()); err == nil || !strings.Contains(err.Error(), "unsupported MCP scope") {
		t.Fatalf("unsupported scope error = %v", err)
	}
	for _, serverID := range []string{"server/with%delimiter", "server-α-서버"} {
		subject, err := topologymcp.ProjectionSubject(target.TargetCodex, target.ScopeProject, serverID)
		gotServerID, ok := topologymcp.ServerID(subject)
		if err != nil || !ok || gotServerID != serverID || subject.Key() != serverID {
			t.Fatalf("ProjectionSubject(%q) = %s, %v", serverID, subject, err)
		}
		roundTripped, err := topology.ParseSubjectID(subject.String())
		if err != nil || roundTripped != subject {
			t.Fatalf("ParseSubjectID(%q) = %s, %v; want %s", subject.String(), roundTripped, err, subject)
		}
	}
	if subject, err := topologymcp.ProjectionSubject(target.TargetCodex, target.ScopeProject, ""); err == nil {
		t.Fatalf("ProjectionSubject(zero) = %s, want error", subject)
	}

	projection, err := topologymcp.ProjectionSubject(target.TargetCodex, target.ScopeProject, context7.Name())
	if err != nil {
		t.Fatalf("ProjectionSubject returned error: %v", err)
	}
	foreignKind, err := topology.NewSubjectID(topology.SubjectHostRelation, projection.Namespace(), projection.Key())
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	for _, subject := range []topology.SubjectID{{}, foreignKind} {
		if topologymcp.IsProjectionFor(target.TargetCodex, target.ScopeProject, subject) {
			t.Fatalf("IsProjectionFor accepted foreign subject %s", subject)
		}
	}
	if topologymcp.IsProjectionFor(target.TargetCodex, target.ScopeGlobal, projection) {
		t.Fatalf("project projection %s classified as global", projection)
	}
}

func TestBindingRejectsForeignBinding(t *testing.T) {
	owned := binding(t, target.TargetClaudeCode, target.ScopeProject, "npx", []string{"server@1"})
	foreign := binding(t, target.TargetClaudeCode, target.ScopeProject, "npx", []string{"server@2"})
	server := desiredtest.MCPServer(t, desiredmcp.Spec{Name: "server", Bindings: []desiredmcp.Binding{owned}})
	if _, err := topologymcp.Binding(server, foreign); err == nil || !strings.Contains(err.Error(), "binding is not owned") {
		t.Fatalf("foreign Binding error = %v", err)
	}
}

func TestServersRejectDuplicateProjectionSubject(t *testing.T) {
	server := ambientServer(t, "server", target.TargetClaudeCode, target.ScopeProject, "npx", nil, nil)
	_, err := topologymcp.Servers([]desiredmcp.Server{server, server})
	if err == nil || !strings.Contains(err.Error(), "duplicate MCP projection subject") {
		t.Fatalf("Servers error = %v, want duplicate projection", err)
	}
}

func ambientServer(
	t *testing.T,
	name string,
	selected target.Target,
	scope target.Scope,
	command string,
	args []string,
	env map[string]string,
) desiredmcp.Server {
	t.Helper()
	references := make(map[string]desiredmcp.EnvReference, len(env))
	for slot, fromEnv := range env {
		references[slot] = desiredtest.MCPEnvReference(t, fromEnv)
	}
	transport := desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, command), args, references)
	binding := desiredtest.MCPBinding(t, selected, scope, transport, desiredmcp.OnAbsentRemoveBinding)
	return desiredtest.MCPServer(t, desiredmcp.Spec{Name: name, Bindings: []desiredmcp.Binding{binding}})
}

func binding(t *testing.T, selected target.Target, scope target.Scope, command string, args []string) desiredmcp.Binding {
	t.Helper()
	transport := desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, command), args, nil)
	return desiredtest.MCPBinding(t, selected, scope, transport, desiredmcp.OnAbsentRemoveBinding)
}

func onlyBinding(t *testing.T, server desiredmcp.Server) desiredmcp.Binding {
	t.Helper()
	bindings := server.Bindings()
	if len(bindings) != 1 {
		t.Fatalf("server bindings = %d, want 1", len(bindings))
	}
	return bindings[0]
}

func projectionSubject(t *testing.T, server desiredmcp.Server, binding desiredmcp.Binding) topology.SubjectID {
	t.Helper()
	subject, err := topologymcp.ProjectionSubject(binding.Target(), binding.Scope(), server.ID().Name())
	if err != nil {
		t.Fatalf("ProjectionSubject returned error: %v", err)
	}
	return subject
}

func mustMCPServerID(t *testing.T, name string) entity.ID {
	t.Helper()
	id, err := entity.New(entity.KindMCPServer, name)
	if err != nil {
		t.Fatalf("MCP server entity %q: %v", name, err)
	}
	return id
}

func mustEnvironmentReferenceSubject(t *testing.T, name string) topology.SubjectID {
	t.Helper()
	subject, err := topologymcp.EnvironmentReferenceSubject(name)
	if err != nil {
		t.Fatalf("EnvironmentReferenceSubject returned error: %v", err)
	}
	return subject
}

func mustGraph(t *testing.T, server desiredmcp.Server) topology.Graph {
	t.Helper()
	graph, err := topologymcp.Servers([]desiredmcp.Server{server})
	if err != nil {
		t.Fatalf("Servers returned error: %v", err)
	}
	return graph
}

func assertSubjectStrings(t *testing.T, subjects []topology.SubjectID, want []string) {
	t.Helper()
	got := subjectStrings(subjects)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects = %v, want %v", got, want)
	}
}

func subjectStrings(subjects []topology.SubjectID) []string {
	values := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		values = append(values, subject.String())
	}
	return values
}
