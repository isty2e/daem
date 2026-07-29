package lock_test

import (
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildDeltaDetectsOrderOnlyChange(t *testing.T) {
	first := lockedOrderExtension(
		t,
		"first",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		"first",
	)
	second := lockedOrderExtension(
		t,
		"second",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		"second",
	)
	before := lockedOrderFile(t, []desiredextension.Extension{first, second})
	after := lockedOrderFile(t, []desiredextension.Extension{second, first})

	delta := lock.BuildDelta(before, after)
	if !delta.HasChanges() {
		t.Fatal("HasChanges = false for order-only change")
	}
	if got := delta.Counts(); got.Unchanged != 2 ||
		got.Added != 0 ||
		got.Changed != 0 ||
		got.Removed != 0 {
		t.Fatalf("subject counts = %#v, want two unchanged", got)
	}
	if got := delta.OrderCounts(); got.Changed != 1 ||
		got.Added != 0 ||
		got.Removed != 0 ||
		got.Unchanged != 0 {
		t.Fatalf("order counts = %#v, want one changed", got)
	}
	entry := delta.OrderEntries()[0]
	if entry.Before.Members()[0].Subject().Key() != "first" ||
		entry.After.Members()[0].Subject().Key() != "second" {
		t.Fatalf("order delta = %#v", entry)
	}
}

func lockedOrderFile(
	t *testing.T,
	extensions []desiredextension.Extension,
) lock.File {
	t.Helper()
	subjects, constraints := lockedOrderFixture(t, extensions)
	section, err := lock.NewLockedSection(subjects, constraints)
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	return lock.File{Version: lock.CurrentVersion, Locked: section}
}
