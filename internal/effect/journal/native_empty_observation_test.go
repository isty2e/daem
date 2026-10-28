package journal

import (
	"testing"

	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeEmptyObservationRetainsActualContext(t *testing.T) {
	fixture := nativeCaptureInput(t, false, target.ScopeProject)
	reader, err := ownershipstore.NewRecoveryReader(fixture.registry)
	if err != nil {
		t.Fatal(err)
	}
	input := NativeDocumentObservationInput{
		Owner: fixture.batch.owner, Before: fixture.batch.BeforeState(),
		Roots: fixture.batch.Namespaces()[0].roots, Filesystem: fixture.options.Filesystem,
		Registry: reader, Limits: fixture.batch.limits, Budget: fixture.batch.budget,
	}
	observed, err := ObserveNativeDocuments(t.Context(), input)
	if err != nil {
		t.Fatal("an observed empty document census must remain executable context", err)
	}
	if len(observed.Namespaces()) != 0 || !observed.BeforeState().Equal(input.Before) {
		t.Fatal("empty census invented a namespace or discarded unselected native state")
	}
	if err := observed.ValidatePlanningContext(t.Context(), input.Owner, fixture.batch.registry, input.Budget); err != nil {
		t.Fatal("empty census lost its actual owner, Registry or allocation", err)
	}
	if err := observed.validateOriginals(t.Context()); err != nil {
		t.Fatal("empty census lost its independently retained root", err)
	}
	fixture.requireUnchanged(t)

	input.Roots.Manifest = NativeDocumentRoot{}
	if _, err := ObserveNativeDocuments(t.Context(), input); err == nil {
		t.Fatal("empty census accepted absent manifest authority")
	}
	input.Roots.Manifest = nativeNamespaceRoot(t, t.TempDir())
	other, err := ObserveNativeDocuments(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateNativeCaptureContext(t.Context(), fixture.paths, other, fixture.options.CaptureOptions); err == nil {
		t.Fatal("empty census authorized capture under another manifest root")
	}
}
