package publication

import (
	"testing"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	"github.com/isty2e/daem/internal/effect/physicalwork"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeEmptyObservationRetainsActualContext(t *testing.T) {
	fixture := nativeCaptureInput(t, false, target.ScopeProject)
	reader, err := ownershipstore.NewRecoveryReader(fixture.registry)
	if err != nil {
		t.Fatal(err)
	}
	input := journalobservation.Input{
		Owner: fixture.batch.owner, Before: fixture.batch.Evidence().BeforeState(),
		Roots: fixture.batch.roots, Filesystem: fixture.options.Filesystem,
		Registry: reader, Limits: fixture.batch.Evidence().DocumentLimits(), Budget: fixture.batch.budget,
	}
	observed, err := ObserveNativeDocuments(t.Context(), input)
	if err != nil {
		t.Fatal("an observed empty document census must remain executable context", err)
	}
	if len(observed.Evidence().Namespaces()) != 0 || !observed.Evidence().BeforeState().Equal(input.Before) {
		t.Fatal("empty census invented a namespace or discarded unselected native state")
	}
	current, err := reader.LoadForClaimRemovals(t.Context(), nil, physicalwork.MaximumPhysicalPathDepth, input.Budget)
	if err != nil {
		t.Fatal(err)
	}
	if err := observed.Evidence().ValidatePlanningContext(t.Context(), input.Owner, current, input.Budget); err != nil {
		t.Fatal("empty census lost its actual owner, Registry or allocation", err)
	}
	if err := observed.validateOriginals(t.Context()); err != nil {
		t.Fatal("empty census lost its independently retained root", err)
	}
	fixture.requireUnchanged(t)

	input.Roots.Manifest = journalnamespace.RootSelection{}
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
