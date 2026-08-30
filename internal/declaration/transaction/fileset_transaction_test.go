package transaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
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

func TestObserveClearFenceDistinguishesAbsentAndDamagedEvidence(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := ObserveClearFence(context.Background(), stateDir); err != nil {
			t.Fatalf("ObserveClearFence returned error: %v", err)
		}
	})

	t.Run("empty evidence directory", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := os.MkdirAll(transactionDir(stateDir), 0o700); err != nil {
			t.Fatal(err)
		}
		err := ObserveClearFence(context.Background(), stateDir)
		if err == nil || !errors.Is(err, ErrFileSetEvidenceInvalid) ||
			FileSetFenceKindOf(err) != FileSetFenceInvalidEvidence ||
			!strings.Contains(err.Error(), "marker is missing") {
			t.Fatalf("ObserveClearFence error = %v, want typed incomplete evidence rejection", err)
		}
	})

	t.Run("non-directory evidence", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		writeFixture(t, transactionDir(stateDir), "not-a-directory", 0o600)
		err := ObserveClearFence(context.Background(), stateDir)
		if err == nil || !errors.Is(err, ErrFileSetEvidenceInvalid) ||
			FileSetFenceKindOf(err) != FileSetFenceInvalidEvidence ||
			!strings.Contains(err.Error(), "is not a directory") {
			t.Fatalf("ObserveClearFence error = %v, want typed evidence rejection", err)
		}
	})

	t.Run("valid interrupted marker", func(t *testing.T) {
		root := t.TempDir()
		stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
		target := mustRetainedTarget(t, filepath.Join(root, "target"))
		if _, err := prepareMarker(context.Background(), stateDir, []FileTarget{target}); err != nil {
			t.Fatal(err)
		}
		err := ObserveClearFence(context.Background(), stateDir)
		if err == nil || !errors.Is(err, ErrInterruptedFileSetTransaction) ||
			FileSetFenceKindOf(err) != FileSetFencePublishedTransaction {
			t.Fatalf("ObserveClearFence error = %v, want typed interrupted transaction rejection", err)
		}
	})
}

