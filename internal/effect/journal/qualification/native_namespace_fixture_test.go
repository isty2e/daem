package qualification_test

import (
	"context"
	"io/fs"
	"os"
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

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
