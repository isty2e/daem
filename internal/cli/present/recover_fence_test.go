package clipresent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration/transaction"
)

func TestPrintContinuingFileSetFenceIsPathNeutralAndExplicit(t *testing.T) {
	var output bytes.Buffer
	printContinuingFileSetFence(&output, transaction.FileSetFenceAbandonedResidue)
	text := output.String()
	if !strings.Contains(text, "continuing file-set fence: abandoned_residue") ||
		!strings.Contains(text, "journal recovery does not clear this fence") {
		t.Fatalf("output = %q", text)
	}
}
