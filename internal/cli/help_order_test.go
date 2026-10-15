package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
	workflowhelp "github.com/isty2e/daem/internal/workflow/help"
)

func TestMCPHelpTargetAndPlacementOrder(t *testing.T) {
	facts := workflowhelp.BuildUsageFacts()
	context := usageContext(80)
	for _, test := range []struct {
		name       string
		placements []workflowhelp.MCPPlacementFact
		targets    string
		pairs      string
		scopes     string
	}{
		{
			name: "authoring", placements: facts.MCPAuthoringPlacements,
			targets: context.MCPAuthoringTargets, pairs: context.MCPAuthoringPlacements, scopes: context.MCPAuthoringScopes,
		},
		{
			name: "runtime probe", placements: facts.MCPRuntimeProbePlacements,
			targets: context.MCPRuntimeProbeTargets, pairs: context.MCPRuntimeProbePlacements, scopes: context.MCPRuntimeProbeScopes,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var targets []string
			var pairs []string
			for _, supported := range target.SupportedTargets() {
				selected := false
				for _, placement := range test.placements {
					if placement.Target != supported {
						continue
					}
					selected = true
					pairs = append(pairs, string(placement.Target)+"/"+string(placement.Scope))
				}
				if selected {
					targets = append(targets, string(supported))
				}
			}
			if want := strings.Join(targets, ", "); test.targets != want {
				t.Errorf("target values = %q, want common order %q", test.targets, want)
			}
			if want := strings.Join(pairs, ", "); test.pairs != want {
				t.Errorf("placement values = %q, want target-ordered source pairs %q", test.pairs, want)
			}
			if len(pairs) != len(test.placements) {
				t.Fatalf("expected display lost facts: got %d, source has %d", len(pairs), len(test.placements))
			}
			if want := mcpPlacementScopesForHelp(test.placements); test.scopes != want {
				t.Errorf("scope values = %q, want unchanged source scope order %q", test.scopes, want)
			}
		})
	}
}

func TestMCPHelpOrderingPreservesFactsAndScopeCorrelations(t *testing.T) {
	for _, test := range []struct {
		name       string
		placements []workflowhelp.MCPPlacementFact
	}{
		{name: "empty"},
		{name: "one", placements: []workflowhelp.MCPPlacementFact{{Target: target.TargetPi, Scope: target.ScopeGlobal}}},
		{
			name: "mixed targets with nondefault scope order",
			placements: []workflowhelp.MCPPlacementFact{
				{Target: target.TargetPi, Scope: target.ScopeProject},
				{Target: target.TargetClaudeCode, Scope: target.ScopeGlobal},
				{Target: target.TargetCodex, Scope: target.ScopeGlobal},
				{Target: target.TargetPi, Scope: target.ScopeGlobal},
				{Target: target.TargetCodex, Scope: target.ScopeProject},
				{Target: target.TargetOpenCode, Scope: target.ScopeProject},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := slices.Clone(test.placements)
			ordered := mcpPlacementsInHelpOrder(test.placements)
			if !slices.Equal(test.placements, before) {
				t.Fatalf("display sorting mutated source facts: before=%v after=%v", before, test.placements)
			}
			if len(ordered) != len(before) {
				t.Fatalf("display has %d placements, source has %d", len(ordered), len(before))
			}

			targets := target.SupportedTargets()
			for index := 1; index < len(ordered); index++ {
				if slices.Index(targets, ordered[index-1].Target) > slices.Index(targets, ordered[index].Target) {
					t.Fatalf("display does not follow common target order: %v", ordered)
				}
			}
			for _, selected := range targets {
				var originalScopes []target.Scope
				var displayedScopes []target.Scope
				for _, placement := range before {
					if placement.Target == selected {
						originalScopes = append(originalScopes, placement.Scope)
					}
				}
				for _, placement := range ordered {
					if placement.Target == selected {
						displayedScopes = append(displayedScopes, placement.Scope)
					}
				}
				if !slices.Equal(originalScopes, displayedScopes) {
					t.Fatalf("%s scope correlation/order changed: source=%v display=%v", selected, originalScopes, displayedScopes)
				}
			}

			if len(ordered) != 0 {
				ordered[0].Target = target.TargetAntigravityCLI
				if !slices.Equal(test.placements, before) {
					t.Fatal("display result aliases source facts")
				}
			}
		})
	}
}
