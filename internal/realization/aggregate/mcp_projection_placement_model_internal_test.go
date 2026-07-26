package aggregate

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestValidateMCPPlacementCatalogRejectsDuplicateTargetScope(t *testing.T) {
	left := mustTestMCPPlacement(t, "left", target.TargetCodex, target.ScopeProject, "codex-left-config")
	right := mustTestMCPPlacement(t, "right", target.TargetCodex, target.ScopeProject, "codex-right-config")

	err := validateMCPPlacementCatalog([]MCPPlacement{left, right})
	if err == nil || !strings.Contains(err.Error(), "share target/scope") {
		t.Fatalf("error = %v, want duplicate target/scope", err)
	}
}

func TestValidateMCPPlacementCatalogRejectsDuplicatePlacementID(t *testing.T) {
	left := mustTestMCPPlacement(t, "shared", target.TargetCodex, target.ScopeProject, "codex-left-config")
	right := mustTestMCPPlacement(t, "shared", target.TargetOpenCode, target.ScopeProject, "opencode-right-config")

	err := validateMCPPlacementCatalog([]MCPPlacement{left, right})
	if err == nil || !strings.Contains(err.Error(), "share placement id") {
		t.Fatalf("error = %v, want duplicate placement id", err)
	}
}

func TestValidateMCPPlacementCatalogRejectsDuplicateConfigPath(t *testing.T) {
	left := mustTestMCPPlacement(t, "left", target.TargetCodex, target.ScopeProject, "codex-left-config")
	right := mustTestMCPPlacement(t, "right", target.TargetOpenCode, target.ScopeProject, "opencode-right-config")

	err := validateMCPPlacementCatalog([]MCPPlacement{left, right})
	if err == nil || !strings.Contains(err.Error(), "share config path") {
		t.Fatalf("error = %v, want duplicate config path", err)
	}
}

func TestValidateMCPPlacementCatalogRejectsDuplicateCodecContract(t *testing.T) {
	left := mustTestMCPPlacement(t, "left", target.TargetCodex, target.ScopeProject, "codex-left-config")
	right := mustTestMCPPlacement(t, "right", target.TargetOpenCode, target.ScopeProject, "opencode-right-config")
	right.aggregateSpec.root = ".test/other-config"
	right.codecContractID = left.codecContractID

	err := validateMCPPlacementCatalog([]MCPPlacement{left, right})
	if err == nil || !strings.Contains(err.Error(), "share codec contract") {
		t.Fatalf("error = %v, want duplicate codec contract", err)
	}
}

func TestValidateMCPPlacementCatalogRejectsConflictingConfigPathCollisions(t *testing.T) {
	t.Run("duplicate conflict path", func(t *testing.T) {
		left := mustTestMCPPlacement(t, "left", target.TargetCodex, target.ScopeProject, "codex-left-config")
		right := mustTestMCPPlacement(t, "right", target.TargetOpenCode, target.ScopeGlobal, "opencode-right-config")
		right.aggregateSpec.root = ".test/right-config"
		left.conflictingConfigPath = ".test/conflict"
		right.conflictingConfigPath = ".test/conflict"

		err := validateMCPPlacementCatalog([]MCPPlacement{left, right})
		if err == nil || !strings.Contains(err.Error(), "share conflicting config path") {
			t.Fatalf("error = %v, want duplicate conflicting config path", err)
		}
	})

	t.Run("conflict path owned by another row", func(t *testing.T) {
		left := mustTestMCPPlacement(t, "left", target.TargetCodex, target.ScopeProject, "codex-left-config")
		right := mustTestMCPPlacement(t, "right", target.TargetOpenCode, target.ScopeGlobal, "opencode-right-config")
		right.aggregateSpec.root = ".test/right-config"
		left.conflictingConfigPath = right.aggregateSpec.root

		err := validateMCPPlacementCatalog([]MCPPlacement{left, right})
		if err == nil || !strings.Contains(err.Error(), "is owned by placement") {
			t.Fatalf("error = %v, want conflicting path ownership collision", err)
		}
	})
}

func mustTestMCPPlacement(
	t *testing.T,
	id MCPPlacementID,
	selectedTarget target.Target,
	selectedScope target.Scope,
	configLayer MCPConfigLayer,
) MCPPlacement {
	t.Helper()
	placement, err := NewMCPPlacement(MCPPlacementInput{
		ID:                id,
		Target:            selectedTarget,
		Scope:             selectedScope,
		ConfigLayer:       configLayer,
		ConfigPath:        ".test/config",
		MergeUnit:         MCPMergeUnitServerEntry,
		ContentPathPrefix: "/mcp",
		SiblingRetention:  MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:   CodecContractID(string(id) + "-codec-v1"),
		ComparedFields:    []string{"command", "target"},
		Absence:           MCPAbsenceRemoveBinding,
	})
	if err != nil {
		t.Fatalf("NewMCPPlacement returned error: %v", err)
	}
	return placement
}
