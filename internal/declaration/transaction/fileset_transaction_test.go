package transaction

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitWritesEveryTargetAndRemovesEvidence(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	first := filepath.Join(root, "a")
	second := filepath.Join(root, "b")
	retained := filepath.Join(root, "c")
	writeFixture(t, first, "first-before", 0o644)
	writeFixture(t, retained, "retained", 0o600)

	firstTarget := mustWriteTarget(t, first, "first-after")
	secondTarget := mustWriteTarget(t, second, "second-after")
	retainedTarget := mustRetainedTarget(t, retained)
	err := CommitFileSet(context.Background(), FileSetInput{
		StateDir: stateDir,
		Targets:  []FileTarget{secondTarget, retainedTarget, firstTarget},
	})
	if err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	assertContent(t, first, "first-after")
	assertContent(t, second, "second-after")
	assertContent(t, retained, "retained")
	assertMode(t, first, 0o644)
	assertMode(t, second, 0o600)
	assertMissing(t, transactionDir(stateDir))
}

func TestCommitRejectsDuplicateCanonicalTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "value")
	first := mustWriteTarget(t, path, "first")
	second := mustWriteTarget(t, filepath.Join(root, ".", "value"), "second")
	err := CommitFileSet(context.Background(), FileSetInput{
		StateDir: mustCanonicalStateDir(t, filepath.Join(root, ".daem")),
		Targets:  []FileTarget{first, second},
	})
	if err == nil || !strings.Contains(err.Error(), "appears more than once") {
		t.Fatalf("Commit error = %v, want duplicate rejection", err)
	}
	assertMissing(t, path)
}

func TestCommitPointPublishesAfterOrdinaryTargets(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	registry := filepath.Join(root, "a-global-registry")
	project := filepath.Join(root, "z-project-state")
	writeFixture(t, registry, "registry-before", 0o600)
	writeFixture(t, project, "project-before", 0o600)
	registryTarget := mustCommitPointTarget(t, registry, "registry-after")
	projectTarget := mustWriteTarget(t, project, "project-after")

	var writes []string
	err := commitWithOperations(context.Background(), FileSetInput{
		StateDir: stateDir,
		Targets: []FileTarget{
			registryTarget,
			projectTarget,
		},
	}, operations{
		writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
			writes = append(writes, path)
			return commitFile(ctx, path, content, mode)
		},
	})
	if err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	if len(writes) != 2 || writes[0] != projectTarget.Path() || writes[1] != registryTarget.Path() {
		t.Fatalf("write order = %v, want [%s %s]", writes, projectTarget.Path(), registryTarget.Path())
	}
	assertContent(t, project, "project-after")
	assertContent(t, registry, "registry-after")
	assertMissing(t, transactionDir(stateDir))
}

func TestHardKillBeforeCommitPointRetainsPublishedAuthority(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	registry := filepath.Join(root, "a-global-registry")
	project := filepath.Join(root, "z-project-state")
	writeFixture(t, registry, "registry-before", 0o600)
	writeFixture(t, project, "project-before", 0o600)
	registryTarget := mustCommitPointTarget(t, registry, "registry-after")
	projectTarget := mustWriteTarget(t, project, "project-after")
	targets := []FileTarget{
		registryTarget,
		projectTarget,
	}

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("Commit did not simulate a hard kill")
			}
		}()
		_ = commitWithOperations(context.Background(), FileSetInput{
			StateDir: stateDir,
			Targets:  targets,
		}, operations{
			writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
				if err := commitFile(ctx, path, content, mode); err != nil {
					return err
				}
				if path == projectTarget.Path() {
					panic("simulated hard kill before commit point")
				}
				return nil
			},
		})
	}()

	assertContent(t, project, "project-after")
	assertContent(t, registry, "registry-before")
	if _, err := os.Stat(markerPath(stateDir)); err != nil {
		t.Fatalf("transaction evidence missing after hard kill: %v", err)
	}
	if err := RecoverFileSet(context.Background(), stateDir, []string{registry, project}); err != nil {
		t.Fatalf("Recover returned error: %v", err)
	}
	assertContent(t, project, "project-before")
	assertContent(t, registry, "registry-before")
	assertMissing(t, transactionDir(stateDir))
}

