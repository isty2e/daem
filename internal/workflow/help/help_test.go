package help

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildUsageFactsReturnsStaticTargetInventory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "relative-config-that-path-resolution-would-reject")
	facts := BuildUsageFacts()
	if len(facts.SupportedTargets) == 0 {
		t.Fatalf("SupportedTargets = %+v, want target facts", facts.SupportedTargets)
	}
	if want := profile.ImportableTargets(); !reflect.DeepEqual(facts.ImportTargets, want) {
		t.Fatalf("ImportTargets = %+v, want profile-derived %+v", facts.ImportTargets, want)
	}
	wantAuthoring := []MCPPlacementFact{
		{Target: target.TargetClaudeCode, Scope: target.ScopeProject},
		{Target: target.TargetClaudeCode, Scope: target.ScopeGlobal},
		{Target: target.TargetAntigravityCLI, Scope: target.ScopeGlobal},
		{Target: target.TargetOpenCode, Scope: target.ScopeProject},
		{Target: target.TargetOpenCode, Scope: target.ScopeGlobal},
		{Target: target.TargetCodex, Scope: target.ScopeProject},
		{Target: target.TargetCodex, Scope: target.ScopeGlobal},
		{Target: target.TargetPi, Scope: target.ScopeProject},
		{Target: target.TargetPi, Scope: target.ScopeGlobal},
	}
	if !reflect.DeepEqual(facts.MCPAuthoringPlacements, wantAuthoring) {
		t.Fatalf("MCPAuthoringPlacements = %+v, want %+v", facts.MCPAuthoringPlacements, wantAuthoring)
	}
	wantProbe := []MCPPlacementFact{
		{Target: target.TargetClaudeCode, Scope: target.ScopeProject},
		{Target: target.TargetOpenCode, Scope: target.ScopeProject},
	}
	if !reflect.DeepEqual(facts.MCPRuntimeProbePlacements, wantProbe) {
		t.Fatalf("MCPRuntimeProbePlacements = %+v, want %+v", facts.MCPRuntimeProbePlacements, wantProbe)
	}

	views := catalog.Product().MCPInOwnerOrder()
	if len(facts.MCPAuthoringPlacements) != len(views) {
		t.Fatalf("MCPAuthoringPlacements = %d, compiled owner-order = %d", len(facts.MCPAuthoringPlacements), len(views))
	}
	for index, fact := range facts.MCPAuthoringPlacements {
		if fact.Target != views[index].Key().Target() || fact.Scope != views[index].Key().Scope() {
			t.Fatalf("MCPAuthoringPlacements[%d] = %+v, want compiled %s/%s", index, fact, views[index].Key().Target(), views[index].Key().Scope())
		}
	}
}

func TestInitHintManifestPathResolvesExplicitPath(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "nested", "daem.toml")

	resolvedPath, err := InitHintManifestPath(manifestPath)
	if err != nil {
		t.Fatalf("InitHintManifestPath returned error: %v", err)
	}
	if resolvedPath != manifestPath {
		t.Fatalf("resolvedPath = %q, want %q", resolvedPath, manifestPath)
	}
}
