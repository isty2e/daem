package profile

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestHookAssetPlacementRequiresCorrelatedRoute(t *testing.T) {
	placement, err := HookAssetPlacementFor(target.ScopeProject, []target.Target{target.TargetCodex})
	if err != nil {
		t.Fatal(err)
	}
	route, ok := Profile(target.TargetCodex).OperationRoute(entity.KindHookAsset, placement.ID(), OperationWrite)
	if !ok {
		t.Fatal("hook asset write route missing")
	}
	hash := artifact.ContentHash("sha256:" + strings.Repeat("a", 64))
	spec, err := placement.Realize("review", hash, true, route)
	if err != nil {
		t.Fatal(err)
	}
	projection, ok := spec.ManagedPathProjection()
	if !ok || projection.PermissionPolicy() != realization.PathPermissionsExact {
		t.Fatalf("projection = %#v, %t", projection, ok)
	}
	wrong := mustOperationRoute(entity.KindHookAsset, OperationWrite, hookAssetGlobalPlacementID, hookAssetWriteRoute, hookAssetAdapterContract)
	if _, err := placement.Realize("review", hash, true, wrong); err == nil {
		t.Fatal("wrongly correlated hook asset route was accepted")
	}
}

func TestManagedPathRouteResolutionRejectsIncompatibleConsumersAndOperations(t *testing.T) {
	placement := mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "skill.project.agents", ConsumerTargets: []target.Target{target.TargetClaudeCode, target.TargetCodex},
		ResourceKind: entity.KindSkill, Scope: target.ScopeProject, Root: ".agents/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	})
	if _, err := ManagedPathOperationRoute(placement, OperationWrite); err == nil || !strings.Contains(err.Error(), "has no unique write route") {
		t.Fatalf("incompatible-consumer route error = %v", err)
	}

	placement = Profile(target.TargetCodex).Placements(entity.KindSkill, target.ScopeProject)[0]
	removeRoute, ok := Profile(target.TargetCodex).OperationRoute(entity.KindSkill, placement.ID(), OperationRemove)
	if !ok {
		t.Fatal("remove route is missing")
	}
	destination, err := placement.ChildDestination("review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := placement.Realize(destination, realization.PathProjectionCopy, removeRoute); err == nil || !strings.Contains(err.Error(), "does not correlate") {
		t.Fatalf("wrong-operation realization error = %v", err)
	}
}

func TestFacetConstructorsRejectImpossibleAxisStates(t *testing.T) {
	if _, err := NewSupported(target.TargetCodex, entity.KindSkill); err != nil {
		t.Fatal(err)
	}
	if err := (Support{selectedTarget: target.TargetCodex, resourceKind: entity.KindSkill, supported: true, reason: UnsupportedReasonNotImplemented}).Validate(); err == nil {
		t.Fatal("supported-with-reason state validated")
	}
	if _, err := NewUnsupported(target.TargetCodex, entity.KindSkill, "future"); err == nil {
		t.Fatal("open unsupported reason validated")
	}
	if _, err := NewDiscoveryLocation(target.TargetCodex, entity.KindSkill, target.ScopeProject, "/tmp/skills", 0, ImportPolicyInclude); err == nil {
		t.Fatal("absolute project discovery path validated")
	}
	if _, err := NewOperationRoute(entity.KindSkill, "probe", "skill.project.agents", "route", "v1"); err == nil {
		t.Fatal("open operation kind validated")
	}
}