func TestCommitPointValidationRejectsAmbiguousPublication(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))

	t.Run("multiple commit points", func(t *testing.T) {
		first := filepath.Join(root, "first")
		second := filepath.Join(root, "second")
		err := CommitFileSet(context.Background(), FileSetInput{
			StateDir: stateDir,
			Targets: []FileTarget{
				mustCommitPointTarget(t, first, "first"),
				mustCommitPointTarget(t, second, "second"),
			},
		})
		if err == nil || !strings.Contains(err.Error(), "at most one commit point") {
			t.Fatalf("Commit error = %v, want multiple commit-point rejection", err)
		}
		assertMissing(t, first)
		assertMissing(t, second)
	})

	t.Run("duplicate ordinary and commit point", func(t *testing.T) {
		path := filepath.Join(root, "duplicate")
		intervening := filepath.Join(root, "zz-intervening")
		err := CommitFileSet(context.Background(), FileSetInput{
			StateDir: stateDir,
			Targets: []FileTarget{
				mustWriteTarget(t, path, "ordinary"),
				mustWriteTarget(t, intervening, "intervening"),
				mustCommitPointTarget(t, path, "published"),
			},
		})
		if err == nil || !strings.Contains(err.Error(), "appears more than once") {
			t.Fatalf("Commit error = %v, want duplicate rejection", err)
		}
		assertMissing(t, path)
		assertMissing(t, intervening)
	})
}

func TestRequireClearFileSetDistinguishesAbsentAndDamagedEvidence(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := RequireClearFileSet(context.Background(), stateDir); err != nil {
			t.Fatalf("RequireClearFileSet returned error: %v", err)
		}
	})

	t.Run("empty evidence directory", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := os.MkdirAll(transactionDir(stateDir), 0o700); err != nil {
			t.Fatal(err)
		}
		err := RequireClearFileSet(context.Background(), stateDir)
		if err == nil || !strings.Contains(err.Error(), "evidence") ||
			!strings.Contains(err.Error(), "marker is missing") {
			t.Fatalf("RequireClearFileSet error = %v, want incomplete evidence rejection", err)
		}
	})

	t.Run("non-directory evidence", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		writeFixture(t, transactionDir(stateDir), "not-a-directory", 0o600)
		err := RequireClearFileSet(context.Background(), stateDir)
		if err == nil || !strings.Contains(err.Error(), "is not a directory") {
			t.Fatalf("RequireClearFileSet error = %v, want evidence type rejection", err)
		}
	})

	t.Run("valid interrupted marker", func(t *testing.T) {
		root := t.TempDir()
		stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
		target := mustRetainedTarget(t, filepath.Join(root, "target"))
		if _, err := prepareMarker(context.Background(), stateDir, []FileTarget{target}); err != nil {
			t.Fatal(err)
		}
		err := RequireClearFileSet(context.Background(), stateDir)
		if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
			t.Fatalf("RequireClearFileSet error = %v, want interrupted transaction rejection", err)
		}
	})
}

