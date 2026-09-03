package catalog

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
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

func TestRuntimeProbeFacetMatchesOwnerCapabilities(t *testing.T) {
	t.Parallel()

	catalog := Product()
	owner := make(map[aggregate.MCPPlacementID]profile.MCPRuntimeProbeCapability)
	for _, capability := range profile.MCPRuntimeProbeCapabilities() {
		owner[capability.Placement().ID()] = capability
	}

	compiled := 0
	for _, view := range catalog.Surfaces() {
		probe, ok := view.RuntimeProbe()
		if !ok {
			if _, present := owner[view.Placement().ID()]; present {
				t.Fatalf("compiled view %s/%s missing owner runtime probe", view.Key().Target(), view.Key().Scope())
			}
			continue
		}
		want, present := owner[view.Placement().ID()]
		if !present {
			t.Fatalf("compiled runtime probe for %s/%s has no owner row", view.Key().Target(), view.Key().Scope())
		}
		if probe.Placement().ID() != want.Placement().ID() ||
			probe.RequiresDelegatePlan() != want.RequiresDelegatePlan() {
			t.Fatalf("runtime probe for %q diverged from owner catalog", view.Placement().ID())
		}
		compiled++
	}
	if compiled != len(owner) {
		t.Fatalf("compiled runtime probes = %d want %d", compiled, len(owner))
	}

	if _, ok := catalog.LookupMCP(target.TargetCodex, target.ScopeProject); !ok {
		t.Fatal("Codex project must remain a compiled MCP cell")
	}
	codex, _ := catalog.LookupMCP(target.TargetCodex, target.ScopeProject)
	if _, ok := codex.RuntimeProbe(); ok {
		t.Fatal("Codex project must not compile a runtime-probe purpose")
	}
	if _, ok := catalog.LookupMCP(target.TargetAntigravityCLI, target.ScopeProject); ok {
		t.Fatal("Antigravity project must remain an unsupported MCP cell")
	}
}

func TestHasMCPProviderAuthoringMatchesOwnerCatalog(t *testing.T) {
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
		_, want := profile.MCPProviderAuthoringProfileForTarget(selected)
		if catalog.HasMCPProviderAuthoring(selected) != want {
			t.Fatalf("HasMCPProviderAuthoring(%s) = %v want owner catalog", selected, catalog.HasMCPProviderAuthoring(selected))
		}
	}
}

func TestLookupMCPBySubjectMatchesOwnerPlacement(t *testing.T) {
	t.Parallel()

	catalog := Product()
	id, err := entity.New(entity.KindMCPServer, "context7")
	if err != nil {
		t.Fatal(err)
	}
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		subject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), id.Name())
		if err != nil {
			t.Fatalf("ProjectionSubject(%q, %q) returned error: %v", placement.Target(), placement.Scope(), err)
		}
		owner, ownerOK := aggregate.MCPPlacementForSubject(subject)
		view, ok := catalog.LookupMCPBySubject(subject)
		if ok != ownerOK {
			t.Fatalf("LookupMCPBySubject(%s) ok = %v want owner %v", subject, ok, ownerOK)
		}
		if !ok {
			continue
		}
		if view.Placement().ID() != owner.ID() {
			t.Fatalf("LookupMCPBySubject(%s) placement = %q want %q", subject, view.Placement().ID(), owner.ID())
		}
	}
}

func TestLookupMCPBySubjectRejectsForeignSubjects(t *testing.T) {
	t.Parallel()

	catalog := Product()
	subjects := []topology.SubjectID{
		mustLookupSubject(t, topology.SubjectHostRelation, "claude-code.project.mcp-server", "context7"),
		mustLookupSubject(t, topology.SubjectProjection, "unknown.mcp-server", "context7"),
		mustLookupSubject(t, topology.SubjectProjection, "antigravity-cli.project.mcp-server", "context7"),
	}
	for _, subject := range subjects {
		if _, ok := catalog.LookupMCPBySubject(subject); ok {
			t.Fatalf("LookupMCPBySubject(%s) = true", subject)
		}
		if _, ok := aggregate.MCPPlacementForSubject(subject); ok {
			t.Fatalf("owner MCPPlacementForSubject(%s) = true", subject)
		}
	}
}

func TestMCPInOwnerOrderMatchesOwnerPlacementCatalog(t *testing.T) {
	t.Parallel()

	catalog := Product()
	owner := aggregate.ImplementedMCPPlacements()
	ordered := catalog.MCPInOwnerOrder()
	if len(ordered) != len(owner) {
		t.Fatalf("MCPInOwnerOrder = %d want %d", len(ordered), len(owner))
	}
	for index, view := range ordered {
		if view.Placement().ID() != owner[index].ID() {
			t.Fatalf("owner-order[%d] = %q want %q", index, view.Placement().ID(), owner[index].ID())
		}
	}
	identity := catalog.Surfaces()
	if ordered[0].Placement().ID() != aggregate.MCPPlacementClaudeProject {
		t.Fatalf("owner-order[0] = %q want Claude project", ordered[0].Placement().ID())
	}
	if identity[0].Placement().ID() != aggregate.MCPPlacementAntigravityGlobal {
		t.Fatalf("SurfaceID-order[0] = %q want Antigravity global", identity[0].Placement().ID())
	}
}

func mustLookupSubject(t *testing.T, kind topology.SubjectKind, namespace string, key string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(kind, namespace, key)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	return subject
}
