package catalog

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestProductCatalogMatchesOwnerMCPRows(t *testing.T) {
	t.Parallel()

	catalog := Product()
	placements := aggregate.ImplementedMCPPlacements()
	if len(catalog.Surfaces()) != len(placements) || len(placements) != 9 {
		t.Fatalf("compiled surfaces = %d placements = %d", len(catalog.Surfaces()), len(placements))
	}

	piCodecs := make(map[aggregate.CodecContractID]int)
	probeCount := 0
	for _, placement := range placements {
		key, err := hostsurface.MCPSurfaceKey(placement.Target(), placement.Scope())
		if err != nil {
			t.Fatal(err)
		}
		view, ok := catalog.Lookup(key)
		if !ok {
			t.Fatalf("missing compiled surface for %s/%s", placement.Target(), placement.Scope())
		}
		if !view.ID().Key().Equal(key) {
			t.Fatalf("ID/key mismatch for %s", view.ID())
		}
		if view.ID().String() == string(placement.ID()) {
			t.Fatalf("surface ID %q equals placement ID", view.ID())
		}
		if view.Placement().ID() != placement.ID() {
			t.Fatalf("placement ID = %q want %q", view.Placement().ID(), placement.ID())
		}
		wantNamespace, err := topologymcp.Namespace(placement.Target(), placement.Scope())
		if err != nil {
			t.Fatal(err)
		}
		if view.Namespace() != wantNamespace {
			t.Fatalf("namespace = %q want %q", view.Namespace(), wantNamespace)
		}
		write, remove, ok := profile.MCPAggregateRouteIDs(placement.ID())
		if !ok || view.WriteRouteID() != write || view.RemoveRouteID() != remove {
			t.Fatalf("routes = %q/%q want %q/%q", view.WriteRouteID(), view.RemoveRouteID(), write, remove)
		}
		if view.Placement().CodecContractID() != placement.CodecContractID() {
			t.Fatalf("codec = %q want %q", view.Placement().CodecContractID(), placement.CodecContractID())
		}
		if view.Placement().ConfigPath().String() != placement.ConfigPath().String() {
			t.Fatalf("config path = %q", view.Placement().ConfigPath())
		}
		gotConflict, gotHasConflict := view.Placement().ConflictingConfigPath()
		wantConflict, wantHasConflict := placement.ConflictingConfigPath()
		if gotHasConflict != wantHasConflict || gotConflict.String() != wantConflict.String() {
			t.Fatalf("conflict path mismatch for %q", placement.ID())
		}
		if view.Placement().MergeUnit() != placement.MergeUnit() ||
			view.Placement().ContentPathPrefix() != placement.ContentPathPrefix() ||
			view.Placement().SiblingRetention() != placement.SiblingRetention() {
			t.Fatalf("aggregate contract mismatch for %q", placement.ID())
		}
		if !slices.Equal(view.Placement().ComparedFields(), placement.ComparedFields()) {
			t.Fatalf("compared fields mismatch for %q", placement.ID())
		}
		gotEnv := view.Placement().EnvReferenceContract()
		wantEnv := placement.EnvReferenceContract()
		if gotEnv.Mapping() != wantEnv.Mapping() || gotEnv.Resolution() != wantEnv.Resolution() {
			t.Fatalf("env contract mismatch for %q", placement.ID())
		}
		if !view.IsDefaultVariant() {
			t.Fatalf("MCP surface %q is not the default variant", view.ID())
		}
		_, wantProvider := profile.MCPProviderAuthoringProfileForTarget(placement.Target())
		if view.ProviderAuthoringAdmitted() != wantProvider {
			t.Fatalf("provider authoring = %v want %v for %s", view.ProviderAuthoringAdmitted(), wantProvider, placement.Target())
		}
		if placement.Target() == target.TargetPi {
			piCodecs[placement.CodecContractID()]++
		}
		if _, hasProbe := view.RuntimeProbe(); hasProbe {
			probeCount++
			purposes := view.ObservationPurposes()
			if len(purposes) != 1 || purposes[0] != ObservationRuntimeProbe {
				t.Fatalf("probe purposes = %v", purposes)
			}
		} else if len(view.ObservationPurposes()) != 0 {
			t.Fatalf("unexpected purposes %v", view.ObservationPurposes())
		}
	}

	if len(piCodecs) != 1 || piCodecs[aggregate.MCPCodecPiAdapterStdio] != 2 {
		t.Fatalf("Pi codec sharing = %v", piCodecs)
	}
	if probeCount != 2 {
		t.Fatalf("runtime probes = %d want 2", probeCount)
	}

	claudeProject, ok := catalog.Lookup(mustMCPKey(t, target.TargetClaudeCode, target.ScopeProject))
	if !ok {
		t.Fatal("missing Claude project surface")
	}
	probe, ok := claudeProject.RuntimeProbe()
	if !ok || !probe.RequiresDelegatePlan() {
		t.Fatal("Claude project must require a delegate-plan probe")
	}
	openCodeProject, ok := catalog.Lookup(mustMCPKey(t, target.TargetOpenCode, target.ScopeProject))
	if !ok {
		t.Fatal("missing OpenCode project surface")
	}
	probe, ok = openCodeProject.RuntimeProbe()
	if !ok || probe.RequiresDelegatePlan() {
		t.Fatal("OpenCode project probe must not require a delegate plan")
	}

	unsupported, err := hostsurface.MCPSurfaceKey(target.TargetAntigravityCLI, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.Lookup(unsupported); ok {
		t.Fatal("Antigravity project must remain an unsupported MCP cell")
	}
}

func TestCompileRejectsDuplicateSurfaceKey(t *testing.T) {
	t.Parallel()

	seed := productSeed()
	key, err := hostsurface.MCPSurfaceKey(target.TargetClaudeCode, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	seed.Bindings = append(derivedBindings(t, seed), SurfaceBinding{
		Key:         key,
		PlacementID: aggregate.MCPPlacementClaudeProject,
	})
	if _, err := Compile(seed); err == nil || !strings.Contains(err.Error(), "duplicate surface key") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompileRejectsMissingNamespace(t *testing.T) {
	t.Parallel()

	seed := productSeed()
	trimmed := seed.Namespaces[:0]
	for _, row := range seed.Namespaces {
		if row.Target() == target.TargetClaudeCode && row.Scope() == target.ScopeProject {
			continue
		}
		trimmed = append(trimmed, row)
	}
	seed.Namespaces = trimmed
	if _, err := Compile(seed); err == nil || !strings.Contains(err.Error(), "missing topology namespace") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompileAllowsManyToOnePlacement(t *testing.T) {
	t.Parallel()

	placement, ok := aggregate.MCPPlacementForID(aggregate.MCPPlacementClaudeProject)
	if !ok {
		t.Fatal("missing Claude project placement")
	}
	shared, err := hostsurface.ParseVariantID("shared")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hostsurface.NewSurfaceKey(
		target.TargetClaudeCode,
		target.ScopeProject,
		entity.KindMCPServer,
		shared,
	)
	if err != nil {
		t.Fatal(err)
	}
	write, remove, ok := profile.MCPAggregateRouteIDs(placement.ID())
	if !ok {
		t.Fatal("missing routes")
	}
	catalog, err := Compile(Seed{
		Bindings: []SurfaceBinding{
			{Key: mustMCPKey(t, target.TargetClaudeCode, target.ScopeProject), PlacementID: placement.ID()},
			{Key: second, PlacementID: placement.ID()},
		},
		Placements: []aggregate.MCPPlacement{placement},
		Namespaces: []topologymcp.ProjectionNamespace{mustNamespace(t, target.TargetClaudeCode, target.ScopeProject)},
		Routes: map[aggregate.MCPPlacementID]RoutePair{
			placement.ID(): {Write: write, Remove: remove},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Surfaces()) != 2 {
		t.Fatalf("surfaces = %d", len(catalog.Surfaces()))
	}
	first, _ := catalog.Lookup(mustMCPKey(t, target.TargetClaudeCode, target.ScopeProject))
	secondView, ok := catalog.Lookup(second)
	if !ok {
		t.Fatal("missing shared variant")
	}
	if first.Placement().ID() != secondView.Placement().ID() {
		t.Fatal("many-to-one placement was not preserved")
	}
	if first.ID().Equal(secondView.ID()) {
		t.Fatal("distinct keys must receive distinct surface IDs")
	}
}

func TestCompileRejectsProbeForUnknownPlacement(t *testing.T) {
	t.Parallel()

	placement, ok := aggregate.MCPPlacementForID(aggregate.MCPPlacementAntigravityGlobal)
	if !ok {
		t.Fatal("missing Antigravity placement")
	}
	write, remove, ok := profile.MCPAggregateRouteIDs(placement.ID())
	if !ok {
		t.Fatal("missing routes")
	}
	_, err := Compile(Seed{
		Placements: []aggregate.MCPPlacement{placement},
		Namespaces: []topologymcp.ProjectionNamespace{
			mustNamespace(t, target.TargetAntigravityCLI, target.ScopeGlobal),
		},
		Routes: map[aggregate.MCPPlacementID]RoutePair{
			placement.ID(): {Write: write, Remove: remove},
		},
		Probes: profile.MCPRuntimeProbeCapabilities(),
	})
	if err == nil || !strings.Contains(err.Error(), "unreferenced placement") {
		t.Fatalf("err = %v", err)
	}
}

func derivedBindings(t *testing.T, seed Seed) []SurfaceBinding {
	t.Helper()
	bindings, err := deriveBindings(nil, seed.Placements)
	if err != nil {
		t.Fatal(err)
	}
	return append([]SurfaceBinding(nil), bindings...)
}

func mustMCPKey(t *testing.T, selected target.Target, scope target.Scope) hostsurface.SurfaceKey {
	t.Helper()
	key, err := hostsurface.MCPSurfaceKey(selected, scope)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustNamespace(t *testing.T, selected target.Target, scope target.Scope) topologymcp.ProjectionNamespace {
	t.Helper()
	for _, row := range topologymcp.ImplementedProjectionNamespaces() {
		if row.Target() == selected && row.Scope() == scope {
			return row
		}
	}
	t.Fatalf("missing namespace for %s/%s", selected, scope)
	return topologymcp.ProjectionNamespace{}
}
