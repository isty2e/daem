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
				if err != nil || placement.ID() == "" {
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

func TestProfileImportabilityRequiresIncludeDiscovery(t *testing.T) {
	projectClassify := mustTestDiscoveryLocation(
		t,
		target.TargetCodex,
		target.ScopeProject,
		"classified",
		ImportPolicyClassify,
	)
	globalClassify := mustTestDiscoveryLocation(
		t,
		target.TargetCodex,
		target.ScopeGlobal,
		"~/classified",
		ImportPolicyClassify,
	)
	projectInclude := mustTestDiscoveryLocation(
		t,
		target.TargetCodex,
		target.ScopeProject,
		"included",
		ImportPolicyInclude,
	)
	globalInclude := mustTestDiscoveryLocation(
		t,
		target.TargetCodex,
		target.ScopeGlobal,
		"~/included",
		ImportPolicyInclude,
	)

	for _, test := range []struct {
		name        string
		discoveries []DiscoveryLocation
		want        bool
	}{
		{name: "empty"},
		{name: "classify-only", discoveries: []DiscoveryLocation{projectClassify, globalClassify}},
		{name: "include-only", discoveries: []DiscoveryLocation{projectInclude}, want: true},
		{name: "mixed-classify-first", discoveries: []DiscoveryLocation{projectClassify, globalInclude}, want: true},
		{name: "mixed-include-first", discoveries: []DiscoveryLocation{projectInclude, globalClassify}, want: true},
		{name: "duplicate-include", discoveries: []DiscoveryLocation{projectInclude, projectInclude}, want: true},
		{name: "global-only", discoveries: []DiscoveryLocation{globalInclude}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := TargetProfile{discoveries: test.discoveries}
			if got := profile.HasImportableDiscovery(); got != test.want {
				t.Fatalf("HasImportableDiscovery() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestImportableTargetsFollowStableProfilePolicy(t *testing.T) {
	want := target.SupportedTargets()
	got := ImportableTargets()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ImportableTargets() = %#v, want %#v", got, want)
	}
	got[0] = target.TargetPi
	if reflect.DeepEqual(got, ImportableTargets()) {
		t.Fatal("ImportableTargets returned shared mutable storage")
	}
	if Profile(target.Target("future-agent")).HasImportableDiscovery() {
		t.Fatal("unknown target profile became importable")
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

func TestSkillPlacementAdmissionsExposeDefaultsAndAlternatesWithoutChangingDefaults(t *testing.T) {
	tests := []struct {
		selectedTarget target.Target
		scope          target.Scope
		defaultRoot    string
		alternateRoots []string
	}{
		{
			selectedTarget: target.TargetCodex,
			scope:          target.ScopeGlobal,
			defaultRoot:    "~/.agents/skills",
			alternateRoots: []string{"~/.codex/skills"},
		},
		{
			selectedTarget: target.TargetOpenCode,
			scope:          target.ScopeProject,
			defaultRoot:    ".opencode/skills",
			alternateRoots: []string{".agents/skills", ".claude/skills"},
		},
		{
			selectedTarget: target.TargetOpenCode,
			scope:          target.ScopeGlobal,
			defaultRoot:    "~/.config/opencode/skills",
			alternateRoots: []string{"~/.agents/skills", "~/.claude/skills"},
		},
		{
			selectedTarget: target.TargetPi,
			scope:          target.ScopeProject,
			defaultRoot:    ".pi/skills",
			alternateRoots: []string{".agents/skills"},
		},
		{
			selectedTarget: target.TargetPi,
			scope:          target.ScopeGlobal,
			defaultRoot:    "~/.pi/agent/skills",
			alternateRoots: []string{"~/.agents/skills"},
		},
	}
	for _, test := range tests {
		t.Run(string(test.selectedTarget)+"/"+string(test.scope), func(t *testing.T) {
			selectedProfile := Profile(test.selectedTarget)
			defaultPlacement, err := selectedProfile.DefaultPlacement(entity.KindSkill, test.scope)
			if err != nil || defaultPlacement.Root().String() != test.defaultRoot {
				t.Fatalf("default placement = %#v, %v", defaultPlacement, err)
			}
			defaultAdmission, ok := selectedProfile.PlacementAdmissionAt(
				entity.KindSkill,
				test.scope,
				test.defaultRoot,
			)
			if !ok || !defaultAdmission.Default() {
				t.Fatalf("default admission = %#v, %t", defaultAdmission, ok)
			}
			for _, root := range test.alternateRoots {
				placement, ok := selectedProfile.PlacementAt(entity.KindSkill, test.scope, root)
				if !ok || placement.Root().String() != root {
					t.Fatalf("alternate root %q = %#v, %t", root, placement, ok)
				}
				admission, ok := selectedProfile.PlacementAdmissionAt(entity.KindSkill, test.scope, root)
				if !ok || admission.Default() {
					t.Fatalf("alternate admission %q = %#v, %t", root, admission, ok)
				}
			}
		})
	}
}

func TestManagedPathPlacementSelectionsRespectPerTargetAdmissions(t *testing.T) {
	defaults, err := ManagedPathPlacementsFor(
		entity.KindSkill,
		target.ScopeProject,
		[]target.Target{target.TargetOpenCode},
	)
	if err != nil {
		t.Fatal(err)
	}
	explicitDefault, err := ManagedPathPlacementsForSelections(
		entity.KindSkill,
		target.ScopeProject,
		[]target.Target{target.TargetOpenCode},
		map[target.Target]string{target.TargetOpenCode: ".opencode/skills"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(defaults, explicitDefault) {
		t.Fatalf("explicit default changed selection: default=%#v explicit=%#v", defaults, explicitDefault)
	}

	shared, err := ManagedPathPlacementsForSelections(
		entity.KindSkill,
		target.ScopeProject,
		[]target.Target{target.TargetCodex, target.TargetOpenCode},
		map[target.Target]string{target.TargetOpenCode: ".agents/skills"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(shared) != 1 || shared[0].ID() != "skill.project.agents" {
		t.Fatalf("shared selection = %#v", shared)
	}
	wantConsumers := []target.Target{target.TargetCodex, target.TargetOpenCode}
	if !reflect.DeepEqual(shared[0].ConsumerTargets(), wantConsumers) {
		t.Fatalf("shared consumers = %#v, want %#v", shared[0].ConsumerTargets(), wantConsumers)
	}

	split, err := ManagedPathPlacementsForSelections(
		entity.KindSkill,
		target.ScopeProject,
		[]target.Target{target.TargetCodex, target.TargetOpenCode},
		map[target.Target]string{target.TargetOpenCode: ".claude/skills"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(split) != 2 || split[0].ID() != "skill.project.agents" || split[1].ID() != "skill.project.claude" {
		t.Fatalf("split selection = %#v", split)
	}
}

func TestManagedPathPlacementSelectionsRejectCrossTargetAuthority(t *testing.T) {
	_, err := ManagedPathPlacementsForSelections(
		entity.KindSkill,
		target.ScopeProject,
		[]target.Target{target.TargetCodex},
		map[target.Target]string{target.TargetCodex: ".claude/skills"},
	)
	if err == nil ||
		!strings.Contains(err.Error(), `target "codex"`) ||
		!strings.Contains(err.Error(), `placement ".claude/skills" is not admitted`) ||
		!strings.Contains(err.Error(), `admitted roots: .agents/skills`) {
		t.Fatalf("selection error = %v", err)
	}

	_, err = ManagedPathPlacementsForSelections(
		entity.KindSkill,
		target.ScopeProject,
		[]target.Target{target.TargetCodex},
		map[target.Target]string{target.TargetOpenCode: ".agents/skills"},
	)
	if err == nil || !strings.Contains(err.Error(), `target "opencode" is not a consumer`) {
		t.Fatalf("extraneous selection error = %v", err)
	}
}

func TestManagedPathPlacementForConsumersRevalidatesExactIdentity(t *testing.T) {
	selected, err := ManagedPathPlacementForConsumers(
		entity.KindSkill,
		target.ScopeProject,
		"skill.project.agents",
		[]target.Target{target.TargetPi, target.TargetCodex, target.TargetOpenCode},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantConsumers := []target.Target{target.TargetCodex, target.TargetOpenCode, target.TargetPi}
	if !reflect.DeepEqual(selected.ConsumerTargets(), wantConsumers) {
		t.Fatalf("consumers = %#v, want %#v", selected.ConsumerTargets(), wantConsumers)
	}

	if _, err := ManagedPathPlacementForConsumers(
		entity.KindSkill,
		target.ScopeProject,
		"skill.project.opencode",
		[]target.Target{target.TargetCodex},
	); err == nil || !strings.Contains(err.Error(), "is not selected by its consumers") {
		t.Fatalf("cross-target identity error = %v", err)
	}
}

func mustTestDiscoveryLocation(
	t *testing.T,
	selectedTarget target.Target,
	scope target.Scope,
	path string,
	policy ImportPolicy,
) DiscoveryLocation {
	t.Helper()
	location, err := NewDiscoveryLocation(selectedTarget, entity.KindSkill, scope, path, 0, policy)
	if err != nil {
		t.Fatalf("NewDiscoveryLocation returned error: %v", err)
	}
	return location
}

func TestCanonicalTargetCallersRejectInvalidConsumers(t *testing.T) {
	invalid := []target.Target{"future"}
	if _, err := ManagedPathPlacementsFor(entity.KindInstructions, target.ScopeProject, invalid); err == nil ||
		!strings.Contains(err.Error(), `target[0]: unknown target "future"`) {
		t.Fatalf("ManagedPathPlacementsFor error = %v", err)
	}
	if _, err := NewPlacementAdmission(invalid[0], "skill.project.agents", true); err == nil ||
		!strings.Contains(err.Error(), `unknown target "future"`) {
		t.Fatalf("NewPlacementAdmission error = %v", err)
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
	placements[0] = ManagedPathPlacement{}
	again := profile.Placements(entity.KindSkill, target.ScopeProject)
	if reflect.DeepEqual(placements, again) {
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
