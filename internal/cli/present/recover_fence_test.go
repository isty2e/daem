package clipresent

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration/transaction"
)

func TestPrintContinuingFileSetFenceIsPathNeutralAndExplicit(t *testing.T) {
	var output bytes.Buffer
	printContinuingFileSetFence(&output, transaction.FileSetFenceAbandonedResidue)
	text := output.String()
	if !strings.Contains(text, "continuing file-set fence: abandoned_residue") ||
		!strings.Contains(text, "journal recovery does not clear this fence") ||
		!strings.Contains(text, "do not delete reserved names by prefix") {
		t.Fatalf("output = %q", text)
	}
}

func TestFileSetFenceObservationProjectionPreservesAccessAndUnknown(t *testing.T) {
	access := transaction.KnownFileSetFence(transaction.FileSetFenceAccessUnprovable)
	if got := fileSetFenceObservationValue(access); got != "access_unprovable" {
		t.Fatalf("access observation = %q", got)
	}
	if guidance := fileSetFenceRemediation(access.Kind(), access.Known()); !strings.Contains(guidance, "restore StateDir") {
		t.Fatalf("access guidance = %q", guidance)
	}

	unknown := transaction.ObserveFileSetFence(errors.New("unclassified"))
	if got := fileSetFenceObservationValue(unknown); got != "unknown" {
		t.Fatalf("unknown observation = %q", got)
	}
	if guidance := fileSetFenceRemediation(unknown.Kind(), unknown.Known()); !strings.Contains(guidance, "preserve recovery and file-set evidence") {
		t.Fatalf("unknown guidance = %q", guidance)
	}
}
