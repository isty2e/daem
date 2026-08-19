package gitcli

import (
	"context"
	"errors"
	"testing"
)

func TestGitAttemptContextErrUsesObservedWaitNotCallerReread(t *testing.T) {
	if got := gitAttemptContextErr(nil, gitProcessResult{}); got != nil {
		t.Fatalf("empty result context error = %v, want nil", got)
	}

	exitErr := errors.New("exit status 1")
	if got := gitAttemptContextErr(nil, gitProcessResult{commandErr: exitErr}); got != nil {
		t.Fatalf("nonzero git exit context error = %v, want nil", got)
	}

	if got := gitAttemptContextErr(context.Canceled, gitProcessResult{}); got != context.Canceled {
		t.Fatalf("consume cancel = %v, want exact context.Canceled", got)
	}
	if got := gitAttemptContextErr(nil, gitProcessResult{commandErr: context.DeadlineExceeded}); got != context.DeadlineExceeded {
		t.Fatalf("await deadline = %v, want exact context.DeadlineExceeded", got)
	}
}

func TestGitAttemptContextErrDoesNotPromoteCommandCancelOverConsumerFailure(t *testing.T) {
	consumer := errors.New("listing budget exceeded")
	got := gitAttemptContextErr(consumer, gitProcessResult{commandErr: context.Canceled})
	if got != nil {
		t.Fatalf("non-context consumer with later command cancel = %v, want frozen consumer", got)
	}
	if got := gitAttemptContextErr(consumer, gitProcessResult{commandErr: context.DeadlineExceeded}); got != nil {
		t.Fatalf("non-context consumer with later command deadline = %v, want frozen consumer", got)
	}
}

func TestGitObservedLifecycleErrorPrefersCapturedDiagnostic(t *testing.T) {
	cause := context.Canceled
	captured := &capturedGitCommandError{diagnostic: "fatal: redacted", cause: cause}
	got := gitObservedLifecycleError(nil, gitProcessResult{commandErr: captured})
	if got != captured {
		t.Fatalf("lifecycle error = %#v, want captured diagnostic wrapper", got)
	}
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("lifecycle error = %v, want unwrap to context.Canceled", got)
	}
	if got.Error() != "fatal: redacted" {
		t.Fatalf("public error = %q, want diagnostic without cause text", got.Error())
	}
}

func TestGitObservedLifecycleErrorJoinsMissingConsumerCancelIntoDiagnostic(t *testing.T) {
	captured := &capturedGitCommandError{diagnostic: "fatal: redacted", cause: errors.New("exit status 128")}
	got := gitObservedLifecycleError(context.Canceled, gitProcessResult{commandErr: captured})
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("lifecycle error = %v, want consumer context.Canceled preserved", got)
	}
	if got.Error() != "fatal: redacted" {
		t.Fatalf("public error = %q, want diagnostic without cause text", got.Error())
	}
	if errors.Is(captured, context.Canceled) {
		t.Fatal("original wrapper must not be mutated to carry the consumer cause")
	}
}
