package publication

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	"github.com/isty2e/daem/internal/output"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

type nativeInitialLaterReadFailure struct {
	mutationfs.RootedReader
	reads        int
	err          error
	capabilities []rootedpath.CommitCapability
}

func (reader *nativeInitialLaterReadFailure) ReadRootedRegularFileUpTo(ctx context.Context, capability rootedpath.CommitCapability, limit int64) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	reader.reads++
	reader.capabilities = append(reader.capabilities, capability)
	if reader.reads == 2 {
		return nil, 0, nil, reader.err
	}
	return reader.RootedReader.ReadRootedRegularFileUpTo(ctx, capability, limit)
}

func TestNativeInitialObservationDiscardsEarlierOriginalOnLaterReadFailure(t *testing.T) {
	fixture, input, registry, _ := nativeInitialFixture(t, target.ScopeProject, "record")
	second, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeProject, outputtest.Parse(t, ".pi/other.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(namespacetest.LegacyRoot(t, input.Roots, output.RootProject).SelectedPath, filepath.FromSlash(second.AggregateRoot().RelativePath()))
	requireWholeGroupWrite(t, path, []byte("second"), 0o644)
	input.Aliases = append(input.Aliases, second)
	sentinel := errors.New("later original read failed")
	reader := &nativeInitialLaterReadFailure{RootedReader: input.Filesystem, err: sentinel}
	input.Filesystem = reader
	observed, err := ObserveNativeDocuments(t.Context(), input)
	if !errors.Is(err, sentinel) || observed.Evidence().Validate() == nil || len(observed.Evidence().Namespaces()) != 0 || registry.loads != 1 || reader.reads != 2 {
		t.Fatal("later read failure exposed an earlier observed original", err)
	}
	for _, capability := range reader.capabilities {
		if capability.Validate() == nil {
			t.Fatal("failed original read leaked an acquired capability")
		}
	}
	fixture.requireUnchangedHost(t)
}
