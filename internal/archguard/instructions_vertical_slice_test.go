package archguard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstructionsVerticalSliceCannotRegainLegacyOperationalAuthority(t *testing.T) {
	root := findRepoRoot(t)
	legacyProjector := filepath.Join(root, "internal", "output", "project", "instructions")
	if _, err := os.Stat(legacyProjector); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired Instructions DesiredOutput projector exists or cannot be inspected: %v", err)
	}

	for _, packagePath := range []string{
		"internal/reconcile",
		"internal/effect/execute",
		"internal/effect/journal",
		"internal/assurance/observe/live",
	} {
		assertProductionPackageOmits(t, root, packagePath, []string{
			"ResourceKindInstructions",
			"KindInstructions",
		})
		assertTestPackageOmitsExcept(t, root, packagePath, nil, "ResourceKindInstructions")
	}
	assertProductionPackageOmits(t, root, "internal/workflow/apply", []string{
		"ResourceKindInstructions",
		"KindInstructions",
		".Instructions()",
	})

	for _, check := range []struct {
		path      string
		forbidden []string
	}{
		{
			path: filepath.Join(root, "internal", "assurance", "observe", "lock", "instruction.go"),
			forbidden: []string{
				"ResourceKindInstructions",
				"InstructionsPathProjectionContracts",
				"ManagedFilePlacementFor",
				"internal/realization/profile",
				"targetsurface",
			},
		},
		{
			path: filepath.Join(root, "internal", "effect", "payload", "build", "instructions.go"),
			forbidden: []string{
				"ResourceKindInstructions",
				"InstructionsPathProjectionContracts",
				"ManagedFilePlacementFor",
				"target/selection",
				"targetsurface",
			},
		},
	} {
		content, err := os.ReadFile(check.path)
		if err != nil {
			t.Fatalf("read Instructions convergence consumer %q: %v", check.path, err)
		}
		for _, forbidden := range check.forbidden {
			if strings.Contains(string(content), forbidden) {
				t.Fatalf("Instructions convergence consumer %q regained target selection or placement refinement through %q", check.path, forbidden)
			}
		}
	}

	readinessPlan := filepath.Join(root, "internal", "workflow", "readiness", "assessment.go")
	content, err := os.ReadFile(readinessPlan)
	if err != nil {
		t.Fatalf("read readiness planner assembly: %v", err)
	}
	if strings.Contains(string(content), "ResourceKindInstructions") {
		t.Fatal("generic readiness planner regained a resource-owned Instructions declaration")
	}

	managedPathReadiness := filepath.Join(root, "internal", "workflow", "readiness", "managed_path.go")
	content, err = os.ReadFile(managedPathReadiness)
	if err != nil {
		t.Fatalf("read managed path readiness assembly: %v", err)
	}
	for _, forbidden := range []string{"internal/desired/instructions", ".Instructions()", "InstructionsPathProjectionContracts"} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("generic managed path readiness assembly regained Instructions refinement %q", forbidden)
		}
	}

	lockedCollection := filepath.Join(root, "internal", "realization", "lock", "collection_admission.go")
	content, err = os.ReadFile(lockedCollection)
	if err != nil {
		t.Fatalf("read generic locked collection validator: %v", err)
	}
	if strings.Contains(string(content), "KindInstructions") {
		t.Fatal("generic locked collection validator regained an Instructions family branch")
	}
}
