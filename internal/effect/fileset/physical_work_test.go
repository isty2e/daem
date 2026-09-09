package fileset

import (
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestFenceObservationBudgetRetainsIndependentLimits(t *testing.T) {
	budget := &fenceObservationBudget{}
	if err := budget.AdmitPhysicalWork(mutationfs.MaximumPhysicalPathComponentVisits-1, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitPhysicalWork(2, 1, 1); err == nil {
		t.Fatal("accepted excessive aggregate path work")
	}
	if budget.entries != 0 || budget.bytes != 0 {
		t.Fatal("rejected path work consumed entry or byte capacity")
	}
	if err := budget.AdmitPhysicalWork(1, maximumFileSetOperationEntries, maximumFileSetOperationBytes); err != nil {
		t.Fatalf("admit exact remaining independent capacity: %v", err)
	}
	for _, work := range []fileSetPhysicalWork{
		{pathComponents: 1},
		{entries: 1},
		{bytes: 1},
		{pathComponents: -1},
		{entries: -1},
		{bytes: -1},
	} {
		if err := budget.AdmitPhysicalWork(work.pathComponents, work.entries, work.bytes); err == nil {
			t.Fatalf("accepted exhausted or invalid work: %+v", work)
		}
	}
}

func TestFileSetPathCounterRetainsAggregateCeiling(t *testing.T) {
	counter := &fileSetPathWorkCounter{}
	if err := counter.AdmitPathComponents(mutationfs.MaximumPhysicalPathComponentVisits); err != nil {
		t.Fatal(err)
	}
	if err := counter.AdmitPathComponents(1); err == nil {
		t.Fatal("accepted aggregate path-component overflow")
	}
	if err := counter.AdmitPathComponents(-1); err == nil {
		t.Fatal("accepted negative path work")
	}
}
