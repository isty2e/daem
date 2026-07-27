package adopt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestWritePlanStaleValidationRemovesOnlyCreatedPathsAndDirectories(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "generated", "daem.toml")
	sourcePath := filepath.Join(root, "generated", "sources", "project.md")
	plan := testAdoptPlan(t, output, []adoptmodel.Source{{
		SourcePath: sourcePath,
		Content:    []byte("content"),
	}}, nil)
	err := writePlan(context.Background(), plan, func() error { return mutation.StaleSnapshotError{} })
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("writePlan error = %v, want StaleSnapshotError", err)
	}
	for _, path := range []string{output, sourcePath, filepath.Dir(sourcePath), filepath.Dir(output)} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("created path %q remains after rollback: %v", path, statErr)
		}
	}
}

func TestWritePlanCollisionDoesNotRemovePreexistingDestination(t *testing.T) {
	root := t.TempDir()
	preexisting := filepath.Join(root, "sources", "occupied.md")
	if err := os.MkdirAll(filepath.Dir(preexisting), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preexisting, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(root, "sources", "created.md")
	plan := testAdoptPlan(t, filepath.Join(root, "daem.toml"), []adoptmodel.Source{
		{SourcePath: created, Content: []byte("created")},
		{SourcePath: preexisting, Content: []byte("replace")},
	}, nil)
	err := writePlan(context.Background(), plan, nil)
	if err == nil {
		t.Fatal("writePlan accepted an occupied destination")
	}
	if _, statErr := os.Stat(created); !os.IsNotExist(statErr) {
		t.Fatalf("created source remains after collision: %v", statErr)
	}
	content, readErr := os.ReadFile(preexisting)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := string(content); got != "keep" {
		t.Fatalf("preexisting destination content = %q", got)
	}
}

func TestWritePlanCleanupRefusesCreatedPathThatDrifted(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "sources", "project.md")
	plan := testAdoptPlan(t, filepath.Join(root, "daem.toml"), []adoptmodel.Source{{
		SourcePath: sourcePath,
		Content:    []byte("created"),
	}}, nil)
	err := writePlan(context.Background(), plan, func() error {
		if writeErr := os.WriteFile(sourcePath, []byte("external"), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		return mutation.StaleSnapshotError{}
	})
	if err == nil || !strings.Contains(err.Error(), "cleanup refused") {
		t.Fatalf("writePlan error = %v, want cleanup refusal", err)
	}
	content, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := string(content); got != "external" {
		t.Fatalf("drifted destination content = %q", got)
	}
}

func TestWritePlanCleanupRefusesIdenticalReplacement(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "sources", "project.md")
	displacedPath := filepath.Join(root, "displaced.md")
	plan := testAdoptPlan(t, filepath.Join(root, "daem.toml"), []adoptmodel.Source{{
		SourcePath: sourcePath,
		Content:    []byte("created"),
	}}, nil)
	err := writePlan(context.Background(), plan, func() error {
		if renameErr := os.Rename(sourcePath, displacedPath); renameErr != nil {
			t.Fatal(renameErr)
		}
		if writeErr := os.WriteFile(sourcePath, []byte("created"), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		return mutation.StaleSnapshotError{}
	})
	if err == nil || !strings.Contains(err.Error(), "was replaced after creation; cleanup refused") {
		t.Fatalf("writePlan error = %v, want identity cleanup refusal", err)
	}
	content, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := string(content); got != "created" {
		t.Fatalf("replacement destination content = %q", got)
	}
}

func TestWritePlanCancellationAfterSourceCreationRollsBack(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "generated", "daem.toml")
	sourcePath := filepath.Join(root, "generated", "sources", "project.md")
	ctx, cancel := context.WithCancel(context.Background())
	plan := testAdoptPlan(t, output, []adoptmodel.Source{{
		SourcePath: sourcePath,
		Content:    []byte("created"),
	}}, nil)
	err := writePlan(ctx, plan, func() error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("writePlan error = %v, want context cancellation", err)
	}
	for _, path := range []string{output, sourcePath, filepath.Dir(sourcePath), filepath.Dir(output)} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("created path %q remains after cancellation: %v", path, statErr)
		}
	}
}

func TestImportMutationEvidenceGuardsSkillEntryAndReferent(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "live")
	read := filepath.Join(root, "read")
	output := filepath.Join(root, "daem.toml")
	plan := testAdoptPlan(t, output, nil, []adoptmodel.Skill{{
		Target: target.TargetCodex, Scope: target.ScopeGlobal,
		LivePath: live, ReadPath: read, SourcePath: filepath.Join(root, "sources", "skill"),
	}})
	_, requests, stable, err := importMutationEvidence(plan)
	if err != nil {
		t.Fatal(err)
	}
	assertImportRevisionRequest(t, requests, live, mutation.PathEffectDirectoryEntry)
	assertImportRevisionRequest(t, requests, read, mutation.PathEffectReferent)
	assertImportRevisionRequest(t, requests, filepath.Join(root, "daem.toml"), mutation.PathEffectDirectoryEntry)
	assertImportRevisionRequest(t, requests, filepath.Join(root, "daem.toml"), mutation.PathEffectReferent)
	assertImportRevisionRequest(t, stable, live, mutation.PathEffectDirectoryEntry)
	assertImportRevisionRequest(t, stable, read, mutation.PathEffectReferent)
	assertImportRevisionRequest(t, stable, filepath.Join(root, "daem.toml"), mutation.PathEffectDirectoryEntry)
	assertImportRevisionRequest(t, stable, filepath.Join(root, "daem.toml"), mutation.PathEffectReferent)
}

func testAdoptPlan(
	t *testing.T,
	output string,
	sources []adoptmodel.Source,
	skills []adoptmodel.Skill,
) adoptmodel.Plan {
	t.Helper()
	root := filepath.Dir(output)
	sourceDirectory, err := adoptmodel.NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest(
		profile.ImportableTargets(),
		[]target.Scope{target.ScopeProject, target.ScopeGlobal},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	for index := range sources {
		if sources[index].ResourceName == "" {
			sources[index].ResourceName = "source"
		}
		if sources[index].Target == "" {
			sources[index].Target = target.TargetCodex
		}
		if sources[index].Scope == "" {
			sources[index].Scope = target.ScopeProject
		}
		if sources[index].LivePath == "" {
			sources[index].LivePath = sources[index].SourcePath + ".live"
		}
	}
	for index := range skills {
		if skills[index].ResourceName == "" {
			skills[index].ResourceName = "skill"
		}
		if skills[index].InstallName == "" {
			skills[index].InstallName = "skill"
		}
		if len(skills[index].Targets) == 0 {
			skills[index].Targets = []target.Target{skills[index].Target}
		}
		if skills[index].ContentHash == "" {
			skills[index].ContentHash = artifact.HashFileContent([]byte("mutation test skill"))
		}
	}
	candidates, err := adoptmodel.NewCandidateSet(sources, skills, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := adoptmodel.NewPlan(request, nil, []byte("version = 1\n"), candidates, nil)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func assertImportRevisionRequest(t *testing.T, requests []mutation.RevisionRequest, path string, effect mutation.PathEffect) {
	t.Helper()
	for _, request := range requests {
		if request.Path == path && request.Effect == effect {
			return
		}
	}
	t.Fatalf("revision request %q/%d missing from %#v", path, effect, requests)
}
