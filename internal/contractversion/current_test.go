package contractversion

import (
	"testing"
)

func TestRecoveryJSONVersionIncludesFileSetObservationSemantics(t *testing.T) {
	t.Parallel()

	if RecoveryJSON != 9 {
		t.Fatalf("RecoveryJSON = %d, want 9", RecoveryJSON)
	}
}