func TestCanonicalStateDirWrapsSupportedPathFailures(t *testing.T) {
	t.Parallel()

	t.Run("empty path stays input validation", func(t *testing.T) {
		t.Parallel()
		_, err := canonicalStateDir("")
		if err == nil || !strings.Contains(err.Error(), "state dir is required") {
			t.Fatalf("canonicalStateDir error = %v, want required-path validation", err)
		}
		if errors.Is(err, ErrFileSetFenceUnprovable) {
			t.Fatalf("empty path must not be ErrFileSetFenceUnprovable: %v", err)
		}
	})

	t.Run("regular file is unprovable", func(t *testing.T) {
		t.Parallel()
		stateDir := filepath.Join(t.TempDir(), ".daem")
		if err := os.WriteFile(stateDir, []byte("not-a-directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := canonicalStateDir(stateDir)
		if err == nil || !errors.Is(err, ErrFileSetFenceUnprovable) ||
			!errors.Is(err, ErrFileSetAccessUnprovable) ||
			FileSetFenceKindOf(err) != FileSetFenceAccessUnprovable {
			t.Fatalf("canonicalStateDir error = %v, want typed StateDir access failure", err)
		}
		err = ObserveClearFence(context.Background(), stateDir)
		if err == nil || !errors.Is(err, ErrFileSetFenceUnprovable) {
			t.Fatalf("ObserveClearFence error = %v, want ErrFileSetFenceUnprovable", err)
		}
		err = RecoverFileSet(context.Background(), stateDir, nil)
		if err == nil || !errors.Is(err, ErrFileSetFenceUnprovable) {
			t.Fatalf("RecoverFileSet error = %v, want ErrFileSetFenceUnprovable", err)
		}
	})

	t.Run("dangling symlink is unprovable", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		stateDir := filepath.Join(root, ".daem")
		if err := os.Symlink(filepath.Join(root, "missing-state"), stateDir); err != nil {
			t.Fatal(err)
		}
		_, err := canonicalStateDir(stateDir)
		if err == nil || !errors.Is(err, ErrFileSetFenceUnprovable) ||
			!errors.Is(err, ErrFileSetAccessUnprovable) ||
			FileSetFenceKindOf(err) != FileSetFenceAccessUnprovable {
			t.Fatalf("canonicalStateDir error = %v, want typed StateDir access failure", err)
		}
		_, err = FileSetAuthorityPath(stateDir)
		if err == nil || !errors.Is(err, ErrFileSetFenceUnprovable) {
			t.Fatalf("FileSetAuthorityPath error = %v, want ErrFileSetFenceUnprovable", err)
		}
	})
}

func TestFileSetFenceRejectsAbandonedPrivateResidue(t *testing.T) {
	t.Run("unpublished stage directory", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		residue := filepath.Join(stateDir, fileSetTemporaryPrefix+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		if err := os.Mkdir(residue, 0o700); err != nil {
			t.Fatal(err)
		}
		assertFileSetFenceDirty(t, stateDir, residue)
	})

	t.Run("legacy unpublished stage directory", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		residue := filepath.Join(stateDir, fileSetLegacyStagePrefix+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		if err := os.Mkdir(residue, 0o700); err != nil {
			t.Fatal(err)
		}
		assertFileSetFenceDirty(t, stateDir, residue)
	})

	t.Run("tombstone directory", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		residue := filepath.Join(stateDir, fileSetTombstonePrefix+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		if err := os.Mkdir(residue, 0o700); err != nil {
			t.Fatal(err)
		}
		assertFileSetFenceDirty(t, stateDir, residue)
	})

	t.Run("cleanup directory", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		residue := filepath.Join(stateDir, fileSetCleanupPrefix+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		if err := os.Mkdir(residue, 0o700); err != nil {
			t.Fatal(err)
		}
		assertFileSetFenceDirty(t, stateDir, residue)
	})

	t.Run("regular temporary file is not file-set residue", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := os.MkdirAll(filepath.Join(stateDir, "cache"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(stateDir, "recovery"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, filepath.Join(stateDir, "state.json"), "{}", 0o600)
		writeFixture(t, filepath.Join(stateDir, fileSetTemporaryPrefix+"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), "stage", 0o600)
		writeFixture(t, filepath.Join(stateDir, fileSetLegacyStagePrefix+"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), "legacy", 0o600)
		if err := ObserveClearFence(context.Background(), stateDir); err != nil {
			t.Fatalf("ObserveClearFence returned error: %v", err)
		}
		if err := RecoverFileSet(context.Background(), stateDir, nil); err != nil {
			t.Fatalf("RecoverFileSet returned error: %v", err)
		}
	})

	t.Run("nested recovery private names are not file-set residue", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		nested := filepath.Join(stateDir, "recovery", fileSetTemporaryPrefix+"cccccccccccccccccccccccccccccccc")
		if err := os.MkdirAll(nested, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := ObserveClearFence(context.Background(), stateDir); err != nil {
			t.Fatalf("ObserveClearFence returned error: %v", err)
		}
	})

	t.Run("cancelled inspection fails closed", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := ObserveClearFence(ctx, stateDir)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("ObserveClearFence error = %v, want context.Canceled", err)
		}
	})

	t.Run("entry overflow fails closed", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"state.json", "cache", "recovery"} {
			if err := os.WriteFile(filepath.Join(stateDir, name), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, err := inspectAbandonedFileSetResidueLimited(context.Background(), stateDir, 2)
		if err == nil || !errors.Is(err, ErrFileSetFenceUnprovable) ||
			!errors.Is(err, ErrFileSetFenceCensusLimit) ||
			FileSetFenceKindOf(err) != FileSetFenceCensusLimit ||
			!strings.Contains(err.Error(), "exceeds 2 entries") {
			t.Fatalf("inspectAbandonedFileSetResidueLimited error = %v, want typed census limit", err)
		}
	})

	t.Run("last-batch cancellation fails closed", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		firstSuccess := 0
		for cancelAt := 1; cancelAt <= 16; cancelAt++ {
			var calls atomic.Int32
			ctx := &cancelAfterContext{Context: context.Background(), calls: &calls, cancelAt: int32(cancelAt)}
			_, err := inspectAbandonedFileSetResidueLimited(ctx, stateDir, maximumStateDirFenceEntries)
			if errors.Is(err, context.Canceled) {
				continue
			}
			if err != nil {
				t.Fatalf("inspectAbandonedFileSetResidueLimited cancelAt=%d error = %v", cancelAt, err)
			}
			firstSuccess = cancelAt
			break
		}
		if firstSuccess < 4 {
			t.Fatalf(
				"successful empty-dir return at cancelAt=%d; last-batch EOF path did not check context before success",
				firstSuccess,
			)
		}
	})

	t.Run("residue diagnostics are sorted", func(t *testing.T) {
		stateDir := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), ".daem"))
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		later := filepath.Join(stateDir, fileSetTemporaryPrefix+"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
		earlier := filepath.Join(stateDir, fileSetTemporaryPrefix+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		for _, residue := range []string{later, earlier} {
			if err := os.Mkdir(residue, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		err := ObserveClearFence(context.Background(), stateDir)
		if err == nil || !errors.Is(err, ErrAbandonedFileSetResidue) {
			t.Fatalf("ObserveClearFence error = %v, want ErrAbandonedFileSetResidue", err)
		}
		if !strings.Contains(err.Error(), "first "+earlier) {
			t.Fatalf("ObserveClearFence error = %v, want sorted first path %s", err, earlier)
		}
	})

	t.Run("non-directory state dir is unprovable", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), "state-file")
		writeFixture(t, stateDir, "not a directory", 0o600)
		_, err := inspectAbandonedFileSetResidueLimited(context.Background(), stateDir, maximumStateDirFenceEntries)
		if err == nil || !errors.Is(err, ErrFileSetFenceUnprovable) {
			t.Fatalf("inspectAbandonedFileSetResidueLimited error = %v, want ErrFileSetFenceUnprovable", err)
		}
	})

	t.Run("interrupted marker plus sibling residue blocks prepare", func(t *testing.T) {
		root := t.TempDir()
		stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
		targetPath := filepath.Join(root, "target")
		writeFixture(t, targetPath, "before", 0o600)
		target := mustWriteTarget(t, targetPath, "after")
		if _, err := prepareMarker(context.Background(), stateDir, []FileTarget{target}); err != nil {
			t.Fatal(err)
		}
		residue := filepath.Join(stateDir, fileSetTemporaryPrefix+"dddddddddddddddddddddddddddddddd")
		if err := os.Mkdir(residue, 0o700); err != nil {
			t.Fatal(err)
		}
		err := CommitFileSet(context.Background(), FileSetInput{
			StateDir: stateDir,
			Targets:  []FileTarget{target},
		})
		if err == nil || !errors.Is(err, ErrAbandonedFileSetResidue) || !strings.Contains(err.Error(), residue) {
			t.Fatalf("CommitFileSet error = %v, want residue at %s", err, residue)
		}
		assertContent(t, targetPath, "before")
		if _, statErr := os.Lstat(transactionDir(stateDir)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("published evidence exists after residue rejection: %v", statErr)
		}
		if _, statErr := os.Lstat(residue); statErr != nil {
			t.Fatalf("sibling residue was removed: %v", statErr)
		}
	})
}

func assertFileSetFenceDirty(t *testing.T, stateDir string, residue string) {
	t.Helper()
	err := ObserveClearFence(context.Background(), stateDir)
	if err == nil || !errors.Is(err, ErrAbandonedFileSetResidue) || !strings.Contains(err.Error(), residue) {
		t.Fatalf("ObserveClearFence error = %v, want residue at %s", err, residue)
	}
	err = RecoverFileSet(context.Background(), stateDir, nil)
	if err == nil || !errors.Is(err, ErrAbandonedFileSetResidue) || !strings.Contains(err.Error(), residue) {
		t.Fatalf("RecoverFileSet error = %v, want residue at %s", err, residue)
	}
	root := filepath.Dir(stateDir)
	target := mustWriteTarget(t, filepath.Join(root, "target"), "after")
	err = CommitFileSet(context.Background(), FileSetInput{
		StateDir: stateDir,
		Targets:  []FileTarget{target},
	})
	if err == nil || !strings.Contains(err.Error(), "residue") {
		t.Fatalf("CommitFileSet error = %v, want residue rejection before prepare", err)
	}
	if _, statErr := os.Lstat(transactionDir(stateDir)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("published evidence exists after residue rejection: %v", statErr)
	}
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

func TestLoadMarkerRejectsNonCanonicalAndAbsentAuthorityFields(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "value")
	versionField := fmt.Sprintf(`"version":%d`, contractversion.MetadataTransaction)
	valid := fmt.Sprintf(
		`{"version":%d,"targets":[{"path":%q,"before":{"exists":false},"write":true,"after_hash":"sha256:%s"}]}`,
		contractversion.MetadataTransaction,
		path,
		strings.Repeat("a", 64),
	)
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "root alias", content: strings.Replace(valid, `"version"`, `"Version"`, 1), want: "ASCII lower_snake_case"},
		{name: "nested alias", content: strings.Replace(valid, `"before"`, `"Before"`, 1), want: "ASCII lower_snake_case"},
		{name: "case-folded duplicate", content: strings.Replace(valid, versionField, versionField+fmt.Sprintf(`,"Version":%d`, contractversion.MetadataTransaction), 1), want: "ASCII lower_snake_case"},
		{name: "unknown field", content: strings.Replace(valid, versionField, versionField+`,"unknown":true`, 1), want: "unknown field"},
		{name: "nested unknown field", content: strings.Replace(valid, `"exists":false`, `"exists":false,"unknown":true`, 1), want: "unknown field"},
		{name: "missing path", content: strings.Replace(valid, `"path":`+fmt.Sprintf("%q", path)+`,`, "", 1), want: `targets[0] field "path" is required`},
		{name: "missing targets", content: strings.Replace(valid, `,"targets":[{"path":`+fmt.Sprintf("%q", path)+`,"before":{"exists":false},"write":true,"after_hash":"sha256:`+strings.Repeat("a", 64)+`"}]`, "", 1), want: `field "targets" is required`},
		{name: "missing before", content: strings.Replace(valid, `"before":{"exists":false},`, "", 1), want: `targets[0] field "before" is required`},
		{name: "null before", content: strings.Replace(valid, `"before":{"exists":false}`, `"before":null`, 1), want: `targets[0] field "before" is required`},
		{name: "missing exists", content: strings.Replace(valid, `"exists":false`, "", 1), want: `targets[0].before field "exists" is required`},
		{name: "null exists", content: strings.Replace(valid, `"exists":false`, `"exists":null`, 1), want: `targets[0].before field "exists" is required`},
		{name: "null commit point", content: strings.Replace(valid, `"write":true`, `"write":true,"commit_point":null`, 1), want: `targets[0] field "commit_point" must not be null`},
		{name: "missing write", content: strings.Replace(valid, `"write":true,`, "", 1), want: `targets[0] field "write" is required`},
		{name: "null write", content: strings.Replace(valid, `"write":true`, `"write":null`, 1), want: `targets[0] field "write" is required`},
		{name: "missing after hash", content: strings.Replace(valid, `,"after_hash":"sha256:`+strings.Repeat("a", 64)+`"`, "", 1), want: `targets[0] field "after_hash" is required`},
		{name: "null after hash", content: strings.Replace(valid, `"after_hash":"sha256:`+strings.Repeat("a", 64)+`"`, `"after_hash":null`, 1), want: `targets[0] field "after_hash" must not be null`},
		{name: "missing existing hash", content: strings.Replace(valid, `"exists":false`, `"exists":true`, 1), want: `targets[0].before field "hash" is required`},
		{name: "future version", content: strings.Replace(valid, versionField, fmt.Sprintf(`"version":%d`, contractversion.MetadataTransaction+1), 1), want: "written by a newer daem"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			markerPath := filepath.Join(t.TempDir(), transactionMarkerFile)
			if err := os.WriteFile(markerPath, []byte(test.content), transactionEvidenceMode); err != nil {
				t.Fatal(err)
			}
			if _, err := loadMarker(context.Background(), markerPath); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("loadMarker error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadMarkerRejectsLegacyVersionsWithoutReinterpretingEvidence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "value")
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprintf("version %d", version), func(t *testing.T) {
			content := fmt.Sprintf(
				`{"version":%d,"targets":[{"path":%q,"before":{"exists":false},"write":true,"after_hash":"sha256:%s"}]}`,
				version,
				path,
				strings.Repeat("a", 64),
			)
			markerPath := filepath.Join(t.TempDir(), transactionMarkerFile)
			if err := os.WriteFile(markerPath, []byte(content), transactionEvidenceMode); err != nil {
				t.Fatal(err)
			}
			_, err := loadMarker(context.Background(), markerPath)
			if err == nil ||
				!strings.Contains(err.Error(), "use the daem version that wrote it") ||
				!strings.Contains(err.Error(), "do not delete the transaction evidence") {
				t.Fatalf("loadMarker legacy version error = %v, want preservation guidance", err)
			}
		})
	}
}

