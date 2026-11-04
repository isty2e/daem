package fileset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestFileSet4MissingImagesRefuseRecoveryWithoutChangingEvidence(t *testing.T) {
	tests := []struct {
		name        string
		reference   bool
		afterImage  bool
		beforeIndex int
	}{
		{name: "reference after-image", reference: true, afterImage: true},
		{name: "ordinary before-image", reference: true},
		{name: "commit-point before-image", reference: true, beforeIndex: 1},
		{name: "static before-image"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
			source := filepath.Join(root, "source")
			manifest := filepath.Join(root, "manifest")
			writeFixture(t, source, "source-before", 0o600)
			writeFixture(t, manifest, "manifest-before", 0o600)
			marker, err := prepareMarkerVersion(t.Context(), stateDir, []FileTarget{
				mustWriteTarget(t, source, "source-after"),
				mustCommitPointTarget(t, manifest, "manifest-after"),
			}, contractversion.MetadataTransactionReference, test.reference)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, source, "source-after", 0o600)
			markerBytes, err := os.ReadFile(markerPath(stateDir))
			if err != nil {
				t.Fatal(err)
			}

			missingImage := marker.Targets[test.beforeIndex].Before.BackupPath
			if test.afterImage {
				missingImage = filepath.Join(transactionDir(stateDir), referenceAfterImageName)
			}
			if err := os.Remove(missingImage); err != nil {
				t.Fatal(err)
			}

			err = RecoverFileSet4(t.Context(), stateDir, []string{source, manifest})
			var missing *os.PathError
			if !errors.As(err, &missing) || missing.Path != missingImage || !errors.Is(err, os.ErrNotExist) {
				t.Errorf("RecoverFileSet4 error = %v, want missing-image error for %q", err, missingImage)
			}
			fenceErr := ObserveClearFenceAt4(t.Context(), stateDir, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{})
			if !errors.Is(fenceErr, ErrFileSetEvidenceInvalid) || !errors.As(fenceErr, &missing) || missing.Path != missingImage {
				t.Errorf("ObserveClearFenceAt4 error = %v, want invalid evidence identifying %q", fenceErr, missingImage)
			}

			assertContent(t, source, "source-after")
			assertContent(t, manifest, "manifest-before")
			assertContent(t, markerPath(stateDir), string(markerBytes))
			assertMissing(t, missingImage)
		})
	}
}

func TestRecoverMissingMarkerKeepsCleanStateUnchanged(t *testing.T) {
	tests := []struct {
		name    string
		recover func(context.Context, string, []string) error
	}{
		{name: "generation 3", recover: RecoverFileSet},
		{name: "generation 4", recover: RecoverFileSet4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
			target := filepath.Join(root, "target")
			writeFixture(t, target, "unchanged", 0o600)

			if err := test.recover(t.Context(), stateDir, []string{target}); err != nil {
				t.Fatalf("recovery without a transaction failed: %v", err)
			}
			assertContent(t, target, "unchanged")
			assertMissing(t, transactionDir(stateDir))
		})
	}
}
