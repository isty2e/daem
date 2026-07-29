package aggregate

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
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

func TestValidateMCPPlacementCatalogAllowsCodecContractSharedAcrossPlacements(t *testing.T) {
	left := mustTestMCPPlacement(t, "left", target.TargetCodex, target.ScopeProject, "codex-left-config")
	right := mustTestMCPPlacement(t, "right", target.TargetOpenCode, target.ScopeProject, "opencode-right-config")
	right.aggregateSpec.root = outputtest.Parse(t, ".test/other-config")
	right.codecContractID = left.codecContractID

	err := validateMCPPlacementCatalog([]MCPPlacement{left, right})
	if err != nil {
		t.Fatalf("shared codec contract returned error: %v", err)
	}
}

func TestValidateMCPPlacementCatalogRejectsConflictingConfigPathCollisions(t *testing.T) {
	t.Run("duplicate conflict path", func(t *testing.T) {
		left := mustTestMCPPlacement(t, "left", target.TargetCodex, target.ScopeProject, "codex-left-config")
		right := mustTestMCPPlacement(t, "right", target.TargetOpenCode, target.ScopeProject, "opencode-right-config")
		right.aggregateSpec.root = outputtest.Parse(t, ".test/right-config")
		left.conflictingConfigPath = outputtest.Parse(t, ".test/conflict")
		left.hasConflictingPath = true
		right.conflictingConfigPath = outputtest.Parse(t, ".test/conflict")
		right.hasConflictingPath = true

		err := validateMCPPlacementCatalog([]MCPPlacement{left, right})
		if err == nil || !strings.Contains(err.Error(), "share conflicting config path") {
			t.Fatalf("error = %v, want duplicate conflicting config path", err)
		}
	})

	t.Run("conflict path owned by another row", func(t *testing.T) {
		left := mustTestMCPPlacement(t, "left", target.TargetCodex, target.ScopeProject, "codex-left-config")
		right := mustTestMCPPlacement(t, "right", target.TargetOpenCode, target.ScopeProject, "opencode-right-config")
		right.aggregateSpec.root = outputtest.Parse(t, ".test/right-config")
		left.conflictingConfigPath = right.aggregateSpec.root
		left.hasConflictingPath = true

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
		ID:                     id,
		Target:                 selectedTarget,
		Scope:                  selectedScope,
		ConfigLayer:            configLayer,
		ConfigPath:             ".test/config",
		MergeUnit:              MCPMergeUnitServerEntry,
		ContentPathPrefix:      "/mcp",
		SiblingRetention:       MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        CodecContractID(string(id) + "-codec-v1"),
		ComparedFields:         []string{"command", "target"},
		Absence:                MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    MCPEnvMappingUnsupported,
		EnvReferenceResolution: MCPEnvResolutionUnavailable,
	})
	if err != nil {
		t.Fatalf("NewMCPPlacement returned error: %v", err)
	}
	return placement
}
