//go:build darwin || linux

package adopt

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestBuildCommandPlanRejectsSkillRootGrowthBeforeNothingToImport(t *testing.T) {
	root := enterAdoptTestDirectory(t)
	t.Setenv("HOME", root)
	skillsRoot := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(skillsRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	mutated := false
	_, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: filepath.Join(root, "daem.toml"),
		ProgressEvents: func(event ProgressEvent) {
			if mutated || event.Kind != ProgressEventTargetScopeCompleted {
				return
			}
			mutated = true
			skillRoot := filepath.Join(skillsRoot, "review")
			if mkdirErr := os.Mkdir(skillRoot, 0o700); mkdirErr != nil {
				t.Fatalf("create late skill root: %v", mkdirErr)
			}
			if writeErr := os.WriteFile(
				filepath.Join(skillRoot, "SKILL.md"),
				[]byte("---\nname: review\ndescription: Review skill\n---\n"),
				0o600,
			); writeErr != nil {
				t.Fatalf("write late SKILL.md: %v", writeErr)
			}
		},
	})
	if !mutated {
		t.Fatal("test did not mutate the search root after its target/scope pass")
	}
	if err == nil || IsNothingToImport(err) || !strings.Contains(err.Error(), "changed after observation") {
		t.Fatalf("BuildCommandPlan error = %v, want stale search-root observation", err)
	}
}

func TestBuildCommandPlanRejectsSkillRootGrowthBeforeNonemptyPlan(t *testing.T) {
	root := enterAdoptTestDirectory(t)
	t.Setenv("HOME", root)
	skillsRoot := filepath.Join(root, ".agents", "skills")
	initialRoot := filepath.Join(skillsRoot, "initial")
	if err := os.MkdirAll(initialRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(initialRoot, "SKILL.md"),
		[]byte("---\nname: initial\ndescription: Initial skill\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "daem.toml")
	_, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
		ProgressEvents: func(event ProgressEvent) {
			if event.Kind != ProgressEventTargetScopeCompleted {
				return
			}
			lateRoot := filepath.Join(skillsRoot, "late")
			if mkdirErr := os.Mkdir(lateRoot, 0o700); mkdirErr != nil {
				t.Fatalf("create late skill root: %v", mkdirErr)
			}
			if writeErr := os.WriteFile(
				filepath.Join(lateRoot, "SKILL.md"),
				[]byte("---\nname: late\ndescription: Late skill\n---\n"),
				0o600,
			); writeErr != nil {
				t.Fatalf("write late SKILL.md: %v", writeErr)
			}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "changed after observation") {
		t.Fatalf("BuildCommandPlan error = %v, want stale search-root observation", err)
	}
	if _, statErr := os.Lstat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stale search root published manifest: %v", statErr)
	}
}

func TestBuildCommandPlanRejectsEmptySkillRootAliasRetarget(t *testing.T) {
	root := enterAdoptTestDirectory(t)
	t.Setenv("HOME", root)
	first := filepath.Join(root, "first-skills")
	second := filepath.Join(root, "second-skills")
	for _, physicalRoot := range []string{first, second} {
		if err := os.Mkdir(physicalRoot, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeImportSkill(t, filepath.Join(second, "late"), "late")
	alias := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(filepath.Dir(alias), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "daem.toml")
	_, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
		ProgressEvents: func(event ProgressEvent) {
			if event.Kind == ProgressEventTargetScopeCompleted {
				retargetImportSkillRoot(t, alias, second)
			}
		},
	})
	if err == nil || IsNothingToImport(err) || !strings.Contains(err.Error(), "binding") ||
		!strings.Contains(err.Error(), "changed after observation") {
		t.Fatalf("BuildCommandPlan error = %v, want changed root binding", err)
	}
	if _, statErr := os.Lstat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("retargeted empty root published manifest: %v", statErr)
	}
}

