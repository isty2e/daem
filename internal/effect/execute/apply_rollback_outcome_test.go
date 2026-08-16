package execute

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestApplyRollbackErrorPreservesTypedOutcomeAndCauses(t *testing.T) {
	t.Parallel()

	primary := context.Canceled
	retirement := errors.New("private retirement failure")
	err := newApplyRollbackError(primary, retirement)

	if !ApplyHostChangesRolledBack(err) {
		t.Fatal("ApplyHostChangesRolledBack = false, want true")
	}
	if !errors.Is(err, primary) || !errors.Is(err, retirement) {
		t.Fatalf("error causes = %v, want primary and retirement causes", err)
	}
	for _, fragment := range []string{"host changes rolled back", "retire recovery journal failed"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("error = %q, want %q", err, fragment)
		}
	}
}

func TestApplyHostChangesRolledBackRejectsOrdinaryErrors(t *testing.T) {
	t.Parallel()

	if ApplyHostChangesRolledBack(errors.New("ordinary failure")) {
		t.Fatal("ApplyHostChangesRolledBack = true, want false")
	}
}
