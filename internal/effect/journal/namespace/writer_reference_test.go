package namespace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestWriterReferenceDoesNotCollapseCoincidentDirectoryGrants(t *testing.T) {
	base := t.TempDir()
	referent := filepath.Join(base, "document.json")
	if err := os.WriteFile(referent, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	selection, err := CaptureRootSelector(t.Context(), base, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = selection.Close() })
	first, err := output.NewRootReference("first-directory", "directory-fixture-v1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := output.NewRootReference("second-directory", "directory-fixture-v1")
	if err != nil {
		t.Fatal(err)
	}
	roots, err := (Roots{}).With(first, selection)
	if err != nil {
		t.Fatal(err)
	}
	roots, err = roots.With(second, selection)
	if err != nil {
		t.Fatal(err)
	}
	qualified, err := roots.Qualify(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	referent, err = rootedpath.ResolveReferentPathBounded(referent, 64, budget)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(referent, 64, budget)
	if err != nil {
		t.Fatal(err)
	}
	provenance := qualified.grants[first].Provenance()
	if qualified.grants[second].Provenance() != provenance {
		t.Fatal("fixture roots do not share one physical provenance")
	}
	relative, err := rootedpath.NewRelativeDestination("document.json")
	if err != nil {
		t.Fatal(err)
	}
	coordinate, err := configuration.NewDirectoryChild(relative.Path())
	if err != nil {
		t.Fatal(err)
	}
	bound, err := NewBoundWriterPin(second, provenance, coordinate)
	if err != nil {
		t.Fatal(err)
	}
	grant, _, err := nativeDocumentWriterGrant(qualified.grants, nil, referent, observation, &bound)
	if err != nil || grant.RootReference() != second {
		t.Fatal("a referenced writer pin selected another coincident directory", grant.RootReference(), err)
	}
	onlyFirst := map[output.RootReference]RootGrant{first: qualified.grants[first]}
	if _, _, err := nativeDocumentWriterGrant(onlyFirst, nil, referent, observation, &bound); !errors.Is(err, ErrNoTrustedWriterRoot) {
		t.Fatal("a missing reference fell back to equal physical provenance", err)
	}
	newPolicy, err := output.NewRootReference(first.Identity(), "directory-fixture-v2")
	if err != nil {
		t.Fatal(err)
	}
	versioned, err := NewBoundWriterPin(newPolicy, provenance, coordinate)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := nativeDocumentWriterGrant(onlyFirst, nil, referent, observation, &versioned); !errors.Is(err, ErrNoTrustedWriterRoot) {
		t.Fatal("a pin matched the root identity but lost its locator revision", err)
	}
	legacy, err := NewWriterPin(provenance, relative)
	if err != nil {
		t.Fatal(err)
	}
	if _, referenced := legacy.RootReference(); referenced {
		t.Fatal("legacy provenance-only evidence acquired an inferred reference")
	}
	if _, _, err := nativeDocumentWriterGrant(onlyFirst, nil, referent, observation, &legacy); err != nil {
		t.Fatal("legacy provenance-only pin semantics changed", err)
	}
	if pin, err := NewBoundWriterPin(output.RootReference{}, provenance, coordinate); err == nil || pin != (WriterPin{}) {
		t.Fatal("a referenced pin retained partial identity")
	}
}

func TestDirectoryWriterPreservesItsPhysicalChildSpelling(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, " target.json")
	if err := os.WriteFile(physical, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, filepath.Join(base, "point.json")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	selected := nativeNamespaceRoot(t, base)
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootPiAgent: selected})
	address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/point.json")
	value, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, fixtureProtection(t), journalTestFilesystem(), imageBudget(t))
	if err != nil {
		t.Fatal("a physical writer child was treated as portable declaration text", err)
	}
	child, directory := value.WriterPin().Coordinate().DirectoryChild()
	if !directory || child != " target.json" {
		t.Fatal("writer spelling was trimmed or replaced by its logical alias", child)
	}
	_, destination, err := value.WriterBinding(t.Context(), imageBudget(t))
	if err != nil || destination.Relative().Path() != child {
		t.Fatal("writer requalification changed the physical child spelling", err)
	}
}
