package journal

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

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
			root, path := imageFixture(t, []byte(content))
			record, err := persistedDocumentImage(aggregate.ExistingDocument([]byte(content)), &hostMode, "images/before")
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			var decoded recoveryDocumentImage
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			budget := imageBudget(t)
			remaining := budget.RemainingBytes()
			reader := &imageReader{RootedReader: journalTestFilesystem()}
			loaded, err := loadDocumentImage(t.Context(), decoded, root, reader, budget, 4096)
			if err != nil || loaded.validate() != nil || !loaded.document.Equal(aggregate.ExistingDocument([]byte(content))) || loaded.mode == nil || *loaded.mode != hostMode {
				t.Fatalf("actual private image or host mode was lost: %v", err)
			}
			if reader.calls != 1 || reader.limit != int64(len(content)) || remaining-budget.RemainingBytes() != int64(len(content)) {
				t.Fatal("image read did not use and charge the exact finite payload size")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("host mode was applied to the private evidence: %v", err)
			}
			previous := *loaded.mode
			*record.Mode = 0o777
			*decoded.Mode = 0o777
			if *loaded.mode != previous {
				t.Fatal("returned image mode aliases the wire input")
			}
		}
	}
	absent, err := persistedDocumentImage(aggregate.AbsentDocument(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadDocumentImage(t.Context(), absent, nil, nil, nil, 4096)
	if err != nil || loaded.document.Exists() || loaded.validate() != nil || loaded.mode != nil {
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
			root, path := imageFixture(t, content)
			mode := recovery.PermissionMode(0o600)
			record, err := persistedDocumentImage(aggregate.ExistingDocument(content), &mode, "images/before")
			if err != nil {
				t.Fatal(err)
			}
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
			loaded, err := loadDocumentImage(t.Context(), record, root, reader, budget, 4096)
			if err == nil || loaded.validate() == nil || loaded.mode != nil {
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
	root, _ := imageFixture(t, []byte("before"))
	mode := recovery.PermissionMode(0o600)
	record, err := persistedDocumentImage(aggregate.ExistingDocument([]byte("before")), &mode, "images/before")
	if err != nil {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	work, err := workmeasure.NewArtifactWork(0, budget.RemainingBytes()-2)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitTree(work); err != nil {
		t.Fatal(err)
	}
	reader := &imageReader{RootedReader: journalTestFilesystem()}
	loaded, err := loadDocumentImage(t.Context(), record, root, reader, budget, 4096)
	if err == nil || loaded.validate() == nil || reader.calls != 0 {
		t.Fatal("image read exceeded the caller's shared remaining budget")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	loaded, err = loadDocumentImage(ctx, record, root, reader, imageBudget(t), 4096)
	if !errors.Is(err, context.Canceled) || loaded.validate() == nil || reader.calls != 0 {
		t.Fatalf("canceled load read or returned an image: %v", err)
	}
}

func TestDocumentImagesReconstructSelectedComparisonAndRejectPartialPairs(t *testing.T) {
	before := []byte(`{"a":false,"outside":true}`)
	after := []byte(`{"a":true,"outside":true}`)
	root, originalPath := imageFixture(t, before)
	expectedPath := filepath.Join(filepath.Dir(originalPath), "expected")
	if err := os.WriteFile(expectedPath, after, 0o600); err != nil {
		t.Fatal(err)
	}
	mode := recovery.PermissionMode(0o640)
	original, err := persistedDocumentImage(aggregate.ExistingDocument(before), &mode, "images/before")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := persistedDocumentImage(aggregate.ExistingDocument(after), &mode, "images/expected")
	if err != nil {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	remaining := budget.RemainingBytes()
	images, err := loadDocumentImages(t.Context(), original, expected, root, journalTestFilesystem(), budget, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if remaining-budget.RemainingBytes() != int64(len(before)+len(after)) {
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
	candidate, err := journaldocument.ReconstructSelected(t.Context(), journaldocument.ComparisonInput{
		Before: images.original.document, Expected: images.expected.document,
		BeforeMode: images.original.mode, ExpectedMode: images.expected.mode,
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	}, []compose.Participant{participant}, nil)
	if err != nil || candidate.Comparison().Validate() != nil {
		t.Fatalf("actual private images could not reconstruct the selected candidate: %v", err)
	}
	if err := os.WriteFile(expectedPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	images, err = loadDocumentImages(t.Context(), original, expected, root, journalTestFilesystem(), imageBudget(t), 4096)
	if err == nil || images.original.validate() == nil || images.expected.validate() == nil {
		t.Fatal("a later expected-image failure exposed a successful original image")
	}
}

func TestDocumentImageCancellationAfterReadRetainsWorkWithoutAnImage(t *testing.T) {
	content := []byte("before")
	root, _ := imageFixture(t, content)
	mode := recovery.PermissionMode(0o600)
	record, err := persistedDocumentImage(aggregate.ExistingDocument(content), &mode, "images/before")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := &imageReader{RootedReader: journalTestFilesystem(), afterRead: cancel}
	budget := imageBudget(t)
	remaining := budget.RemainingBytes()
	loaded, err := loadDocumentImage(ctx, record, root, reader, budget, 4096)
	if !errors.Is(err, context.Canceled) || loaded.validate() == nil || reader.calls != 1 {
		t.Fatalf("late cancellation exposed an image: %v", err)
	}
	if remaining-budget.RemainingBytes() != int64(len(content)) {
		t.Fatal("late cancellation erased already performed read work")
	}
}

func TestDocumentImageWireRejectsMissingMixedAndDuplicateEvidence(t *testing.T) {
	for _, content := range []string{
		`{}`, `null`, `{"kind":"absent","mode":null}`, `{"kind":"absent","mode":0}`,
		`{"kind":"absent","artifact":null}`, `{"kind":"absent","extra":true}`,
		`{"kind":"absent","Mode":null}`, `{"kind":"absent","Kind":"absent"}`,
		`{"kind":"absent","kind":"file"}`, `{"kind":"file","mode":0}`,
		`{"kind":"file","mode":0,"artifact":{"path":"images/before","digest":"sha256:00"}}`,
	} {
		var decoded recoveryDocumentImage
		if err := json.Unmarshal([]byte(content), &decoded); err == nil {
			t.Fatalf("invalid closed image record accepted: %s", content)
		}
	}
	mode := recovery.PermissionMode(0o600)
	for _, path := range []string{"", "../outside", "/outside", "images/../before"} {
		if _, err := persistedDocumentImage(aggregate.ExistingDocument(nil), &mode, path); err == nil {
			t.Fatal("image accepted an escaped or noncanonical artifact reference")
		}
	}
	if _, err := persistedDocumentImage(aggregate.AbsentDocument(), nil, "images/before"); err == nil {
		t.Fatal("absent image retained an artifact")
	}
}

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

func imageFixture(t *testing.T, content []byte) (*rootedpath.CapturedRoot, string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "images", "before")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, path
}
