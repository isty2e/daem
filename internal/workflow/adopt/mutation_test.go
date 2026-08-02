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
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
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

func TestPrepareImportParentDirectoriesDoesNotClaimExternalCreation(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "daem.toml")
	sourcePath := filepath.Join(root, "external", "source.md")
	plan := testAdoptPlan(t, output, []adoptmodel.Source{{
		SourcePath: sourcePath,
		Content:    []byte("created"),
	}}, nil)
	canonicalSourcePath, err := mutation.CanonicalDirectoryEntryPath(sourcePath)
	if err != nil {
		t.Fatal(err)
	}

	var cleanup storagecommit.AncestorCleanup
	defer cleanup.Close()
	err = prepareImportParentDirectories(
		context.Background(),
		plan,
		func(ctx context.Context, path string) error {
			if path == canonicalSourcePath {
				if mkdirErr := os.Mkdir(filepath.Dir(path), 0o755); mkdirErr != nil {
					t.Fatalf("create concurrent external parent: %v", mkdirErr)
				}
			}
			return cleanup.PrepareParent(ctx, path)
		},
	)
	if err != nil {
		t.Fatalf("prepareImportParentDirectories returned error: %v", err)
	}
	if err := cleanup.RemoveEmpty(context.Background()); err != nil {
		t.Fatalf("cleanup external parent observation: %v", err)
	}
	if info, statErr := os.Stat(filepath.Dir(sourcePath)); statErr != nil || !info.IsDir() {
		t.Fatalf("external parent was not preserved: info=%v err=%v", info, statErr)
	}
}

func TestCommitNewImportFileTracksParentRecreatedAfterPreflight(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "recreated")
	target := filepath.Join(parent, "source.md")
	var cleanup storagecommit.AncestorCleanup
	defer cleanup.Close()
	if err := cleanup.PrepareParent(context.Background(), target); err != nil {
		t.Fatalf("prepare initial parent: %v", err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatalf("remove prepared parent: %v", err)
	}
	created, err := commitNewImportFile(
		context.Background(),
		target,
		[]byte("content"),
		0o600,
		&cleanup,
	)
	if err != nil {
		t.Fatalf("commit import file after parent removal: %v", err)
	}
	if err := os.Remove(created.path); err != nil {
		t.Fatalf("remove committed import file: %v", err)
	}
	if err := cleanup.RemoveEmpty(context.Background()); err != nil {
		t.Fatalf("remove recreated parent: %v", err)
	}
	if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recreated file parent remains after rollback: %v", err)
	}
}

