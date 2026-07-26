package mcp_test

import (
	"reflect"
	"strings"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestServersEdgeHuntSameDesiredIDAcrossPlacementsDoesNotCollide(t *testing.T) {
	project := ambientServer(t, "shared", target.TargetClaudeCode, target.ScopeProject, "npx", nil, nil)
	global := ambientServer(t, "shared", target.TargetCodex, target.ScopeGlobal, "npx", nil, nil)

	graph, err := topologymcp.Servers([]desiredmcp.Server{global, project})
	if err != nil {
		t.Fatalf("Servers returned error: %v", err)
	}
	projectSubject := projectionSubject(t, project, onlyBinding(t, project))
	globalSubject := projectionSubject(t, global, onlyBinding(t, global))
	if projectSubject == globalSubject || !graph.Contains(projectSubject) || !graph.Contains(globalSubject) {
		t.Fatalf("placement collision domains collapsed: project=%s global=%s subjects=%v", projectSubject, globalSubject, graph.Subjects())
	}
	if len(graph.Subjects()) != 3 {
		t.Fatalf("subjects = %v, want two projections plus one shared executable", graph.Subjects())
	}
}

func TestServersEdgeHuntOneEntityLowersDistinctBindingsAndSharesOnlyEqualDependencies(t *testing.T) {
	claudeProject := binding(t, target.TargetClaudeCode, target.ScopeProject, "npx", nil)
	claudeGlobal := binding(t, target.TargetClaudeCode, target.ScopeGlobal, "npx", nil)
	codexProject := binding(t, target.TargetCodex, target.ScopeProject, "uvx", nil)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{
		Name:     "shared",
		Bindings: []desiredmcp.Binding{codexProject, claudeGlobal, claudeProject},
	})

	graph, err := topologymcp.Servers([]desiredmcp.Server{server})
	if err != nil {
		t.Fatalf("Servers returned error: %v", err)
	}
	npx, _ := topologymcp.ExecutableSubject("npx")
	uvx, _ := topologymcp.ExecutableSubject("uvx")
	cases := []struct {
		binding  desiredmcp.Binding
		launcher topology.SubjectID
	}{
		{binding: claudeProject, launcher: npx},
		{binding: claudeGlobal, launcher: npx},
		{binding: codexProject, launcher: uvx},
	}
	seenProjections := make(map[topology.SubjectID]struct{}, len(cases))
	for _, test := range cases {
		projection := projectionSubject(t, server, test.binding)
		if _, duplicate := seenProjections[projection]; duplicate {
			t.Fatalf("distinct placement reused projection %s", projection)
		}
		seenProjections[projection] = struct{}{}
		if got := graph.LauncherDependenciesOf(projection); !reflect.DeepEqual(got, []topology.SubjectID{test.launcher}) {
			t.Fatalf("launcher dependencies for %s = %v, want [%s]", projection, got, test.launcher)
		}
	}
	if len(graph.Subjects()) != 5 {
		t.Fatalf("subjects = %v, want three projections and two shared-by-identity executables", graph.Subjects())
	}
	for _, subject := range graph.Subjects() {
		if subject.Kind() != topology.SubjectProjection && subject.Kind() != topology.SubjectRuntimeDependency {
			t.Fatalf("binding lowering fabricated non-structural subject %s", subject)
		}
	}
}

func TestServerEdgeHuntSharedEnvReferencesDeduplicate(t *testing.T) {
	server := ambientServer(
		t,
		"shared-env",
		target.TargetClaudeCode,
		target.ScopeProject,
		"npx",
		nil,
		map[string]string{"FIRST_TOKEN": "HOST_TOKEN", "SECOND_TOKEN": "HOST_TOKEN"},
	)
	binding := onlyBinding(t, server)
	graph := mustGraph(t, server)
	projection := projectionSubject(t, server, binding)
	credential, err := topologymcp.EnvironmentReferenceSubject("HOST_TOKEN")
	if err != nil {
		t.Fatalf("EnvironmentReferenceSubject returned error: %v", err)
	}
	if got := graph.DependenciesOf(projection); !reflect.DeepEqual(got, []topology.SubjectID{credential}) {
		t.Fatalf("dependencies = %v, want one deduplicated credential reference", got)
	}
	if len(graph.Subjects()) != 3 || len(graph.LauncherDependenciesOf(projection)) != 1 {
		t.Fatalf("graph subjects = %v, want projection, launcher, and one shared credential", graph.Subjects())
	}
}