func TestRecoverPreservesOversizedLegacyBeforeImageForOldWriter(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	evidenceDir := transactionDir(stateDir)
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(root, "target")
	writeFixture(t, targetPath, "after", 0o600)
	backupPath := filepath.Join(evidenceDir, "target-000.before")
	backup, err := os.Create(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := backup.Truncate(maximumTargetBytes + 1); err != nil {
		_ = backup.Close()
		t.Fatal(err)
	}
	if err := backup.Close(); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf(
		`{"version":2,"targets":[{"path":%q,"before":{"exists":true,"hash":"sha256:%s","backup_path":%q,"mode":384},"write":true,"after_hash":"sha256:%s"}]}`,
		targetPath,
		strings.Repeat("a", 64),
		backupPath,
		strings.Repeat("b", 64),
	)
	if err := os.WriteFile(markerPath(stateDir), []byte(content), transactionEvidenceMode); err != nil {
		t.Fatal(err)
	}

	err = RecoverFileSet(t.Context(), stateDir, []string{targetPath})
	if err == nil || !strings.Contains(err.Error(), "use the daem version that wrote it") {
		t.Fatalf("RecoverFileSet error = %v, want old-writer recovery guidance", err)
	}
	assertContent(t, targetPath, "after")
	if info, err := os.Stat(backupPath); err != nil || info.Size() != maximumTargetBytes+1 {
		t.Fatalf("legacy backup was not preserved: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(markerPath(stateDir)); err != nil {
		t.Fatalf("legacy marker was not preserved: %v", err)
	}
}

func TestLoadMarkerRejectsCurrentSchemaTargetCardinality(t *testing.T) {
	root := t.TempDir()
	markerFile := filepath.Join(root, transactionMarkerFile)
	targets := make([]targetMarker, 0, maximumFileSetTargets+1)
	for index := 0; index < maximumFileSetTargets+1; index++ {
		targets = append(targets, targetMarker{
			Path:   filepath.Join(root, fmt.Sprintf("target-%02d", index)),
			Before: fileState{},
		})
	}
	content, err := marshalMarker(transactionMarker{
		Version: contractversion.MetadataTransaction,
		Targets: targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerFile, content, transactionEvidenceMode); err != nil {
		t.Fatal(err)
	}
	_, err = loadMarker(context.Background(), markerFile)
	if err == nil || !strings.Contains(err.Error(), "targets, maximum") {
		t.Fatalf("loadMarker error = %v, want current-schema cardinality rejection", err)
	}
}

func TestRecoverRejectsCurrentSchemaTargetCardinalityWithoutRestore(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	evidenceDir := transactionDir(stateDir)
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	allowed := make([]string, 0, maximumFileSetTargets+1)
	targets := make([]targetMarker, 0, maximumFileSetTargets+1)
	live := make([]string, 0, maximumFileSetTargets+1)
	for index := 0; index < maximumFileSetTargets+1; index++ {
		path := mustWriteTarget(t, filepath.Join(root, fmt.Sprintf("target-%02d", index)), "after").Path()
		writeFixture(t, path, "after", 0o600)
		allowed = append(allowed, path)
		live = append(live, path)
		targets = append(targets, targetMarker{
			Path: path,
			Before: fileState{
				Exists:     true,
				Hash:       hashBytes([]byte("before")),
				BackupPath: filepath.Join(evidenceDir, fmt.Sprintf("target-%03d.before", index)),
				Mode:       0o600,
			},
			AfterHash: hashBytes([]byte("after")),
			Write:     true,
		})
		writeFixture(t, targets[index].Before.BackupPath, "before", transactionEvidenceMode)
	}
	content, err := marshalMarker(transactionMarker{
		Version: contractversion.MetadataTransaction,
		Targets: targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath(stateDir), content, transactionEvidenceMode); err != nil {
		t.Fatal(err)
	}
	err = RecoverFileSet(t.Context(), stateDir, allowed)
	if err == nil || !strings.Contains(err.Error(), "targets, maximum") {
		t.Fatalf("RecoverFileSet error = %v, want current-schema cardinality rejection", err)
	}
	for _, path := range live {
		assertContent(t, path, "after")
	}
	if _, err := os.Stat(markerPath(stateDir)); err != nil {
		t.Fatalf("over-limit marker was not preserved: %v", err)
	}
}

func TestRecoverPreflightsBackupsBeforeAnyRestore(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	evidenceDir := transactionDir(stateDir)
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	first := mustWriteTarget(t, filepath.Join(root, "first"), "first-after").Path()
	second := mustWriteTarget(t, filepath.Join(root, "second"), "second-after").Path()
	writeFixture(t, first, "first-after", 0o600)
	writeFixture(t, second, "second-before", 0o600)
	firstBackup := filepath.Join(evidenceDir, "target-000.before")
	secondBackup := filepath.Join(evidenceDir, "target-001.before")
	writeFixture(t, firstBackup, "first-before", transactionEvidenceMode)
	writeFixture(t, secondBackup, "tampered-before", transactionEvidenceMode)
	content, err := marshalMarker(transactionMarker{
		Version: contractversion.MetadataTransaction,
		Targets: []targetMarker{
			{
				Path: first,
				Before: fileState{
					Exists:     true,
					Hash:       hashBytes([]byte("first-before")),
					BackupPath: firstBackup,
					Mode:       0o600,
				},
				AfterHash: hashBytes([]byte("first-after")),
				Write:     true,
			},
			{
				Path: second,
				Before: fileState{
					Exists:     true,
					Hash:       hashBytes([]byte("second-before")),
					BackupPath: secondBackup,
					Mode:       0o600,
				},
				AfterHash: hashBytes([]byte("second-after")),
				Write:     true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath(stateDir), content, transactionEvidenceMode); err != nil {
		t.Fatal(err)
	}

	writes := 0
	err = recoverWithOperations(t.Context(), stateDir, []string{first, second}, operations{
		writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
			writes++
			return commitFile(ctx, path, content, mode)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match marker hash") {
		t.Fatalf("Recover error = %v, want backup hash preflight rejection", err)
	}
	if writes != 0 {
		t.Fatalf("restore wrote %d targets before backup preflight failed", writes)
	}
	assertContent(t, first, "first-after")
	assertContent(t, second, "second-before")
	if _, err := os.Stat(markerPath(stateDir)); err != nil {
		t.Fatalf("invalid-backup marker was not preserved: %v", err)
	}
}

func TestPreflightRestorableBackupsRejectsOversizedBackupBeforeRestore(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	evidenceDir := transactionDir(stateDir)
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	firstBackup := filepath.Join(evidenceDir, "target-000.before")
	secondBackup := filepath.Join(evidenceDir, "target-001.before")
	writeFixture(t, firstBackup, "first-before", transactionEvidenceMode)
	oversized, err := os.Create(secondBackup)
	if err != nil {
		t.Fatal(err)
	}
	if err := oversized.Truncate(maximumTargetBytes + 1); err != nil {
		_ = oversized.Close()
		t.Fatal(err)
	}
	if err := oversized.Close(); err != nil {
		t.Fatal(err)
	}
	err = preflightRestorableBackups(t.Context(), transactionMarker{
		Version: contractversion.MetadataTransaction,
		Targets: []targetMarker{
			{
				Path: filepath.Join(root, "first"),
				Before: fileState{
					Exists:     true,
					Hash:       hashBytes([]byte("first-before")),
					BackupPath: firstBackup,
					Mode:       0o600,
				},
			},
			{
				Path: filepath.Join(root, "second"),
				Before: fileState{
					Exists:     true,
					Hash:       hashBytes([]byte("second-before")),
					BackupPath: secondBackup,
					Mode:       0o600,
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "preflight backup") {
		t.Fatalf("preflight error = %v, want oversized backup rejection", err)
	}
}

func TestTransactionTargetByteLimit(t *testing.T) {
	t.Parallel()

	if err := validateTargetContentLength(maximumTargetBytes); err != nil {
		t.Fatalf("exact target limit returned error: %v", err)
	}
	if err := validateTargetContentLength(maximumTargetBytes + 1); err == nil {
		t.Fatal("oversized target length was admitted")
	}
}

func TestFileSetTargetCardinalityLimit(t *testing.T) {
	t.Parallel()

	if err := admitFileSetTargetCount(maximumFileSetTargets); err != nil {
		t.Fatalf("exact target cardinality returned error: %v", err)
	}
	if err := admitFileSetTargetCount(maximumFileSetTargets + 1); err == nil {
		t.Fatal("over-limit target cardinality was admitted")
	}

	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	targets := make([]FileTarget, 0, maximumFileSetTargets+1)
	for index := 0; index < maximumFileSetTargets+1; index++ {
		targets = append(targets, mustWriteTarget(t, filepath.Join(root, fmt.Sprintf("target-%02d", index)), "after"))
	}
	err := CommitFileSet(context.Background(), FileSetInput{
		StateDir: stateDir,
		Targets:  targets,
	})
	if err == nil || !strings.Contains(err.Error(), "targets, maximum") {
		t.Fatalf("Commit error = %v, want target cardinality rejection", err)
	}
	assertMissing(t, transactionDir(stateDir))
}

func TestFileSetTargetCardinalityExactLimitSucceeds(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	targets := make([]FileTarget, 0, maximumFileSetTargets)
	paths := make([]string, 0, maximumFileSetTargets)
	for index := 0; index < maximumFileSetTargets; index++ {
		path := filepath.Join(root, fmt.Sprintf("target-%02d", index))
		paths = append(paths, path)
		targets = append(targets, mustWriteTarget(t, path, fmt.Sprintf("after-%02d", index)))
	}
	if err := CommitFileSet(context.Background(), FileSetInput{
		StateDir: stateDir,
		Targets:  targets,
	}); err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	for index, path := range paths {
		assertContent(t, path, fmt.Sprintf("after-%02d", index))
	}
	assertMissing(t, transactionDir(stateDir))
}

func TestPrepareMarkerRejectsTargetCardinalityBeforeEvidence(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	targets := make([]FileTarget, 0, maximumFileSetTargets+1)
	for index := 0; index < maximumFileSetTargets+1; index++ {
		targets = append(targets, mustRetainedTarget(t, filepath.Join(root, fmt.Sprintf("target-%02d", index))))
	}
	_, err := prepareMarker(context.Background(), stateDir, targets)
	if err == nil || !strings.Contains(err.Error(), "targets, maximum") {
		t.Fatalf("prepareMarker error = %v, want target cardinality rejection", err)
	}
	assertMissing(t, transactionDir(stateDir))
}

func TestAdmitStagedBeforeImageBytes(t *testing.T) {
	t.Parallel()

	if err := admitStagedBeforeImageBytes(0, int(maximumStagedBeforeImageBytes)); err != nil {
		t.Fatalf("exact aggregate before-image limit returned error: %v", err)
	}
	if err := admitStagedBeforeImageBytes(maximumStagedBeforeImageBytes, 0); err != nil {
		t.Fatalf("zero additional at exact aggregate limit returned error: %v", err)
	}
	if err := admitStagedBeforeImageBytes(0, int(maximumStagedBeforeImageBytes)+1); err == nil {
		t.Fatal("aggregate before-image limit plus one was admitted")
	}
	if err := admitStagedBeforeImageBytes(maximumStagedBeforeImageBytes, 1); err == nil {
		t.Fatal("additional byte beyond aggregate before-image limit was admitted")
	}
	if err := admitStagedBeforeImageBytes(maximumStagedBeforeImageBytes-1, 1); err != nil {
		t.Fatalf("final remaining before-image byte returned error: %v", err)
	}
}

func TestPrepareMarkerWritesEachBeforeImageThenMarker(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeFixture(t, first, "first-before", 0o644)
	writeFixture(t, second, "second-before", 0o600)
	targets := []FileTarget{
		mustWriteTarget(t, first, "first-after"),
		mustRetainedTarget(t, second),
	}
	marker, err := prepareMarker(context.Background(), stateDir, targets)
	if err != nil {
		t.Fatalf("prepareMarker returned error: %v", err)
	}
	evidenceDir := transactionDir(stateDir)
	assertContent(t, filepath.Join(evidenceDir, "target-000.before"), "first-before")
	assertContent(t, filepath.Join(evidenceDir, "target-001.before"), "second-before")
	assertMode(t, filepath.Join(evidenceDir, "target-000.before"), transactionEvidenceMode)
	assertMode(t, filepath.Join(evidenceDir, transactionMarkerFile), transactionEvidenceMode)
	if len(marker.Targets) != 2 || !marker.Targets[0].Before.Exists || !marker.Targets[1].Before.Exists {
		t.Fatalf("marker targets = %+v, want two existing before-images", marker.Targets)
	}
	loaded, err := loadMarker(context.Background(), markerPath(stateDir))
	if err != nil {
		t.Fatalf("loadMarker returned error: %v", err)
	}
	if loaded.Targets[0].Before.Hash != hashBytes([]byte("first-before")) ||
		loaded.Targets[1].Before.Hash != hashBytes([]byte("second-before")) {
		t.Fatalf("loaded before hashes = %+v", loaded.Targets)
	}
}

func TestPrepareMarkerCallbackFailureLeavesNoEvidence(t *testing.T) {
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	valid := filepath.Join(root, "valid")
	oversized := filepath.Join(root, "oversized")
	writeFixture(t, valid, "valid-before", 0o600)
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maximumTargetBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = prepareMarker(context.Background(), stateDir, []FileTarget{
		mustRetainedTarget(t, valid),
		mustRetainedTarget(t, oversized),
	})
	if err == nil {
		t.Fatal("prepareMarker admitted an oversized before-image")
	}
	assertMissing(t, transactionDir(stateDir))
}

func TestMarshalMarkerRejectsOutputOverReadLimit(t *testing.T) {
	t.Parallel()

	marker := transactionMarker{Version: contractversion.MetadataTransaction}
	for index := 0; index < 4_096; index++ {
		marker.Targets = append(marker.Targets, targetMarker{
			Path:      filepath.Join(string(filepath.Separator), strings.Repeat("p", 300), fmt.Sprintf("%04d", index)),
			Before:    fileState{},
			AfterHash: hashBytes(nil),
			Write:     true,
		})
	}
	if _, err := marshalMarker(marker); err == nil {
		t.Fatal("marshalMarker admitted output above its read limit")
	}
}

func TestTransactionPhysicalReadsRejectOversizedFiles(t *testing.T) {
	root := t.TempDir()
	oversized := filepath.Join(root, "oversized")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maximumTargetBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	t.Run("capture before-image", func(t *testing.T) {
		_, _, err := captureFileState(t.Context(), oversized, filepath.Join(root, "active.before"))
		if err == nil {
			t.Fatal("captureFileState admitted an oversized target")
		}
	})

	t.Run("classify target", func(t *testing.T) {
		matches, err := fileMatchesExpected(t.Context(), oversized, hashBytes(nil), 0o600)
		if err == nil || matches {
			t.Fatalf("fileMatchesExpected = (%t, %v), want bounded read failure", matches, err)
		}
	})

	t.Run("restore backup", func(t *testing.T) {
		writeCalled := false
		err := restoreFile(t.Context(), filepath.Join(root, "target"), fileState{
			Exists:     true,
			Hash:       hashBytes(nil),
			BackupPath: oversized,
			Mode:       0o600,
		}, operations{writeFile: func(context.Context, string, []byte, os.FileMode) error {
			writeCalled = true
			return nil
		}})
		if err == nil {
			t.Fatal("restoreFile admitted an oversized backup")
		}
		if writeCalled {
			t.Fatal("restoreFile wrote a target after oversized backup rejection")
		}
	})
}

func TestTransactionPhysicalReadAdmitsExactLimit(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "exact")
	writeFixture(t, path, "1234", 0o640)
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	content, mode, err := readTransactionFileUpTo(t.Context(), path, 4)
	if err != nil || string(content) != "1234" || mode.Perm() != 0o640 {
		t.Fatalf("exact transaction read = (%q, %04o, %v)", content, mode.Perm(), err)
	}
	if _, _, err := readTransactionFileUpTo(t.Context(), path, 3); err == nil {
		t.Fatal("transaction read admitted content over its limit")
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

type cancelAfterContext struct {
	context.Context
	calls    *atomic.Int32
	cancelAt int32
}

func (ctx *cancelAfterContext) Err() error {
	if ctx.calls.Add(1) >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}
