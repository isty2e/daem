package readiness

import (
	"errors"
	"testing"
)

func TestRelationReconciliationErrorPreservesCauseAndText(t *testing.T) {
	cause := errors.New("carrier reconciliation failed")
	err := newRelationReconciliationError(cause)

	if !IsRelationReconciliationError(err) {
		t.Fatal("IsRelationReconciliationError returned false")
	}
	if !errors.Is(err, cause) {
		t.Fatal("relation reconciliation error does not preserve its cause")
	}
	if err.Error() != cause.Error() {
		t.Fatalf("error text = %q, want %q", err, cause)
	}
}
