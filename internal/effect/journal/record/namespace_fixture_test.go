package record

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

type observedRecordFixture struct {
	nativeRecordFixture
	observation mutation.DirectoryEntryAuthorityObservation
	provenance  rootedpath.AuthorityProvenance
	relative    rootedpath.RelativeDestination
}

func observedNativeRecordFixture(t *testing.T) observedRecordFixture {
	t.Helper()
	fixture := nativeJournalFixture(t)
	parent := t.TempDir()
	path := filepath.Join(parent, "custom.json")
	if err := os.WriteFile(path, fixture.original.Content(), 0o600); err != nil {
		t.Fatal(err)
	}
	observation, err := mutation.ObserveDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	exact, present := observation.Exact()
	if !present {
		t.Fatal("fixture requires a real exact observation")
	}
	provenance, err := rootedpath.NewAuthorityProvenance(parent, testContentHashString("observed object"), testContentHashString("observed mount"))
	if err != nil {
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination("custom.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture.claims.exact = exact
	fixture.record.ManifestRootProvenance = persistedNativeRoot(provenance)
	group := &fixture.record.DocumentGroups[0]
	group.Writer = persistedNativeWriter(provenance, relative)
	group.CapturedTarget, err = persistedNativeObservation(observation)
	if err != nil {
		t.Fatal(err)
	}
	for index := range group.Aliases {
		group.Aliases[index].LogicalRoot.Provenance = persistedNativeRoot(provenance)
		group.Aliases[index].SelectedObservation = group.CapturedTarget
		group.Aliases[index].ObservedBefore = group.CapturedTarget
	}
	fixture.withSelectedRows(t, fixture.before.ManagedAggregates()[0], fixture.after.ManagedAggregates()[0])
	fixture.withImages(t, fixture.original, fixture.expected)
	return observedRecordFixture{nativeRecordFixture: fixture, observation: observation, provenance: provenance, relative: relative}
}

func captureRecordTestRoot(t *testing.T, path string) *rootedpath.CapturedRoot {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
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
