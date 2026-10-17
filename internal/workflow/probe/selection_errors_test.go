package probe

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestSelectedMCPBindingRetainsSelectionFactsAndPrecedence(t *testing.T) {
	transport := testfixture.MCPStdio(t, testfixture.MCPCommand(t, "unused-probe-command"), nil, nil)
	binding := func(selected target.Target, scope target.Scope) desiredmcp.Binding {
		return testfixture.MCPBinding(t, selected, scope, transport, desiredmcp.OnAbsentKeep)
	}
	claude := binding(target.TargetClaudeCode, target.ScopeProject)
	claudeGlobal := binding(target.TargetClaudeCode, target.ScopeGlobal)
	openCode := binding(target.TargetOpenCode, target.ScopeProject)
	codex := binding(target.TargetCodex, target.ScopeProject)
	codexGlobal := binding(target.TargetCodex, target.ScopeGlobal)

	for _, test := range []struct {
		name     string
		query    string
		bindings []desiredmcp.Binding
		target   target.Target
		scope    target.Scope
		outcome  string
		matches  int
		selected desiredmcp.Binding
	}{
		{name: "missing query", query: "instructions/mcp_server:%25", bindings: []desiredmcp.Binding{claude}, outcome: "no row"},
		{name: "target mismatch", bindings: []desiredmcp.Binding{claude}, target: target.TargetOpenCode, outcome: "no row"},
		{name: "scope mismatch", bindings: []desiredmcp.Binding{claude}, scope: target.ScopeGlobal, outcome: "no row"},
		{name: "single rejected row", bindings: []desiredmcp.Binding{codex}, outcome: "admission"},
		{name: "multiple rejected rows", bindings: []desiredmcp.Binding{codex, codexGlobal}, outcome: "no row"},
		{name: "filter rejections first", bindings: []desiredmcp.Binding{codex, codexGlobal}, scope: target.ScopeGlobal, outcome: "admission"},
		{name: "target ambiguity", bindings: []desiredmcp.Binding{claude, openCode}, outcome: "ambiguity", matches: 2},
		{name: "unadmitted scope does not count", bindings: []desiredmcp.Binding{claude, claudeGlobal}, outcome: "selected", selected: claude},
		{name: "rejections do not count", bindings: []desiredmcp.Binding{claude, openCode, codex}, outcome: "ambiguity", matches: 2},
		{name: "infer unique row", bindings: []desiredmcp.Binding{claude}, outcome: "selected", selected: claude},
		{name: "admitted row wins", bindings: []desiredmcp.Binding{claude, codex}, outcome: "selected", selected: claude},
		{name: "target narrows", bindings: []desiredmcp.Binding{claude, openCode}, target: target.TargetOpenCode, outcome: "selected", selected: openCode},
		{name: "scope selects rejected row", bindings: []desiredmcp.Binding{claude, claudeGlobal}, scope: target.ScopeGlobal, outcome: "admission"},
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%t", test.name, reverse), func(t *testing.T) {
				bindings := slices.Clone(test.bindings)
				if reverse {
					slices.Reverse(bindings)
				}
				server := testfixture.MCPServer(t, desiredmcp.Spec{Name: "context7", Bindings: bindings})
				query := test.query
				if query == "" {
					query = "context7"
				}
				selectedServer, selectedBinding, err := selectedMCPBinding([]desiredmcp.Server{server}, query, test.target, test.scope)
				switch test.outcome {
				case "no row":
					var missing NoAdmittedBindingError
					if !errors.As(err, &missing) || missing.Name != query || missing.Target != test.target || missing.Scope != test.scope {
						t.Fatalf("selection error = %#v, want original query and selectors", err)
					}
				case "admission":
					var admission BindingAdmissionError
					if !errors.As(err, &admission) || admission.Name != query || admission.Err == nil {
						t.Fatalf("selection error = %#v, want named admission cause", err)
					}
				case "ambiguity":
					var ambiguous AmbiguousBindingError
					if !errors.As(err, &ambiguous) || ambiguous.Name != query || ambiguous.Matches != test.matches {
						t.Fatalf("selection error = %#v, want admitted match count %d", err, test.matches)
					}
				case "selected":
					if err != nil || selectedServer.ID() != server.ID() || !selectedServer.OwnsBinding(selectedBinding) ||
						selectedBinding.Target() != test.selected.Target() || selectedBinding.Scope() != test.selected.Scope() {
						t.Fatalf("selection = %v, %v/%v, %v; want exact admitted row", selectedServer.ID(), selectedBinding.Target(), selectedBinding.Scope(), err)
					}
				}
			})
		}
	}
}

func TestBindingAdmissionErrorRetainsCauseIdentity(t *testing.T) {
	sentinel := errors.New("admission sentinel")
	cause := &fs.PathError{Op: "admit", Path: "fixture", Err: sentinel}
	wrapped := fmt.Errorf("selection: %w", BindingAdmissionError{Name: "context7", Err: cause})
	var pathFailure *fs.PathError
	if !errors.Is(wrapped, sentinel) || !errors.Is(wrapped, cause) || !errors.As(wrapped, &pathFailure) || pathFailure != cause {
		t.Fatalf("admission lost cause identity: %v", wrapped)
	}
}

func TestSelectedMCPBindingRetainsTypedAdmissionCause(t *testing.T) {
	transport := testfixture.MCPStdio(t, testfixture.MCPCommand(t, "unused-probe-command"), nil, map[string]desiredmcp.EnvReference{
		"CHILD_TOKEN": testfixture.MCPEnvReference(t, "HOST_TOKEN"),
	})
	binding := testfixture.MCPBinding(t, target.TargetOpenCode, target.ScopeProject, transport, desiredmcp.OnAbsentKeep)
	server := testfixture.MCPServer(t, desiredmcp.Spec{Name: "context7", Bindings: []desiredmcp.Binding{binding}})
	_, _, err := selectedMCPBinding([]desiredmcp.Server{server}, "context7", "", "")
	var admission BindingAdmissionError
	var cause aggregate.MCPEnvReferenceAdmissionError
	if !errors.As(err, &admission) || !errors.As(err, &cause) || !errors.Is(err, admission.Err) ||
		cause.PlacementID() != aggregate.MCPPlacementOpenCodeProject || cause.Mapping() != aggregate.MCPEnvMappingUnsupported {
		t.Fatalf("selection error = %#v, want original typed environment admission cause", err)
	}
}
