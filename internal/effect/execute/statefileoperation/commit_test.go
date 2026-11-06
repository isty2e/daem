package statefileoperation

import (
	"errors"
	"testing"
)

func TestInvalidStatefileCommitOutcomesRequireRecovery(t *testing.T) {
	for _, outcome := range []Outcome{
		{},
		{status: statefileCommitted, err: errors.New("contradictory success")},
		{status: statefileUncommitted},
		{status: statefileCommitIndeterminate},
	} {
		if outcome.Committed() || !outcome.RequiresRecovery() {
			t.Fatalf("invalid outcome %#v was treated as committed or rollback-safe", outcome)
		}
	}
}
