package fileset

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
)

func TestFreshReferenceCommitRefusesPendingRecoveryWithoutRetry(t *testing.T) {
	for _, finalized := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "finalize"}[finalized], func(t *testing.T) {
			_, stateDir, source, manifest := interruptedReferenceFixture(t, false)
			evidence := transactionDir(stateDir)
			if finalized {
				writeFixture(t, manifest, "manifest-after", 0o644)
			}
			before, err := os.ReadFile(markerPath(stateDir))
			if err != nil {
				t.Fatal(err)
			}
			err = CommitFreshFileSet4WithReference(t.Context(), FileSetInput{
				StateDir: stateDir,
				Targets: []FileTarget{
					mustCommitPointTarget(t, manifest, "recaptured-manifest"),
					mustWriteTarget(t, source, "recaptured-source"),
				},
			})
			if !errors.Is(err, ErrInterruptedFileSetTransaction) {
				t.Fatalf("fresh capture did not refuse pending recovery: %v", err)
			}
			assertContent(t, source, "source-after")
			wantManifest := "manifest-before"
			if finalized {
				wantManifest = "manifest-after"
			}
			assertContent(t, manifest, wantManifest)
			after, err := os.ReadFile(markerPath(stateDir))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("refused commit recovered or changed the pending marker", err)
			}
			if _, err := os.Stat(evidence); err != nil {
				t.Fatal("refused commit discarded recovery evidence", err)
			}
		})
	}
}

func TestReferenceSourcePublishesBeforeManifestAndReturnedFailureRestoresBoth(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "z-source")
	manifest := filepath.Join(root, "a-manifest")
	stateDir := filepath.Join(root, ".daem")
	writeFixture(t, source, "source-before", 0o640)
	writeFixture(t, manifest, "manifest-before", 0o644)
	sourceTarget := mustWriteTarget(t, source, "source-after")
	manifestTarget := mustCommitPointTarget(t, manifest, "manifest-after")
	source, manifest = sourceTarget.Path(), manifestTarget.Path()
	failure := errors.New("manifest publication failed")
	var writes []string
	err := commitWithOperationsVersion(t.Context(), FileSetInput{
		StateDir: stateDir,
		Targets: []FileTarget{
			manifestTarget,
			sourceTarget,
		},
	}, operations{writeFile: func(ctx context.Context, path string, content []byte, mode os.FileMode) error {
		writes = append(writes, path)
		if path == manifest && string(content) == "manifest-after" {
			return failure
		}
		return commitFile(ctx, path, content, mode)
	}}, contractversion.MetadataTransactionReference, true)
	if !errors.Is(err, failure) {
		t.Fatalf("returned failure lost its cause: %v", err)
	}
	if len(writes) < 2 || !slices.Equal(writes[:2], []string{source, manifest}) {
		t.Fatal("manifest published before source", writes)
	}
	assertContent(t, source, "source-before")
	assertContent(t, manifest, "manifest-before")
	for path, mode := range map[string]os.FileMode{source: 0o640, manifest: 0o644} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatal("rollback changed file permissions", path, err)
		}
	}
}
