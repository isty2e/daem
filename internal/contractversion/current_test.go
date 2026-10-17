package contractversion

import (
	"testing"
)

func TestRecoveryJSONVersionIncludesFileSetObservationSemantics(t *testing.T) {
	t.Parallel()

	if RecoveryJSON != 10 {
		t.Fatalf("RecoveryJSON = %d, want 10", RecoveryJSON)
	}
}
