package publication

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
)

func journalTestFilesystem() mutationfs.Store { return storagecommit.Adapter{} }

func journalTestCodecs() aggregate.CodecCatalog { return aggregatecodec.Catalog() }

func imageBudget(t *testing.T) *physicalwork.Budget {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func nativeNamespaceRoot(t *testing.T, path string) journalnamespace.RootSelection {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return journalnamespace.RootSelection{SelectedPath: path, Root: root}
}

func nativeNamespaceAddress(t *testing.T, selected target.Target, scope target.Scope, text string) aggregate.DocumentAddress {
	t.Helper()
	destination, err := output.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	address, err := aggregate.NewDocumentAddress(selected, scope, destination)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

type nativeNamespaceReader struct {
	mutationfs.RootedReader
	capabilities  []rootedpath.CommitCapability
	afterIdentity func()
	imageReads    int
}

func (reader *nativeNamespaceReader) CaptureRootedEntryIdentity(ctx context.Context, capability rootedpath.CommitCapability) (mutationfs.EntryIdentity, error) {
	reader.capabilities = append(reader.capabilities, capability)
	identity, err := reader.RootedReader.CaptureRootedEntryIdentity(ctx, capability)
	if reader.afterIdentity != nil {
		reader.afterIdentity()
	}
	return identity, err
}

func (reader *nativeNamespaceReader) ReadRootedRegularFileUpTo(ctx context.Context, capability rootedpath.CommitCapability, maximumBytes int64) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	reader.imageReads++
	return reader.RootedReader.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
}

func nativeRecordJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func nativeRecordCanonical(t *testing.T, raw string) string {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	document, err := configdocument.Parse(t.Context(), []byte(`{"value":`+raw+`}`), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("value")
	if err != nil {
		t.Fatal(err)
	}
	value, found, err := document.Lookup(key)
	if err != nil || !found {
		t.Fatalf("canonical fixture value: %v", err)
	}
	canonical, err := value.CanonicalContext(t.Context(), limits)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func preparedNativeFixture(t *testing.T) (nativeRecordFixture, journalrecord.Native, journaldocument.Comparison) {
	t.Helper()
	fixture := nativeJournalFixture(t)
	return fixture, fixture.journal, fixture.comparison
}

func assertNoPreparedDocumentRecord(t *testing.T, value journalrecord.PreparedDocuments, err error, codec durable.SnapshotCodec) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid preparation returned no error")
	}
	if comparisons, err := value.Comparisons(); err == nil || comparisons != nil {
		t.Fatal("invalid preparation supplied complete or partial staging comparisons")
	}
	if encoded, err := value.Encode(t.Context(), codec); err == nil || encoded != nil {
		t.Fatal("invalid preparation produced an encodable record")
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
