package postcondition

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/effectpostcondition"
)

func TestSummarySetRejectsDuplicatesAndDefendsCopies(t *testing.T) {
	artifacts, err := NewSummary(
		effectpostcondition.CarrierArtifactsAbsent,
		SummarySatisfied,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSummarySet([]Summary{artifacts, artifacts}); err == nil ||
		!strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate NewSummarySet error = %v", err)
	}

	set, err := NewSummarySet([]Summary{artifacts})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Summaries()
	if len(got) != 1 ||
		got[0].Requirement() != effectpostcondition.CarrierArtifactsAbsent {
		t.Fatalf("canonical summaries = %#v", got)
	}
	got[0] = Summary{}
	if preserved := set.Summaries(); len(preserved) != 1 || preserved[0] != artifacts {
		t.Fatalf("caller mutation changed summary set: %#v", preserved)
	}
}
