package transaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestLoadMarkerRejectsNonCanonicalAndAbsentAuthorityFields(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "value")
	valid := fmt.Sprintf(
		`{"version":2,"targets":[{"path":%q,"before":{"exists":false},"write":true,"after_hash":"sha256:%s"}]}`,
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
		{name: "case-folded duplicate", content: strings.Replace(valid, `"version":2`, `"version":2,"Version":2`, 1), want: "ASCII lower_snake_case"},
		{name: "unknown field", content: strings.Replace(valid, `"version":2`, `"version":2,"unknown":true`, 1), want: "unknown field"},
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
		{name: "future version", content: strings.Replace(valid, `"version":2`, `"version":3`, 1), want: "written by a newer daem"},
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

func TestLoadMarkerAcceptsLegacyVersion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "value")
	content := fmt.Sprintf(
		`{"version":1,"targets":[{"path":%q,"before":{"exists":false},"write":true,"after_hash":"sha256:%s"}]}`,
		path,
		strings.Repeat("a", 64),
	)
	markerPath := filepath.Join(t.TempDir(), transactionMarkerFile)
	if err := os.WriteFile(markerPath, []byte(content), transactionEvidenceMode); err != nil {
		t.Fatal(err)
	}
	marker, err := loadMarker(context.Background(), markerPath)
	if err != nil {
		t.Fatalf("loadMarker legacy version returned error: %v", err)
	}
	if marker.Version != 1 {
		t.Fatalf("marker version = %d, want legacy version 1", marker.Version)
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

func TestMarshalMarkerRejectsOutputOverReadLimit(t *testing.T) {
	t.Parallel()

	marker := transactionMarker{Version: transactionVersion}
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
		staged := filepath.Join(root, "staged.before")
		_, err := captureFileState(t.Context(), oversized, staged, filepath.Join(root, "active.before"))
		if err == nil {
			t.Fatal("captureFileState admitted an oversized target")
		}
		assertMissing(t, staged)
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