func TestServerEdgeHuntDestinationEnvSlotsDoNotBecomeIdentity(t *testing.T) {
	left := ambientServer(
		t,
		"server",
		target.TargetClaudeCode,
		target.ScopeProject,
		"npx",
		nil,
		map[string]string{"DESTINATION_A": "SHARED_SOURCE"},
	)
	right := ambientServer(
		t,
		"server",
		target.TargetClaudeCode,
		target.ScopeProject,
		"npx",
		nil,
		map[string]string{"DESTINATION_B": "SHARED_SOURCE"},
	)

	leftGraph := mustGraph(t, left)
	rightGraph := mustGraph(t, right)
	leftProjection := projectionSubject(t, left, onlyBinding(t, left))
	rightProjection := projectionSubject(t, right, onlyBinding(t, right))
	if !reflect.DeepEqual(subjectStrings(leftGraph.Subjects()), subjectStrings(rightGraph.Subjects())) ||
		!reflect.DeepEqual(leftGraph.DependenciesOf(leftProjection), rightGraph.DependenciesOf(rightProjection)) ||
		!reflect.DeepEqual(leftGraph.LauncherDependenciesOf(leftProjection), rightGraph.LauncherDependenciesOf(rightProjection)) {
		t.Fatalf("destination env slot leaked into topology:\nleft=%v\nright=%v", leftGraph.Subjects(), rightGraph.Subjects())
	}
	for _, subject := range leftGraph.Subjects() {
		if strings.Contains(subject.String(), "DESTINATION_A") || strings.Contains(subject.String(), "DESTINATION_B") {
			t.Fatalf("destination env slot leaked into subject %s", subject)
		}
	}
}

func TestBindingEdgeHuntAbsencePolicyIsPartOfServerOwnership(t *testing.T) {
	transport := desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, "npx"), nil, nil)
	owned := desiredtest.MCPBinding(t, target.TargetClaudeCode, target.ScopeProject, transport, desiredmcp.OnAbsentKeep)
	foreign := desiredtest.MCPBinding(t, target.TargetClaudeCode, target.ScopeProject, transport, desiredmcp.OnAbsentRemoveBinding)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{Name: "server", Bindings: []desiredmcp.Binding{owned}})

	if _, err := topologymcp.Binding(server, foreign); err == nil || !strings.Contains(err.Error(), "binding is not owned") {
		t.Fatalf("Binding error = %v, want exact binding ownership rejection", err)
	}
}

func TestBindingEdgeHuntPlacementFailurePrecedesForeignOwnership(t *testing.T) {
	owned := binding(t, target.TargetClaudeCode, target.ScopeProject, "npx", nil)
	foreign := binding(t, target.TargetPi, target.ScopeProject, "npx", nil)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{Name: "server", Bindings: []desiredmcp.Binding{owned}})

	if _, err := topologymcp.Binding(server, foreign); err == nil || !strings.Contains(err.Error(), "unsupported MCP target") {
		t.Fatalf("Binding error = %v, want placement failure before ownership failure", err)
	}
}

func TestServersEdgeHuntEmptyInputProducesValidEmptyGraph(t *testing.T) {
	graph, err := topologymcp.Servers(nil)
	if err != nil {
		t.Fatalf("Servers(nil) returned error: %v", err)
	}
	if len(graph.Subjects()) != 0 {
		t.Fatalf("Servers(nil) = subjects %v", graph.Subjects())
	}
}

func TestDependencyIdentityEdgeHuntRejectsCrossNamespaceClassification(t *testing.T) {
	tests := []struct {
		name       string
		subject    topology.SubjectID
		classifier func(topology.SubjectID) (string, bool)
	}{
		{
			name:       "runtime dependency with foreign namespace",
			subject:    edgeHuntSubject(t, topology.SubjectRuntimeDependency, "env", "npx"),
			classifier: topologymcp.ExecutableCommand,
		},
		{
			name:       "credential with executable namespace",
			subject:    edgeHuntSubject(t, topology.SubjectCredentialReference, "executable", "TOKEN"),
			classifier: topologymcp.EnvironmentReferenceName,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if value, ok := test.classifier(test.subject); ok || value != "" {
				t.Fatalf("classifier(%s) = (%q, %t), want empty false", test.subject, value, ok)
			}
		})
	}
}

func edgeHuntSubject(t *testing.T, kind topology.SubjectKind, namespace string, key string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(kind, namespace, key)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	return subject
}
