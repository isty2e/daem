package profile

import (
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestProfilesPreserveCurrentSupportAndRealizationMatrix(t *testing.T) {
	directHooks := map[target.Target]bool{
		target.TargetCodex: true, target.TargetClaudeCode: true,
	}
	for _, selectedTarget := range target.SupportedTargets() {
		profile := Profile(selectedTarget)
		for _, resourceKind := range []entity.Kind{entity.KindInstructions, entity.KindSkill} {
			if !profile.Supports(resourceKind) {
				t.Fatalf("Profile(%q) does not support %q", selectedTarget, resourceKind)
			}
			if selected, ok := profile.RealizationKind(resourceKind); !ok || selected != realization.RealizationManagedPathProjection {
				t.Fatalf("Profile(%q).RealizationKind(%q) = %q, %t", selectedTarget, resourceKind, selected, ok)
			}
			for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
				placement, err := profile.DefaultPlacement(resourceKind, scope)
				if err != nil || !placement.Default() {
					t.Fatalf("Profile(%q).DefaultPlacement(%q, %q) = %#v, %v", selectedTarget, resourceKind, scope, placement, err)
				}
			}
		}

		hook, ok := profile.Support(entity.KindHook)
		if !ok || hook.Supported() != directHooks[selectedTarget] {
			t.Fatalf("Profile(%q) hook support = %#v, %t", selectedTarget, hook, ok)
		}
		_, hookRealization := profile.RealizationKind(entity.KindHook)
		_, assetRealization := profile.RealizationKind(entity.KindHookAsset)
		if hookRealization != directHooks[selectedTarget] || assetRealization != directHooks[selectedTarget] {
			t.Fatalf("Profile(%q) hook realizations = %t/%t", selectedTarget, hookRealization, assetRealization)
		}
	}
}

func TestProfileSeparatesPlacementDiscoveryRuntimeAndRoutes(t *testing.T) {
	claude := Profile(target.TargetClaudeCode)
	placements := claude.Placements(entity.KindInstructions, target.ScopeProject)
	if len(placements) != 1 || placements[0].Root().String() != "CLAUDE.md" {
		t.Fatalf("placements = %#v", placements)
	}
	discoveries := claude.DiscoveryLocations(entity.KindInstructions, target.ScopeProject)
	if got := discoveryPaths(discoveries); !reflect.DeepEqual(got, []string{"CLAUDE.md", ".claude/CLAUDE.md"}) {
		t.Fatalf("discovery paths = %#v", got)
	}
	runtime := claude.RuntimeLocations(entity.KindInstructions, target.ScopeProject)
	if len(runtime) != 1 || runtime[0].Path() != "CLAUDE.local.md" {
		t.Fatalf("runtime locations = %#v", runtime)
	}
	if _, admitted := claude.PlacementAt(entity.KindInstructions, target.ScopeProject, ".claude/CLAUDE.md"); admitted {
		t.Fatal("discovery-only path gained placement authority")
	}
	if _, admitted := claude.PlacementAt(entity.KindInstructions, target.ScopeProject, "CLAUDE.local.md"); admitted {
		t.Fatal("runtime-only path gained placement authority")
	}
	write, ok := claude.OperationRoute(entity.KindInstructions, placements[0].ID(), OperationWrite)
	if !ok || write.RouteID() != managedInstructionWriteRoute {
		t.Fatalf("write route = %#v, %t", write, ok)
	}
	if _, ok := claude.OperationRoute(entity.KindInstructions, ".claude/CLAUDE.md", OperationWrite); ok {
		t.Fatal("discovery path gained an operation route")
	}
}