func TestCopyImportedSkillTracksParentRecreatedAfterPreflight(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source-skill")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "recreated")
	target := filepath.Join(parent, "skill")
	var cleanup storagecommit.AncestorCleanup
	defer cleanup.Close()
	if err := cleanup.PrepareParent(context.Background(), target); err != nil {
		t.Fatalf("prepare initial skill parent: %v", err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatalf("remove prepared skill parent: %v", err)
	}
	contentHash, kind, err := access.HashPath(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if kind != artifact.ArtifactKindDirectory {
		t.Fatalf("source kind = %q, want directory", kind)
	}
	created, err := copyImportedSkillDirectory(context.Background(), adoptmodel.Skill{
		ReadPath: source, SourcePath: target, ContentHash: contentHash,
	}, &cleanup)
	if err != nil {
		t.Fatalf("copy skill after parent removal: %v", err)
	}
	if err := os.RemoveAll(created.path); err != nil {
		t.Fatalf("remove committed skill: %v", err)
	}
	if err := cleanup.RemoveEmpty(context.Background()); err != nil {
		t.Fatalf("remove recreated skill parent: %v", err)
	}
	if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recreated skill parent remains after rollback: %v", err)
	}
}

func TestWritePlanRejectsSkillIdentityDriftBeforePublishingManifest(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{
			name: "rewrite file",
			mutate: func(t *testing.T, source string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "add file",
			mutate: func(t *testing.T, source string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(source, "added.txt"), []byte("added"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "remove file",
			mutate: func(t *testing.T, source string) {
				t.Helper()
				if err := os.Remove(filepath.Join(source, "SKILL.md")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "change executable mode",
			mutate: func(t *testing.T, source string) {
				t.Helper()
				if err := os.Chmod(filepath.Join(source, "SKILL.md"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "introduce nested symlink",
			mutate: func(t *testing.T, source string) {
				t.Helper()
				if err := os.Symlink("SKILL.md", filepath.Join(source, "alias.md")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "replace resolved route with symlink",
			mutate: func(t *testing.T, source string) {
				t.Helper()
				displaced := source + "-displaced"
				if err := os.Rename(source, displaced); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(displaced, source); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "replace directory with regular file",
			mutate: func(t *testing.T, source string) {
				t.Helper()
				if err := os.RemoveAll(source); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(source, []byte("not a skill tree"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "live-skill")
			if err := os.Mkdir(source, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("planned"), 0o600); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(root, "generated", "skill")
			output := filepath.Join(root, "generated", "daem.toml")
			skill := plannedImportedSkill(t, source, destination)
			plan := testAdoptPlan(t, output, nil, []adoptmodel.Skill{skill})

			test.mutate(t, source)
			if err := writePlan(context.Background(), plan, nil); err == nil {
				t.Fatal("writePlan accepted a skill source that changed after planning")
			}
			for _, path := range []string{destination, output} {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("path %q was published after source drift: %v", path, err)
				}
			}
			assertNoImportTreeStage(t, root)
		})
	}
}

func TestWritePlanAcceptsReplacementWithSameExactArtifactIdentity(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "live-skill")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "generated", "skill")
	output := filepath.Join(root, "generated", "daem.toml")
	skill := plannedImportedSkill(t, source, destination)
	plan := testAdoptPlan(t, output, nil, []adoptmodel.Skill{skill})

	displaced := source + "-displaced"
	if err := os.Rename(source, displaced); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writePlan(context.Background(), plan, nil); err != nil {
		t.Fatalf("writePlan rejected semantically identical replacement: %v", err)
	}
	contentHash, kind, err := access.HashPath(context.Background(), destination)
	if err != nil {
		t.Fatal(err)
	}
	if kind != artifact.ArtifactKindDirectory || contentHash != skill.ContentHash {
		t.Fatalf("published identity = (%q, %q), want (%q, %q)", kind, contentHash, artifact.ArtifactKindDirectory, skill.ContentHash)
	}
}

func plannedImportedSkill(t *testing.T, source string, destination string) adoptmodel.Skill {
	t.Helper()
	readPath, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	readPath, err = filepath.Abs(readPath)
	if err != nil {
		t.Fatal(err)
	}
	readPath = filepath.Clean(readPath)
	contentHash, kind, err := access.HashPath(context.Background(), readPath)
	if err != nil {
		t.Fatal(err)
	}
	if kind != artifact.ArtifactKindDirectory {
		t.Fatalf("source kind = %q, want directory", kind)
	}
	return adoptmodel.Skill{
		Target:      target.TargetCodex,
		Scope:       target.ScopeProject,
		LivePath:    source,
		ReadPath:    readPath,
		SourcePath:  destination,
		ContentHash: contentHash,
	}
}

func assertNoImportTreeStage(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if strings.HasPrefix(entry.Name(), ".daem-tmp-") {
			t.Fatalf("unpublished import stage remains at %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect import staging residue: %v", err)
	}
}

func TestWritePlanRollbackPreservesPopulatedCreatedParent(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "created", "source.md")
	externalPath := filepath.Join(filepath.Dir(sourcePath), "external.txt")
	plan := testAdoptPlan(t, filepath.Join(root, "daem.toml"), []adoptmodel.Source{{
		SourcePath: sourcePath,
		Content:    []byte("created"),
	}}, nil)

	err := writePlan(context.Background(), plan, func() error {
		if writeErr := os.WriteFile(externalPath, []byte("keep"), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		return mutation.StaleSnapshotError{}
	})
	if err == nil || !strings.Contains(err.Error(), "is not empty") {
		t.Fatalf("writePlan error = %v, want non-empty parent cleanup refusal", err)
	}
	if _, statErr := os.Lstat(sourcePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("daem-created source remains: %v", statErr)
	}
	if content, readErr := os.ReadFile(externalPath); readErr != nil || string(content) != "keep" {
		t.Fatalf("external content was not preserved: content=%q err=%v", content, readErr)
	}
}

func TestWritePlanRollbackReportsMovedCreatedParent(t *testing.T) {
	root := t.TempDir()
	createdParent := filepath.Join(root, "created")
	displacedParent := filepath.Join(root, "displaced")
	sourcePath := filepath.Join(createdParent, "source.md")
	plan := testAdoptPlan(t, filepath.Join(root, "daem.toml"), []adoptmodel.Source{{
		SourcePath: sourcePath,
		Content:    []byte("created"),
	}}, nil)

	err := writePlan(context.Background(), plan, func() error {
		if renameErr := os.Rename(createdParent, displacedParent); renameErr != nil {
			t.Fatalf("move created import parent: %v", renameErr)
		}
		return mutation.StaleSnapshotError{}
	})
	if err == nil || !strings.Contains(err.Error(), "disappeared before daem retirement") {
		t.Fatalf("writePlan error = %v, want moved ancestor residue", err)
	}
	displacedSource := filepath.Join(displacedParent, filepath.Base(sourcePath))
	if content, readErr := os.ReadFile(displacedSource); readErr != nil || string(content) != "created" {
		t.Fatalf("moved import residue was not preserved: content=%q err=%v", content, readErr)
	}
}

func TestWritePlanRollbackPreservesReplacedCreatedParent(t *testing.T) {
	for _, replacementKind := range []string{"directory", "symlink"} {
		t.Run(replacementKind, func(t *testing.T) {
			root := t.TempDir()
			createdParent := filepath.Join(root, "created")
			sourcePath := filepath.Join(createdParent, "source.md")
			displacedParent := filepath.Join(root, "displaced")
			plan := testAdoptPlan(t, filepath.Join(root, "daem.toml"), []adoptmodel.Source{{
				SourcePath: sourcePath,
				Content:    []byte("created"),
			}}, nil)

			err := writePlan(context.Background(), plan, func() error {
				if renameErr := os.Rename(createdParent, displacedParent); renameErr != nil {
					t.Fatal(renameErr)
				}
				var replaceErr error
				switch replacementKind {
				case "directory":
					replaceErr = os.Mkdir(createdParent, 0o700)
				case "symlink":
					replaceErr = os.Symlink(displacedParent, createdParent)
				}
				if replaceErr != nil {
					t.Fatal(replaceErr)
				}
				return mutation.StaleSnapshotError{}
			})
			if err == nil || !strings.Contains(err.Error(), "identity changed") {
				t.Fatalf("writePlan error = %v, want parent identity cleanup refusal", err)
			}
			if _, statErr := os.Lstat(createdParent); statErr != nil {
				t.Fatalf("replacement parent was not preserved: %v", statErr)
			}
			content, readErr := os.ReadFile(filepath.Join(displacedParent, "source.md"))
			if readErr != nil || string(content) != "created" {
				t.Fatalf("displaced daem source was not retained as residue: content=%q err=%v", content, readErr)
			}
		})
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
	candidates, err := adoptmodel.NewCandidateSet(adoptmodel.CandidateSetInput{
		Sources: sources,
		Skills:  skills,
	})
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
