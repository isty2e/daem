package cli

import (
	"context"
	"testing"

	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestApplyFailureDetailPreservesCancellationAcrossPreEffectStages(t *testing.T) {
	for _, stage := range []applyFailureStage{
		applyFailureProjection,
		applyFailureDiff,
		applyFailureConfirmation,
		applyFailureOutput,
	} {
		detail, reason := applyFailureDetail(
			stage,
			context.Canceled,
			applyworkflow.CommandResult{},
		)
		if detail != "apply was cancelled before effects" ||
			reason != applyworkflow.FailureReasonCancelled {
			t.Fatalf("stage %q = %q/%q, want pre-effect cancellation", stage, detail, reason)
		}
	}
}

func TestApplyFailureDetailPreservesPostEffectDeadline(t *testing.T) {
	detail, reason := applyFailureDetail(
		applyFailureDiff,
		context.DeadlineExceeded,
		applyworkflow.CommandResult{ExecutionAttempted: true},
	)
	if detail != "apply was cancelled after an effect boundary was crossed" ||
		reason != applyworkflow.FailureReasonCancelled {
		t.Fatalf("detail/reason = %q/%q, want post-effect cancellation", detail, reason)
	}
}
