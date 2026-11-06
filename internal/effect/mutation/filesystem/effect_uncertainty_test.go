package filesystem

import (
	"errors"
	"fmt"
	"testing"
)

func TestPostEffectUncertaintyPreservesCauseAndDominatesItsClassification(t *testing.T) {
	cause := classifiedTestFailure{kind: FailureUncommitted}
	failure := MarkEffectIndeterminate(cause)
	wrapped := fmt.Errorf("verification failed: %w", failure)
	if !errors.Is(wrapped, cause) {
		t.Fatal("effect uncertainty erased its cause")
	}
	if !HasIndeterminateEffect(wrapped) {
		t.Fatal("wrapped post-effect failure lost its uncertainty")
	}
	if kind, ok := FailureKindOf(wrapped); !ok || kind != FailureIndeterminateCommit {
		t.Fatalf("post-effect failure classification = %q, %v", kind, ok)
	}
	if !MayHaveVisibleEffect(wrapped) {
		t.Fatal("uncommitted verification cause erased a possible earlier effect")
	}
	if MarkEffectIndeterminate(nil) != nil || HasIndeterminateEffect(cause) {
		t.Fatal("uncertainty was added without a post-effect failure")
	}
}
