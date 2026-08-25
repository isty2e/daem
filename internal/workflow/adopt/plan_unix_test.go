//go:build darwin || linux

package adopt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

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
