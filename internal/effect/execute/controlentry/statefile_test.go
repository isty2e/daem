package controlentry

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestBindStatefileEntryRetainsSelectedAllocationAndCustody(t *testing.T) {
	fixture := newSelectedControlFixture(t)
	selected, err := BindStatefileEntry(fixture.root, fixture.path, filepath.Join(fixture.path, "state.json"), storagecommit.Adapter{}, fixture.phase, fixture.allocation)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = selected.Close() })
	state, err := selected.Read(t.Context(), statefile.DefaultPhysicalCodec(), fixture.allocation)
	if err != nil || !state.Equal(durable.EmptySnapshot()) {
		t.Fatalf("statefile factory changed selected read behavior: %v", err)
	}
	if err := selected.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.root.ValidateSelectionBounded(fixture.path, physicalwork.MaximumPhysicalPathDepth, fixture.phase); err != nil {
		t.Fatalf("selected cleanup closed the caller's root: %v", err)
	}
}

func TestControlEntryBindingRejectsTypedNilPhase(t *testing.T) {
	fixture := newSelectedControlFixture(t)
	var phase *physicalwork.TraversalPhase
	selected, err := BindStatefileEntry(fixture.root, fixture.path, filepath.Join(fixture.path, "state.json"), storagecommit.Adapter{}, phase, fixture.allocation)
	if selected != nil || err == nil || err.Error() != "bind project statefile: project control traversal authority is unavailable" {
		t.Fatalf("statefile nil-phase admission changed: %v", err)
	}
	var entries JournalEntries
	err = entries.Bind(fixture.root, fixture.path, fixture.path, phase)
	if err == nil || err.Error() != "bind recovery journal: project control traversal authority is unavailable" {
		t.Fatalf("journal nil-phase admission changed: %v", err)
	}
	if entries.Directory() != nil || entries.Record() != nil {
		t.Fatal("unavailable phase acquired a journal pair")
	}
}
