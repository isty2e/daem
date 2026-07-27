package help

import (
	"path/filepath"
	"reflect"
	"testing"

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
