package aggregate

import (
	"strings"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/target"
)

func TestMCPPlacementForBindingReturnsCatalogPlacement(t *testing.T) {
	transport := desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, "npx"), []string{"server"}, nil)
	binding := desiredtest.MCPBinding(t, target.TargetCodex, target.ScopeProject, transport, desiredmcp.OnAbsentKeep)

	placement, err := MCPPlacementForBinding(binding)
	if err != nil {
		t.Fatalf("MCPPlacementForBinding returned error: %v", err)
	}
	if placement.Target() != target.TargetCodex || placement.Scope() != target.ScopeProject {
		t.Fatalf("placement = %s/%s, want codex/project", placement.Target(), placement.Scope())
	}
}

func TestMCPPlacementForBindingOwnsCapabilityAdmission(t *testing.T) {
	env := map[string]desiredmcp.EnvReference{
		"TOKEN": desiredtest.MCPEnvReference(t, "HOST_TOKEN"),
	}
	tests := []struct {
		name          string
		binding       desiredmcp.Binding
		wantPlacement MCPPlacementID
		wantError     string
	}{
		{
			name: "unsupported scope",
			binding: desiredtest.MCPBinding(
				t,
				target.TargetAntigravityCLI,
				target.ScopeProject,
				desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, "npx"), nil, nil),
				desiredmcp.OnAbsentKeep,
			),
			wantError: "unsupported MCP scope",
		},
		{
			name: "global aliased env",
			binding: desiredtest.MCPBinding(
				t,
				target.TargetClaudeCode,
				target.ScopeGlobal,
				desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, "npx"), nil, env),
				desiredmcp.OnAbsentKeep,
			),
			wantPlacement: MCPPlacementClaudeGlobal,
		},
		{
			name: "aliased env",
			binding: desiredtest.MCPBinding(
				t,
				target.TargetClaudeCode,
				target.ScopeProject,
				desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, "npx"), nil, env),
				desiredmcp.OnAbsentKeep,
			),
			wantPlacement: MCPPlacementClaudeProject,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			placement, err := MCPPlacementForBinding(test.binding)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("MCPPlacementForBinding returned error: %v", err)
				}
				if placement.ID() != test.wantPlacement {
					t.Fatalf("placement = %q, want %q", placement.ID(), test.wantPlacement)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("MCPPlacementForBinding error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}