func TestFacetCatalogRejectsDuplicateDefaultsAndMissingRoutes(t *testing.T) {
	first := mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "test.skill.primary", ConsumerTargets: []target.Target{target.TargetCodex},
		ResourceKind: entity.KindSkill, Scope: target.ScopeProject, Root: ".agents/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	})
	second := mustManagedPathPlacement(ManagedPathPlacementInput{
		ID: "test.skill.alternate", ConsumerTargets: []target.Target{target.TargetCodex},
		ResourceKind: entity.KindSkill, Scope: target.ScopeProject, Root: ".codex/skills",
		DefaultPlacement: true, ContentKind: realization.PathProjectionDirectory,
	})
	if err := validateProfileFacetCatalogs([]ManagedPathPlacement{first, second}, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "default placements") {
		t.Fatalf("duplicate-default validation error = %v", err)
	}

	discovery := mustDiscoveryLocation(target.TargetCodex, entity.KindSkill, target.ScopeProject, first.Root(), 0)
	write := mustOperationRoute(entity.KindSkill, OperationWrite, first.ID(), "test.write", "test-v1")
	if err := validateProfileFacetCatalogs([]ManagedPathPlacement{first}, []DiscoveryLocation{discovery}, nil, []OperationRoute{write}); err == nil || !strings.Contains(err.Error(), "no remove route") {
		t.Fatalf("missing-route validation error = %v", err)
	}
}

func TestFacetCatalogRejectsDuplicateOperationRoutes(t *testing.T) {
	route := mustOperationRoute(entity.KindSkill, OperationWrite, "test.skill", "test.write", "test-v1")
	if err := validateProfileFacetCatalogs(nil, nil, nil, []OperationRoute{route, route}); err == nil || !strings.Contains(err.Error(), "duplicate write route") {
		t.Fatalf("duplicate-route validation error = %v", err)
	}
}

func TestAggregateOperationRoutesAreSelectedIndependentlyFromPlacement(t *testing.T) {
	tests := []struct {
		name        string
		target      target.Target
		kind        entity.Kind
		correlation string
		writeRoute  string
		removeRoute string
		adapter     string
	}{
		{
			name: "Hook", target: target.TargetCodex, kind: entity.KindHook,
			correlation: string(aggregate.HookPlacementCodexProject),
			writeRoute:  "codex-project-hooks.write_projection", removeRoute: "codex-project-hooks.remove_projection",
			adapter: string(aggregate.HookCodecCodexProject),
		},
		{
			name: "MCP", target: target.TargetOpenCode, kind: entity.KindMCPServer,
			correlation: string(aggregate.MCPPlacementOpenCodeProject),
			writeRoute:  "opencode-project-mcp-local-command.write_projection", removeRoute: "opencode-project-mcp-local-command.remove_binding",
			adapter: string(aggregate.MCPCodecOpenCodeProjectLocal),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targetProfile := Profile(tc.target)
			write, ok := targetProfile.OperationRoute(tc.kind, tc.correlation, OperationWrite)
			if !ok || write.RouteID() != tc.writeRoute || write.AdapterContractVersion() != tc.adapter {
				t.Fatalf("write route = %#v, present=%v", write, ok)
			}
			remove, ok := targetProfile.OperationRoute(tc.kind, tc.correlation, OperationRemove)
			if !ok || remove.RouteID() != tc.removeRoute || remove.AdapterContractVersion() != tc.adapter {
				t.Fatalf("remove route = %#v, present=%v", remove, ok)
			}
		})
	}
	if _, ok := Profile(target.TargetClaudeCode).OperationRoute(
		entity.KindMCPServer,
		string(aggregate.MCPPlacementOpenCodeProject),
		OperationWrite,
	); ok {
		t.Fatal("Claude profile exposed OpenCode aggregate route")
	}
}

func TestProfilePreservesMCPPlacementScopeMatrix(t *testing.T) {
	antigravity := Profile(target.TargetAntigravityCLI)
	if _, ok := antigravity.MCPPlacement(aggregate.MCPPlacementAntigravityGlobal); !ok {
		t.Fatal("Antigravity global MCP placement is missing")
	}
	if _, ok := antigravity.MCPPlacement(aggregate.MCPPlacementCodexProject); ok {
		t.Fatal("Antigravity profile admitted a foreign project MCP placement")
	}
	codex := Profile(target.TargetCodex)
	for _, id := range []aggregate.MCPPlacementID{aggregate.MCPPlacementCodexProject, aggregate.MCPPlacementCodexGlobal} {
		if _, ok := codex.MCPPlacement(id); !ok {
			t.Fatalf("Codex MCP placement %q is missing", id)
		}
	}
}
