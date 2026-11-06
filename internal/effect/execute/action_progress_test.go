package execute

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
)

func TestHostActionProgressSelectsOnlySuccessfullyTouchedEntries(t *testing.T) {
	selections := make([]journalrecord.EntrySelection, 3)
	progress := newHostActionProgress(len(selections))
	progress.record(1, hostmutation.EffectExpectedAfter)

	selected, err := progress.rollbackEntries(selections)
	if err != nil {
		t.Fatalf("rollbackEntries returned error: %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("selected entries = %d, want one", len(selected))
	}
}

func TestHostActionProgressRejectsExpectedStateCardinalityDrift(t *testing.T) {
	progress := newHostActionProgress(2)

	_, err := progress.rollbackEntries([]journalrecord.EntrySelection{{}})
	if err == nil || !strings.Contains(err.Error(), "progress count 2 does not match entry selection count 1") {
		t.Fatalf("rollbackEntries error = %v, want cardinality mismatch", err)
	}
}