func TestExecuteCommandPlanRejectsOverlappingSkillRootAliasRetarget(t *testing.T) {
	root := enterAdoptTestDirectory(t)
	t.Setenv("HOME", root)
	first := filepath.Join(root, "first-skills")
	second := filepath.Join(root, "second-skills")
	writeImportSkill(t, filepath.Join(first, "shared"), "shared")
	writeImportSkill(t, filepath.Join(second, "shared"), "shared")
	writeImportSkill(t, filepath.Join(second, "second-only"), "second-only")
	alias := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(filepath.Dir(alias), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "daem.toml")
	planned, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	route, err := planned.AdoptionPlan().Skills()[0].PrimarySourceRoute()
	if err != nil {
		t.Fatal(err)
	}
	wantReadPath, err := filepath.EvalSymlinks(filepath.Join(first, "shared"))
	if err != nil {
		t.Fatal(err)
	}
	if route.ReadPath != wantReadPath {
		t.Fatalf("planned Skill read path = %q, want captured-root path %q", route.ReadPath, wantReadPath)
	}

	mutated := false
	_, err = ExecuteCommandPlan(t.Context(), planned, func(event ProgressEvent) {
		if mutated || event.Phase != ProgressPhaseRevalidation || event.Kind != ProgressEventTargetScopeCompleted {
			return
		}
		mutated = true
		retargetImportSkillRoot(t, alias, second)
	})
	if !mutated {
		t.Fatal("test did not retarget the Skill root during write-mode revalidation")
	}
	if err == nil || !strings.Contains(err.Error(), "binding") ||
		!strings.Contains(err.Error(), "changed after observation") {
		t.Fatalf("ExecuteCommandPlan error = %v, want changed root binding", err)
	}
	if _, statErr := os.Lstat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("retargeted overlapping root published manifest: %v", statErr)
	}
	for _, skill := range planned.AdoptionPlan().Skills() {
		if _, statErr := os.Lstat(skill.SourcePath); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("retargeted overlapping root published Skill source %q: %v", skill.SourcePath, statErr)
		}
	}
}

func TestBuildCommandPlanRejectsSkillTreeBytesBeforePublication(t *testing.T) {
	root := enterAdoptTestDirectory(t)
	skillRoot := filepath.Join(root, ".agents", "skills", "review")
	if err := os.MkdirAll(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(skillRoot, "SKILL.md"),
		[]byte("---\nname: review\ndescription: Review skill\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	payload, err := os.Create(filepath.Join(skillRoot, "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := payload.Truncate(mutationfs.DefaultTreeTraversalLimits().MaximumBytes() + 1); err != nil {
		t.Fatal(err)
	}
	if err := payload.Close(); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "daem.toml")
	_, err = BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
	})
	if err == nil {
		t.Fatal("BuildCommandPlan accepted a skill over the per-tree byte ceiling")
	}
	if _, statErr := os.Lstat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("over-byte source published manifest: %v", statErr)
	}
}

func TestExecuteCommandPlanRejectsSkillRevisionByteGrowthBeforePublication(t *testing.T) {
	root := enterAdoptTestDirectory(t)
	skillRoot := filepath.Join(root, ".agents", "skills", "review")
	if err := os.MkdirAll(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(skillRoot, "SKILL.md"),
		[]byte("---\nname: review\ndescription: Review skill\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	payloadPath := filepath.Join(skillRoot, "payload.bin")
	if err := os.WriteFile(payloadPath, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "daem.toml")
	planned, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(payloadPath, mutationfs.DefaultTreeTraversalLimits().MaximumBytes()+1); err != nil {
		t.Fatal(err)
	}

	_, err = ExecuteCommandPlan(t.Context(), planned, nil)
	if !errors.Is(err, mutation.ErrRevisionLimitExceeded) {
		t.Fatalf("ExecuteCommandPlan error = %v, want revision limit exhaustion", err)
	}
	var limitErr *mutation.RevisionLimitError
	if !errors.As(err, &limitErr) || limitErr.Kind() != mutation.RevisionLimitTreeBytes {
		t.Fatalf("ExecuteCommandPlan error = %v, want tree-byte exhaustion", err)
	}
	if _, statErr := os.Lstat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("over-byte source published manifest: %v", statErr)
	}
	for _, skill := range planned.AdoptionPlan().Skills() {
		if _, statErr := os.Lstat(skill.SourcePath); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("over-byte source published import path %q: %v", skill.SourcePath, statErr)
		}
	}
}

func writeImportSkill(t *testing.T, root string, name string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "SKILL.md"),
		[]byte("---\nname: "+name+"\ndescription: "+name+" skill\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}

func retargetImportSkillRoot(t *testing.T, alias string, target string) {
	t.Helper()
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
}
