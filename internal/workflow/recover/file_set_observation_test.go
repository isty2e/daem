package recover

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
)

func TestExecutionResultPreservesEveryKnownNonClearFileSetFence(t *testing.T) {
	for _, kind := range []fileset.FileSetFenceKind{
		fileset.FileSetFencePublishedTransaction,
		fileset.FileSetFenceInvalidEvidence,
		fileset.FileSetFenceAbandonedResidue,
		fileset.FileSetFenceCensusLimit,
		fileset.FileSetFenceAccessUnprovable,
	} {
		t.Run(string(kind), func(t *testing.T) {
			result := retiredExecutionResult("operation", true).withFileSetFence(kind)
			got := result.FileSetFenceObservation()
			if !got.Observed() || !got.Known() || got.Kind() != kind {
				t.Fatalf(
					"FileSetFenceObservation = observed:%t known:%t kind:%q, want known %q",
					got.Observed(),
					got.Known(),
					got.Kind(),
					kind,
				)
			}
		})
	}
}

func TestExecutionResultPreservesObservedUnknownFileSetFence(t *testing.T) {
	observation := fileset.ObserveFileSetFence(errors.New("unclassified file-set observation"))
	result := retiredExecutionResult("operation", false).withFileSetFenceObservation(observation)
	got := result.FileSetFenceObservation()
	if !got.Observed() || got.Known() {
		t.Fatalf("FileSetFenceObservation = observed:%t known:%t", got.Observed(), got.Known())
	}
	if result.HasNonClearFileSetObservation() != true {
		t.Fatal("unknown file-set observation was not retained as non-clear evidence")
	}
}
