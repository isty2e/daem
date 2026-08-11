package recover

import (
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
)

func TestExecutionResultSemanticErrorFollowsAuthorityLifetime(t *testing.T) {
	cause := errors.New("inspect /private/recovery/path: permission denied")
	retained := ExecutionResult{authorityState: durableAuthorityActive}
	if got := retained.SemanticError(cause); got != cause {
		t.Fatalf("retained semantic error = %v, want original cause", got)
	}

	retired := retiredExecutionResult("operation", false)
	got := retired.SemanticError(cause)
	if !errors.Is(got, cause) {
		t.Fatalf("terminal semantic error lost internal cause: %v", got)
	}
	if strings.Contains(got.Error(), "/private/recovery/path") ||
		got.Error() != "recovery authority retired; post-retirement validation did not complete successfully; no recovery action remains" {
		t.Fatalf("terminal semantic error = %q", got)
	}
}

func TestExecutionResultTerminalAuthorityOverridesStaleCleanupFailure(t *testing.T) {
	cause := journal.WrapCleanupFailure(
		retirement.ActionFinalizeJournalCleanup,
		errors.New("private cleanup cause at /private/recovery/path"),
	)
	got := retiredExecutionResult("operation", false).SemanticError(cause)
	const want = "recovery authority retired; post-retirement validation did not complete successfully; no recovery action remains"
	if got.Error() != want || strings.Contains(got.Error(), "private") {
		t.Fatalf("terminal cleanup failure = %q, want %q", got, want)
	}
}

func TestExecutionResultUnknownAuthorityOverridesStaleCleanupFailure(t *testing.T) {
	cause := journal.WrapCleanupFailure(
		retirement.ActionFinalizeJournalCleanup,
		errors.New("private cleanup cause at /private/recovery/path"),
	)
	got := unknownExecutionResult("operation").SemanticError(cause)
	const want = "recovery write failed and current durable authority could not be classified; preserve recovery artifacts and inspect again"
	if got.Error() != want || strings.Contains(got.Error(), "private") {
		t.Fatalf("unknown cleanup failure = %q, want %q", got, want)
	}
}

func TestExecutionResultFailurePreservesOnlyLiveRetryAuthority(t *testing.T) {
	retained := ExecutionResult{authorityState: durableAuthorityActive}
	if got := retained.withExecutionFailure(); got.Phase() != ExecutionPhaseActiveAuthorityRetained {
		t.Fatalf("retained failure phase = %q", got.Phase())
	}

	completed := retiredExecutionResult("operation", true)
	if got := completed.withExecutionFailure(); got.Phase() != ExecutionPhaseAuthorityRetired {
		t.Fatalf("terminal failure phase = %q", got.Phase())
	}
}

func TestExecutionResultUnknownAuthorityRedactsCause(t *testing.T) {
	cause := errors.New("inspect /private/recovery/path: permission denied")
	got := unknownExecutionResult("operation").SemanticError(cause)
	if !errors.Is(got, cause) {
		t.Fatalf("unknown semantic error lost internal cause: %v", got)
	}
	if strings.Contains(got.Error(), "/private/recovery/path") ||
		got.Error() != "recovery write failed and current durable authority could not be classified; preserve recovery artifacts and inspect again" {
		t.Fatalf("unknown semantic error = %q", got)
	}
}
