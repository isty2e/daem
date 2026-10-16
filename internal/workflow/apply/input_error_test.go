package apply

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestInputErrorCauseRequiresWorkflowProvenance(t *testing.T) {
	cause := errors.New("input cause")
	for _, err := range []error{nil, commandInputError{}, cause, fmt.Errorf("invalid manifest: %w", cause), fmt.Errorf("%w: %w", ErrReadLockfile, cause)} {
		if got, ok := InputErrorCause(err); ok || got != nil {
			t.Fatalf("unmarked error admitted as input: %v -> %v/%t", err, got, ok)
		}
	}
	marked := commandInputError{cause: cause}
	for _, err := range []error{marked, fmt.Errorf("outer operation: %w", marked)} {
		got, ok := InputErrorCause(err)
		if !ok || got != cause || !errors.Is(err, cause) {
			t.Fatalf("input provenance or cause changed: %v -> %v/%t", err, got, ok)
		}
	}
	if marked.Error() != cause.Error() {
		t.Fatalf("marker altered existing error text: %q", marked.Error())
	}
}

func TestInputErrorMarkerPreservesPublicFailureFacts(t *testing.T) {
	for _, cause := range []error{errors.New("private input detail"), fmt.Errorf("%w: private input detail", ErrReadLockfile), context.Canceled} {
		for _, result := range []CommandResult{{}, {ExecutionAttempted: true}} {
			before := ClassifyFailure(cause, result)
			after := ClassifyFailure(commandInputError{cause: cause}, result)
			if before.Reason() != after.Reason() || before.Phase() != after.Phase() || before.Outcome() != after.Outcome() || before.Detail() != after.Detail() {
				t.Fatalf("input marker changed public failure facts: before=%#v after=%#v", before, after)
			}
		}
	}
}