func TestSharedPlacementsRemainOnePhysicalIdentity(t *testing.T) {
	placements, err := ManagedPathPlacementsFor(
		entity.KindInstructions,
		target.ScopeProject,
		[]target.Target{target.TargetPi, target.TargetCodex, target.TargetOpenCode},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(placements) != 1 || placements[0].ID() != "instructions.project.agents" {
		t.Fatalf("placements = %#v", placements)
	}
	wantTargets := []target.Target{target.TargetCodex, target.TargetOpenCode, target.TargetPi}
	if !reflect.DeepEqual(placements[0].ConsumerTargets(), wantTargets) {
		t.Fatalf("consumer targets = %#v, want %#v", placements[0].ConsumerTargets(), wantTargets)
	}
	route, ok := Profile(target.TargetCodex).OperationRoute(entity.KindInstructions, placements[0].ID(), OperationWrite)
	if !ok {
		t.Fatal("shared placement has no write route")
	}
	spec, err := placements[0].Realize(outputtest.Parse(t, "AGENTS.md"), realization.PathProjectionCopy, route)
	if err != nil || spec.Validate() != nil {
		t.Fatalf("Realize = %#v, %v", spec, err)
	}
}

func TestCanonicalTargetCallersRejectInvalidConsumers(t *testing.T) {
	invalid := []target.Target{"future"}
	if _, err := ManagedPathPlacementsFor(entity.KindInstructions, target.ScopeProject, invalid); err == nil ||
		!strings.Contains(err.Error(), `target[0]: unknown target "future"`) {
		t.Fatalf("ManagedPathPlacementsFor error = %v", err)
	}
	if _, err := NewManagedPathPlacement(ManagedPathPlacementInput{ConsumerTargets: invalid}); err == nil ||
		!strings.Contains(err.Error(), `target[0]: unknown target "future"`) {
		t.Fatalf("NewManagedPathPlacement error = %v", err)
	}
	if _, err := HookAssetPlacementFor(target.ScopeProject, invalid); err == nil ||
		!strings.Contains(err.Error(), `target[0]: unknown target "future"`) {
		t.Fatalf("HookAssetPlacementFor error = %v", err)
	}
}

func TestUnknownTargetProfileFailsClosed(t *testing.T) {
	unknown := Profile(target.Target("future-agent"))
	if unknown.Supports(entity.KindSkill) || len(unknown.ResourceSupports()) != 0 ||
		len(unknown.Placements(entity.KindSkill, target.ScopeProject)) != 0 ||
		len(unknown.DiscoveryLocations(entity.KindSkill, target.ScopeProject)) != 0 ||
		len(unknown.RuntimeLocations(entity.KindSkill, target.ScopeProject)) != 0 {
		t.Fatalf("unknown profile exposed admitted facts: %#v", unknown)
	}
	if _, err := unknown.DefaultPlacement(entity.KindSkill, target.ScopeProject); err == nil {
		t.Fatal("unknown profile selected a default placement")
	}
}

func TestProfileQueriesReturnDefensiveValues(t *testing.T) {
	profile := Profile(target.TargetOpenCode)
	placements := profile.Placements(entity.KindSkill, target.ScopeProject)
	targets := placements[0].ConsumerTargets()
	targets[0] = target.TargetCodex
	again := profile.Placements(entity.KindSkill, target.ScopeProject)
	if reflect.DeepEqual(targets, again[0].ConsumerTargets()) {
		t.Fatal("caller mutation changed profile placement")
	}
	route, ok := profile.DelegatedRoute("opencode-plugin")
	if !ok {
		t.Fatal("delegated route missing")
	}
	copy := cloneDelegatedRouteProfile(route)
	copy.allowedScopes[0] = "workspace"
	againRoute, _ := profile.DelegatedRoute("opencode-plugin")
	if reflect.DeepEqual(copy.allowedScopes, againRoute.allowedScopes) {
		t.Fatal("caller mutation changed delegated route")
	}
	routes := route.OperationRoutes()
	routes[0] = OperationRoute{}
	againRoute, _ = profile.DelegatedRoute("opencode-plugin")
	if reflect.DeepEqual(routes, againRoute.OperationRoutes()) {
		t.Fatal("caller mutation changed delegated operation routes")
	}
}
