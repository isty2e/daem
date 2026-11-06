package hostmutation

import (
	"strings"
	"testing"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
)

func TestHostActionProgressSelectsOnlySuccessfullyTouchedEntries(t *testing.T) {
	selections := make([]journalrecord.EntrySelection, 3)
	progress := NewActionProgress(len(selections))
	progress.record(1, EffectExpectedAfter)

	selected, err := progress.RollbackEntries(selections)
	if err != nil {
		t.Fatalf("rollbackEntries returned error: %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("selected entries = %d, want one", len(selected))
	}
}

func TestHostActionProgressRejectsExpectedStateCardinalityDrift(t *testing.T) {
	progress := NewActionProgress(2)

	_, err := progress.RollbackEntries([]journalrecord.EntrySelection{{}})
	if err == nil || !strings.Contains(err.Error(), "progress count 2 does not match entry selection count 1") {
		t.Fatalf("rollbackEntries error = %v, want cardinality mismatch", err)
	}
}

func TestActionProgressCopiesShareUpdatesButCombinationCapturesAnIndependentSnapshot(t *testing.T) {
	left := NewActionProgress(2)
	shared := left
	shared.record(1, EffectExpectedAfter)
	selected, err := left.RollbackEntries(make([]journalrecord.EntrySelection, 2))
	if err != nil || len(selected) != 1 {
		t.Fatalf("copied progress did not retain step result: selected=%d error=%v", len(selected), err)
	}

	right := NewActionProgress(1)
	combined := CombineActionProgress(left, ActionProgress{}, right)
	left.record(1, EffectIndeterminate)
	right.record(0, EffectExpectedAfter)
	selected, err = combined.RollbackEntries(make([]journalrecord.EntrySelection, 3))
	if err != nil || len(selected) != 1 || combined.RequiresRecovery() {
		t.Fatalf("combined progress followed a later source update: selected=%d error=%v", len(selected), err)
	}
	if !left.RequiresRecovery() {
		t.Fatal("source progress lost its uncertain result")
	}

	combined.record(0, EffectExpectedAfter)
	selected, err = left.RollbackEntries(make([]journalrecord.EntrySelection, 2))
	if err != nil || len(selected) != 0 {
		t.Fatalf("combined update changed its source: selected=%d error=%v", len(selected), err)
	}
}
