package journal

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

func TestDocumentImageLoadsActualPrivatePayloadWithIndependentHostMode(t *testing.T) {
	for _, content := range []string{"", "{\"model\":\"fast\"}\n"} {
		for _, hostMode := range []recovery.PermissionMode{0, 0o444, 0o640} {
			document := aggregate.ExistingDocument([]byte(content))
			root, path, reference := imageFixture(t, document, &hostMode)
			budget := imageBudget(t)
			remaining := budget.RemainingBytes()
			reader := &imageReader{RootedReader: journalTestFilesystem()}
			loaded, err := loadDocumentImage(t.Context(), reference, root, reader, budget, 4096)
			if err != nil || zeroLoadedImage(loaded) || !loaded.Document().Equal(document) || loaded.Mode() == nil || *loaded.Mode() != hostMode {
				t.Fatalf("actual private image or host mode was lost: %v", err)
			}
			if reader.calls != 1 || reader.limit != int64(len(content)) || remaining-budget.RemainingBytes() != int64(len(content)) {
				t.Fatal("image read did not use and charge the exact finite payload size")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("host mode was applied to the private evidence: %v", err)
			}
			copy := loaded.Mode()
			*copy = 0o777
			if *loaded.Mode() != hostMode {
				t.Fatal("returned image mode query exposes writable evidence")
			}
		}
	}
	_, _, absent := imageFixture(t, aggregate.AbsentDocument(), nil)
	loaded, err := loadDocumentImage(t.Context(), absent, nil, nil, nil, 4096)
	if err != nil || zeroLoadedImage(loaded) || loaded.Document().Exists() || loaded.Mode() != nil {
		t.Fatalf("explicit absence required a file or became an empty file: %v", err)
	}
}