func TestCommitFailureRestoresCompleteBeforeSet(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	first := filepath.Join(root, "a")
	second := filepath.Join(root, "b")
	writeFixture(t, first, "first-before", 0o600)
	writeFixture(t, second, "second-before", 0o600)
	targets := []FileTarget{
		mustWriteTarget(t, first, "first-after"),
		mustWriteTarget(t, second, "second-after"),
	}
	writes := 0
	err := commitWithOperations(context.Background(), FileSetInput{
		StateDir: stateDir,
		Targets:  targets,
	}, operations{
		writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
			writes++
			if writes == 2 {
				return errors.New("injected second write failure")
			}
			return commitFile(ctx, path, content, mode)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "rolled back file set") {
		t.Fatalf("Commit error = %v, want rollback", err)
	}
	assertContent(t, first, "first-before")
	assertContent(t, second, "second-before")
	assertMissing(t, transactionDir(stateDir))
}

func TestRecoverRestoresMixedBeforeAfterSet(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	first := filepath.Join(root, "a")
	second := filepath.Join(root, "b")
	writeFixture(t, first, "first-before", 0o600)
	writeFixture(t, second, "second-before", 0o600)
	targets := []FileTarget{
		mustWriteTarget(t, first, "first-after"),
		mustWriteTarget(t, second, "second-after"),
	}
	canonical, err := canonicalTargets(targets)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := prepareMarker(context.Background(), stateDir, canonical)
	if err != nil {
		t.Fatal(err)
	}
	if err := commitFile(context.Background(), marker.Targets[0].Path, canonical[0].content, fileMode(marker.Targets[0].Before)); err != nil {
		t.Fatal(err)
	}

	if err := RecoverFileSet(context.Background(), stateDir, []string{first, second}); err != nil {
		t.Fatalf("Recover returned error: %v", err)
	}
	assertContent(t, first, "first-before")
	assertContent(t, second, "second-before")
	assertMissing(t, transactionDir(stateDir))
}

func TestRecoverAcceptsCompleteAfterSetAndRejectsForeignAuthority(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	first := filepath.Join(root, "a")
	second := filepath.Join(root, "b")
	writeFixture(t, first, "first-before", 0o600)
	writeFixture(t, second, "second-before", 0o600)
	targets, err := canonicalTargets([]FileTarget{
		mustWriteTarget(t, first, "first-after"),
		mustWriteTarget(t, second, "second-after"),
	})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := prepareMarker(context.Background(), stateDir, targets)
	if err != nil {
		t.Fatal(err)
	}
	for index, target := range targets {
		if err := commitFile(context.Background(), target.path, target.content, fileMode(marker.Targets[index].Before)); err != nil {
			t.Fatal(err)
		}
	}

	if err := RecoverFileSet(context.Background(), stateDir, []string{first}); err == nil ||
		!strings.Contains(err.Error(), "outside current recovery authority") {
		t.Fatalf("Recover foreign authority error = %v", err)
	}
	if err := RecoverFileSet(context.Background(), stateDir, []string{second, first, filepath.Join(root, "unused")}); err != nil {
		t.Fatalf("Recover complete after-set returned error: %v", err)
	}
	assertContent(t, first, "first-after")
	assertContent(t, second, "second-after")
	assertMissing(t, transactionDir(stateDir))
}

func TestRecoverRefusesDriftAndRetainsEvidence(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	path := filepath.Join(root, "value")
	writeFixture(t, path, "before", 0o600)
	targets, err := canonicalTargets([]FileTarget{mustWriteTarget(t, path, "after")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareMarker(context.Background(), stateDir, targets); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, path, "foreign", 0o600)

	err = RecoverFileSet(context.Background(), stateDir, []string{path})
	if err == nil || !strings.Contains(err.Error(), "cannot be recovered automatically") {
		t.Fatalf("Recover error = %v, want drift refusal", err)
	}
	if _, statErr := os.Stat(markerPath(stateDir)); statErr != nil {
		t.Fatalf("recovery evidence missing: %v", statErr)
	}
	assertContent(t, path, "foreign")
}

func TestLoadMarkerRejectsCorruptAndExposedEvidence(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	path := filepath.Join(root, "value")
	writeFixture(t, path, "before", 0o600)
	targets, err := canonicalTargets([]FileTarget{mustWriteTarget(t, path, "after")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareMarker(context.Background(), stateDir, targets); err != nil {
		t.Fatal(err)
	}
	activeMarker := markerPath(stateDir)
	content, err := os.ReadFile(activeMarker)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["unknown"] = true
	content, err = json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activeMarker, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMarker(context.Background(), activeMarker); err == nil ||
		!strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("loadMarker error = %v, want strict unknown-field rejection", err)
	}

	if err := os.WriteFile(activeMarker, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(activeMarker, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMarker(context.Background(), activeMarker); err == nil ||
		!strings.Contains(err.Error(), "permissions") {
		t.Fatalf("loadMarker error = %v, want permission rejection", err)
	}
}

func TestCancellationRollsBackPartialSet(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	first := filepath.Join(root, "a")
	second := filepath.Join(root, "b")
	writeFixture(t, first, "first-before", 0o600)
	writeFixture(t, second, "second-before", 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	writes := 0
	err := commitWithOperations(ctx, FileSetInput{
		StateDir: stateDir,
		Targets: []FileTarget{
			mustWriteTarget(t, first, "first-after"),
			mustWriteTarget(t, second, "second-after"),
		},
	}, operations{
		writeFile: func(writeCtx context.Context, path string, content []byte, mode os.FileMode) error {
			writes++
			if err := commitFile(writeCtx, path, content, mode); err != nil {
				return err
			}
			if writes == 1 {
				cancel()
			}
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Commit error = %v, want cancellation", err)
	}
	assertContent(t, first, "first-before")
	assertContent(t, second, "second-before")
	assertMissing(t, transactionDir(stateDir))
}

func mustWriteTarget(t *testing.T, path string, content string) FileTarget {
	t.Helper()
	target, err := NewFileWrite(path, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func mustCommitPointTarget(t *testing.T, path string, content string) FileTarget {
	t.Helper()
	target, err := NewFileCommitPointWrite(path, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func mustCanonicalStateDir(t *testing.T, path string) string {
	t.Helper()
	canonical, err := canonicalStateDir(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func mustRetainedTarget(t *testing.T, path string) FileTarget {
	t.Helper()
	target, err := NewFileRetain(path)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func writeFixture(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertContent(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("%s = %q, want %q", path, content, want)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode = %04o, want %04o", path, info.Mode().Perm(), want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists or stat failed: %v", path, err)
	}
}
