package catalog

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestLookupMCPMatchesOwnerPlacement(t *testing.T) {
	t.Parallel()

	catalog := Product()
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		view, ok := catalog.LookupMCP(placement.Target(), placement.Scope())
		if !ok {
			t.Fatalf("LookupMCP missing for %s/%s", placement.Target(), placement.Scope())
		}
		if view.Placement().ID() != placement.ID() {
			t.Fatalf("placement ID = %q want %q", view.Placement().ID(), placement.ID())
		}
		if !catalog.HasMCPTarget(placement.Target()) {
			t.Fatalf("HasMCPTarget missing %s", placement.Target())
		}
	}
}

func TestLookupMCPTreatsUnsupportedAndInvalidAsMissing(t *testing.T) {
	t.Parallel()

	catalog := Product()
	if _, ok := catalog.LookupMCP(target.TargetAntigravityCLI, target.ScopeProject); ok {
		t.Fatal("Antigravity project must remain an unsupported MCP cell")
	}
	if catalog.HasMCPTarget(target.Target("not-a-target")) {
		t.Fatal("invalid target must not compile as an MCP host")
	}
	if _, ok := catalog.LookupMCP(target.TargetClaudeCode, target.Scope("not-a-scope")); ok {
		t.Fatal("invalid scope must remain missing")
	}
}

func TestHasMCPTargetMatchesOwnerCatalog(t *testing.T) {
	t.Parallel()

	catalog := Product()
	targets := []target.Target{
		target.TargetClaudeCode,
		target.TargetCodex,
		target.TargetOpenCode,
		target.TargetPi,
		target.TargetAntigravityCLI,
		target.Target("unknown-host"),
	}
	for _, selected := range targets {
		if catalog.HasMCPTarget(selected) != aggregate.TargetHasImplementedMCPPlacement(selected) {
			t.Fatalf("HasMCPTarget(%s) = %v want owner catalog", selected, catalog.HasMCPTarget(selected))
		}
	}
}