func TestDocumentImageRefusesCorruptPrivateEvidenceWithoutLoadedValue(t *testing.T) {
	for _, change := range []string{"digest", "short", "growth", "mode", "symlink", "empty growth"} {
		t.Run(change, func(t *testing.T) {
			content := []byte("before")
			if change == "empty growth" {
				content = nil
			}
			root, path, reference := imageFixture(t, aggregate.ExistingDocument(content), recovery.NewPermissionMode(0o600))
			var err error
			switch change {
			case "digest":
				err = os.WriteFile(path, []byte("other!"), 0o600)
			case "short":
				err = os.WriteFile(path, []byte("x"), 0o600)
			case "growth", "empty growth":
				err = os.WriteFile(path, append(content, 'x'), 0o600)
			case "mode":
				err = os.Chmod(path, 0o644)
			case "symlink":
				other := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(other, content, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(other, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			reader := &imageReader{RootedReader: journalTestFilesystem()}
			budget := imageBudget(t)
			remaining := budget.RemainingBytes()
			loaded, err := loadDocumentImage(t.Context(), reference, root, reader, budget, 4096)
			if err == nil || !zeroLoadedImage(loaded) {
				t.Fatal("corrupt private evidence returned a usable image")
			}
			if reader.calls > 1 || (reader.calls != 0 && reader.limit != int64(len(content))) {
				t.Fatal("refusal escaped the exact finite image-read limit")
			}
			charged := int64(len(content))
			if change == "short" {
				charged = 1
			}
			if reader.calls == 1 && remaining-budget.RemainingBytes() != charged {
				t.Fatal("failed bounded image observation did not retain its shared work charge")
			}
		})
	}
}

func TestDocumentImageRefusesBudgetAndCancellationBeforeReading(t *testing.T) {
	root, _, reference := imageFixture(t, aggregate.ExistingDocument([]byte("before")), recovery.NewPermissionMode(0o600))
	budget := imageBudget(t)
	work, err := workmeasure.NewArtifactWork(0, budget.RemainingBytes()-2)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitTree(work); err != nil {
		t.Fatal(err)
	}
	reader := &imageReader{RootedReader: journalTestFilesystem()}
	loaded, err := loadDocumentImage(t.Context(), reference, root, reader, budget, 4096)
	if err == nil || !zeroLoadedImage(loaded) || reader.calls != 0 {
		t.Fatal("image read exceeded the caller's shared remaining budget")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	loaded, err = loadDocumentImage(ctx, reference, root, reader, imageBudget(t), 4096)
	if !errors.Is(err, context.Canceled) || !zeroLoadedImage(loaded) || reader.calls != 0 {
		t.Fatalf("canceled load read or returned an image: %v", err)
	}
}

func TestDocumentImagesReconstructSelectedComparisonAndRejectPartialPairs(t *testing.T) {
	before := aggregate.ExistingDocument([]byte(`{"a":false,"outside":true}`))
	after := aggregate.ExistingDocument([]byte(`{"a":true,"outside":true}`))
	mode := recovery.NewPermissionMode(0o640)
	root, _, original := imageFixture(t, before, mode)
	_, expected := imageReferencesFixture(t, before, after, mode, mode)
	reference, err := expected.FileReference()
	if err != nil {
		t.Fatal(err)
	}
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	expectedPath := filepath.Join(authority.PhysicalRoot(), filepath.FromSlash(reference))
	if err := os.WriteFile(expectedPath, after.Content(), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	remaining := budget.RemainingBytes()
	originalImage, expectedImage, err := loadDocumentImages(t.Context(), original, expected, root, journalTestFilesystem(), budget, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if remaining-budget.RemainingBytes() != before.ContentSize()+after.ContentSize() {
		t.Fatal("original and expected images did not share one operation budget")
	}
	fixture := documentClaimsForTest(t)
	contract := stateGroupContract(t, fixture.alias, "a")
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	codec, err := nativecodec.New(t.Context(), contract.CodecContractID(), limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := compose.NewParticipant(codec, selection, configdocument.JSON, []output.KeyPath{contract.Address().KeyPath()})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := journaldocument.ReconstructSelected(t.Context(), journaldocument.ComparisonInput{Before: originalImage.Document(), Expected: expectedImage.Document(), BeforeMode: originalImage.Mode(), ExpectedMode: expectedImage.Mode(), Limits: limits}, []compose.Participant{participant}, nil)
	if err != nil || candidate.Comparison().Validate() != nil {
		t.Fatalf("actual private images could not reconstruct the selected candidate: %v", err)
	}
	if err := os.WriteFile(expectedPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalImage, expectedImage, err = loadDocumentImages(t.Context(), original, expected, root, journalTestFilesystem(), imageBudget(t), 4096)
	if err == nil || !zeroLoadedImage(originalImage) || !zeroLoadedImage(expectedImage) {
		t.Fatal("a later expected-image failure exposed a successful original image")
	}
}

func TestDocumentImageCancellationAfterReadRetainsWorkWithoutAnImage(t *testing.T) {
	content := []byte("before")
	root, _, reference := imageFixture(t, aggregate.ExistingDocument(content), recovery.NewPermissionMode(0o600))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := &imageReader{RootedReader: journalTestFilesystem(), afterRead: cancel}
	budget := imageBudget(t)
	remaining := budget.RemainingBytes()
	loaded, err := loadDocumentImage(ctx, reference, root, reader, budget, 4096)
	if !errors.Is(err, context.Canceled) || !zeroLoadedImage(loaded) || reader.calls != 1 {
		t.Fatalf("late cancellation exposed an image: %v", err)
	}
	if remaining-budget.RemainingBytes() != int64(len(content)) {
		t.Fatal("late cancellation erased already performed read work")
	}
}

func zeroLoadedImage(image journalrecord.LoadedImage) bool { return reflect.ValueOf(image).IsZero() }

type imageReader struct {
	mutationfs.RootedReader
	calls     int
	limit     int64
	afterRead func()
}

func (reader *imageReader) ReadRootedRegularFileUpTo(ctx context.Context, capability rootedpath.CommitCapability, maximumBytes int64) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	reader.calls++
	reader.limit = maximumBytes
	content, mode, identity, err := reader.RootedReader.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
	if reader.afterRead != nil {
		reader.afterRead()
	}
	return content, mode, identity, err
}

func imageBudget(t *testing.T) *physicalwork.Budget {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func imageReferencesFixture(t *testing.T, before, expected aggregate.Document, beforeMode, expectedMode *recovery.PermissionMode) (journalrecord.DocumentImage, journalrecord.DocumentImage) {
	t.Helper()
	comparison, err := journaldocument.NewWholeComparison(t.Context(), journaldocument.ComparisonInput{Before: before, Expected: expected, BeforeMode: beforeMode, ExpectedMode: expectedMode, Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}})
	if err != nil {
		t.Fatal(err)
	}
	_, original, after, err := journalrecord.StagedDocumentImageReferences(comparison, 0)
	if err != nil {
		t.Fatal(err)
	}
	return original, after
}

func imageFixture(t *testing.T, document aggregate.Document, mode *recovery.PermissionMode) (*rootedpath.CapturedRoot, string, journalrecord.DocumentImage) {
	t.Helper()
	original, _ := imageReferencesFixture(t, document, document, mode, mode)
	if !document.Exists() {
		return nil, "", original
	}
	reference, err := original.FileReference()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, filepath.FromSlash(reference))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, document.Content(), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, path, original
}
