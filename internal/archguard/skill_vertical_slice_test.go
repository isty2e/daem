package archguard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillVerticalSliceCannotRegainLegacyOperationalAuthority(t *testing.T) {
	root := findRepoRoot(t)
	legacyProjector := filepath.Join(root, "internal", "output", "project", "skill")
	if _, err := os.Stat(legacyProjector); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired Skill DesiredOutput projector exists or cannot be inspected: %v", err)
	}

	for _, packagePath := range []string{
		"internal/reconcile",
		"internal/effect/execute",
		"internal/effect/journal",
		"internal/assurance/observe/live",
	} {
		assertProductionPackageOmits(t, root, packagePath, []string{
			"ResourceKindSkill",
			"KindSkill",
		})
	}
	assertTestPackageOmitsExcept(
		t,
		root,
		"internal/reconcile",
		map[string]struct{}{
			"managed_path_test.go":         {},
			"planner_test_helpers_test.go": {},
		},
		"ResourceKindSkill",
	)
	for _, filePath := range []string{
		filepath.Join(root, "internal", "assurance", "observe", "lock", "skill.go"),
		filepath.Join(root, "internal", "effect", "payload", "build", "skill.go"),
	} {
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("read Skill convergence consumer %q: %v", filePath, err)
		}
		for _, forbidden := range []string{"internal/realization/profile", "ResourceKindSkill"} {
			if strings.Contains(string(content), forbidden) {
				t.Fatalf("Skill convergence consumer %q rediscovered target placement through %q", filePath, forbidden)
			}
		}
	}

	readinessPlan := filepath.Join(root, "internal", "workflow", "readiness", "assessment.go")
	content, err := os.ReadFile(readinessPlan)
	if err != nil {
		t.Fatalf("read readiness planner assembly: %v", err)
	}
	if strings.Contains(string(content), "ResourceKindSkill") {
		t.Fatal("generic readiness planner regained a resource-owned Skill declaration")
	}
	managedPathReadiness := filepath.Join(root, "internal", "workflow", "readiness", "managed_path.go")
	content, err = os.ReadFile(managedPathReadiness)
	if err != nil {
		t.Fatalf("read managed path readiness assembly: %v", err)
	}
	for _, forbidden := range []string{"internal/desired/skill", ".Skills()", "SkillPathProjectionContracts"} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("generic managed path readiness assembly regained Skill refinement %q", forbidden)
		}
	}
}

func assertTestPackageOmitsExcept(
	t *testing.T,
	root string,
	packagePath string,
	allowedFiles map[string]struct{},
	forbidden string,
) {
	t.Helper()
	directory := filepath.Join(root, filepath.FromSlash(packagePath))
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read test package %s: %v", packagePath, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		if _, allowed := allowedFiles[entry.Name()]; allowed {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read test file %q: %v", path, err)
		}
		if strings.Contains(string(content), forbidden) {
			t.Errorf("%s retains forbidden legacy Skill operational fixture %q", path, forbidden)
		}
	}
}
