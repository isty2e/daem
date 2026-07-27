package profile

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestMCPRuntimeProbeCapabilitiesAreExactAndDefensive(t *testing.T) {
	capabilities := MCPRuntimeProbeCapabilities()
	if len(capabilities) != 2 {
		t.Fatalf("MCP runtime-probe capabilities = %#v, want two rows", capabilities)
	}
	tests := []struct {
		index            int
		placementID      aggregate.MCPPlacementID
		requiresDelegate bool
	}{
		{0, aggregate.MCPPlacementClaudeProject, true},
		{1, aggregate.MCPPlacementOpenCodeProject, false},
	}
	for _, test := range tests {
		capability := capabilities[test.index]
		if capability.Placement().ID() != test.placementID ||
			capability.RequiresDelegatePlan() != test.requiresDelegate {
			t.Fatalf(
				"capability[%d] = %q delegate=%v, want %q delegate=%v",
				test.index,
				capability.Placement().ID(),
				capability.RequiresDelegatePlan(),
				test.placementID,
				test.requiresDelegate,
			)
		}
	}

	capabilities[0] = MCPRuntimeProbeCapability{}
	if got := MCPRuntimeProbeCapabilities()[0].Placement().ID(); got != aggregate.MCPPlacementClaudeProject {
		t.Fatalf("capability catalog leaked caller mutation: first placement = %q", got)
	}
}

func TestTargetProfileSelectsOnlyItsMCPRuntimeProbeCapabilities(t *testing.T) {
	claude := Profile(target.TargetClaudeCode)
	capability, ok := claude.MCPRuntimeProbeCapability(aggregate.MCPPlacementClaudeProject)
	if !ok || !capability.RequiresDelegatePlan() {
		t.Fatalf("Claude project capability = %#v, present=%v", capability, ok)
	}
	if _, ok := claude.MCPRuntimeProbeCapability(aggregate.MCPPlacementOpenCodeProject); ok {
		t.Fatal("Claude profile admitted the OpenCode runtime-probe capability")
	}
	if _, ok := Profile(target.TargetCodex).MCPRuntimeProbeCapability(
		aggregate.MCPPlacementCodexProject,
	); ok {
		t.Fatal("Codex profile admitted an unsupported runtime-probe capability")
	}
}

func TestMCPRuntimeProbeCapabilityCatalogRejectsInvalidAndDuplicateRows(t *testing.T) {
	valid := MCPRuntimeProbeCapabilities()
	invalid := MCPRuntimeProbeCapability{}
	if err := validateMCPRuntimeProbeCapabilityCatalog(
		[]MCPRuntimeProbeCapability{invalid},
	); err == nil || !strings.Contains(err.Error(), "placement") {
		t.Fatalf("invalid capability error = %v, want placement failure", err)
	}
	if err := validateMCPRuntimeProbeCapabilityCatalog(
		[]MCPRuntimeProbeCapability{valid[0], valid[0]},
	); err == nil || !strings.Contains(err.Error(), "share placement") {
		t.Fatalf("duplicate capability error = %v, want duplicate failure", err)
	}
}
