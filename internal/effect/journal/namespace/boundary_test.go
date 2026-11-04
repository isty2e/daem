package namespace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestWriterPinRetainsOnlyItsObservedSelectedRoot(t *testing.T) {
	root := nativeNamespaceRoot(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(root.SelectedPath, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/settings.json")
	roots := Roots{Manifest: root}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	budget := imageBudget(t)
	original, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, reader, budget)
	if err != nil {
		t.Fatal(err)
	}
	pin := original.WriterPin()
	pin, err = NewWriterPin(pin.Provenance(), pin.Relative())
	if err != nil {
		t.Fatal(err)
	}
	current, err := Reobserve(t.Context(), original.Addresses(), roots, reader, budget, pin)
	if err != nil || !original.Observation().Equal(current.Observation()) {
		t.Fatalf("canonical pin lost the selected namespace: %v", err)
	}
	aliases := current.Aliases()
	aliases[0] = Alias{}
	if current.Aliases()[0].Address() != alias {
		t.Fatal("namespace exposed its mutable alias slice")
	}
	qualified, err := current.QualifyRoots(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	manifest, present := qualified.Manifest()
	if !present || manifest.Provenance() != pin.Provenance() || manifest.SelectedPath() != root.SelectedPath {
		t.Fatal("namespace lost its original root selection")
	}
	observation, err := manifest.ObserveRoute(t.Context(), alias.AggregateRoot(), budget)
	if err != nil {
		t.Fatal(err)
	}
	if !observation.Equal(original.Observation()) {
		t.Fatal("route query observed a different writer path")
	}
	other := nativeNamespaceRoot(t, t.TempDir())
	authority, err := other.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := NewWriterPin(provenance, pin.Relative())
	if err != nil {
		t.Fatal(err)
	}
	if value, err := Reobserve(t.Context(), original.Addresses(), roots, reader, budget, foreign); err == nil || value.Observation().Validate() == nil || len(value.Addresses()) != 0 {
		t.Fatal("writer pin introduced a root not selected by the caller")
	}
}

func TestZeroCustodyValuesCannotAuthorizeWork(t *testing.T) {
	budget := imageBudget(t)
	var namespace DocumentNamespace
	if _, err := namespace.QualifyRoots(t.Context(), budget); err == nil {
		t.Fatal("zero namespace supplied qualified roots")
	}
	if value, err := namespace.AcquireReferent(t.Context(), budget); err == nil || value != nil {
		t.Fatal("zero namespace supplied a referent capability")
	}
	if _, err := namespace.ObserveRemovalNamespace(t.Context(), recovery.RemovalNamespaceAuthority{}, budget); err == nil {
		t.Fatal("zero namespace observed removal authority")
	}
	if _, err := namespace.WriterPath(); err == nil {
		t.Fatal("zero namespace supplied a writer path")
	}
	if _, present := (QualifiedRoots{}).Manifest(); present {
		t.Fatal("zero roots supplied a manifest grant")
	}
	if value, err := (RootGrant{}).ObserveRoute(t.Context(), nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/settings.json").AggregateRoot(), budget); err == nil || value.Validate() == nil {
		t.Fatal("zero grant supplied a route observation")
	}
	if _, err := NewWriterPin(rootedpath.AuthorityProvenance{}, rootedpath.RelativeDestination{}); err == nil {
		t.Fatal("zero writer pin was admitted")
	}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	if value, err := Reobserve(t.Context(), nil, Roots{}, reader, budget, WriterPin{}); err == nil || value.Observation().Validate() == nil || len(reader.capabilities) != 0 {
		t.Fatal("zero pin reached namespace observation")
	}
}
