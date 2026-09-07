package apply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/recoverygate"
)

func TestClassifyFailureDerivesClosedPublicFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		result  CommandResult
		reason  FailureReason
		phase   FailurePhase
		outcome FailureOutcome
	}{
		{
			name:    "stale snapshot",
			err:     fmt.Errorf("private path: %w", mutation.StaleSnapshotError{}),
			reason:  FailureReasonStaleSnapshot,
			phase:   FailurePhasePreflight,
			outcome: FailureOutcomeRefused,
		},
		{
			name:    "contention after effect",
			err:     fmt.Errorf("secret: %w", testMutationFailure(mutation.ReasonContention)),
			result:  CommandResult{ExecutionAttempted: true},
			reason:  FailureReasonMutationContended,
			phase:   FailurePhaseExecution,
			outcome: FailureOutcomeIncomplete,
		},
		{
			name:    "context cancellation",
			err:     context.Canceled,
			reason:  FailureReasonCancelled,
			phase:   FailurePhasePreflight,
			outcome: FailureOutcomeRefused,
		},
		{
			name:    "missing environment",
			err:     missingMCPEnvironmentSourcesError{names: []string{"PRIVATE_TOKEN"}},
			reason:  FailureReasonMCPEnvironmentUnavailable,
			phase:   FailurePhasePreflight,
			outcome: FailureOutcomeRefused,
		},
		{
			name:    "relation order risk",
			err:     errors.Join(ErrRelationOrderRiskExpansion, errors.New("/private/path")),
			reason:  FailureReasonRelationOrderRiskExpanded,
			phase:   FailurePhasePreflight,
			outcome: FailureOutcomeRefused,
		},
		{
			name:    "unclassified execution failure",
			err:     errors.New("token=private\n\x1b[2J"),
			result:  CommandResult{ExecutionAttempted: true},
			reason:  FailureReasonApplyIncomplete,
			phase:   FailurePhaseExecution,
			outcome: FailureOutcomeIncomplete,
		},
		{
			name:    "abandoned file-set residue",
			err:     fmt.Errorf("wrap: %w", fileset.ErrAbandonedFileSetResidue),
			reason:  FailureReasonAbandonedFileSetResidue,
			phase:   FailurePhasePreflight,
			outcome: FailureOutcomeRefused,
		},
		{
			name:    "recoverable interrupted apply",
			err:     fmt.Errorf("%w; run: daem recover --dry-run", journal.ErrInterruptedApply),
			reason:  FailureReasonInterruptedApply,
			phase:   FailurePhasePreflight,
			outcome: FailureOutcomeRefused,
		},
		{
			name: "recoverable journal with continuing residue",
			err: recoverygate.Combine(
				fmt.Errorf("%w; run: daem recover --dry-run", journal.ErrInterruptedApply),
				fileset.ErrAbandonedFileSetResidue,
			),
			reason:  FailureReasonInterruptedApplyFileSetFence,
			phase:   FailurePhasePreflight,
			outcome: FailureOutcomeRefused,
		},
		{
			name: "cleanup journal with continuing residue",
			err: recoverygate.Combine(
				fmt.Errorf("%w; run: daem recover --dry-run", journal.ErrIncompleteJournalCleanup),
				fileset.ErrAbandonedFileSetResidue,
			),
			reason:  FailureReasonJournalCleanupFileSetFence,
			phase:   FailurePhasePreflight,
			outcome: FailureOutcomeRefused,
		},
		{
			name:    "unprovable StateDir access after effects still names the boundary",
			err:     fmt.Errorf("wrap: %w", fileset.ErrFileSetAccessUnprovable),
			result:  CommandResult{ExecutionAttempted: true},
			reason:  FailureReasonFileSetAccessUnprovable,
			phase:   FailurePhaseExecution,
			outcome: FailureOutcomeIncomplete,
		},
		{
			name: "joined stale plan does not mask abandoned residue",
			err: errors.Join(
				mutation.StalePlanError{},
				fileset.ErrAbandonedFileSetResidue,
			),
			reason:  FailureReasonAbandonedFileSetResidue,
			phase:   FailurePhasePreflight,
			outcome: FailureOutcomeRefused,
		},
		{
			name: "joined stale snapshot does not mask unprovable access after effects",
			err: errors.Join(
				mutation.StaleSnapshotError{},
				fmt.Errorf("replan after MCP provider prerequisite: %w", fileset.ErrFileSetAccessUnprovable),
			),
			result:  CommandResult{ExecutionAttempted: true},
			reason:  FailureReasonFileSetAccessUnprovable,
			phase:   FailurePhaseExecution,
			outcome: FailureOutcomeIncomplete,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			failure := ClassifyFailure(test.err, test.result)
			if failure.Reason() != test.reason ||
				failure.Phase() != test.phase ||
				failure.Outcome() != test.outcome {
				t.Fatalf(
					"failure = (%q, %q, %q), want (%q, %q, %q)",
					failure.Reason(),
					failure.Phase(),
					failure.Outcome(),
					test.reason,
					test.phase,
					test.outcome,
				)
			}
			for _, private := range []string{"PRIVATE_TOKEN", "token=private", "/private/path", "\x1b"} {
				if strings.Contains(failure.Detail(), private) {
					t.Fatalf("public detail %q contains private cause fragment %q", failure.Detail(), private)
				}
			}
		})
	}
}

func TestStaleApplyErrorPreservesCancellation(t *testing.T) {
	for _, disclosed := range []bool{false, true} {
		if err := staleApplyError(disclosed, context.Canceled); err != context.Canceled {
			t.Fatalf("staleApplyError(%t) = %v, want exact cancellation", disclosed, err)
		}
	}
}

func TestStaleApplyErrorPreservesFileSetFenceSentinels(t *testing.T) {
	t.Parallel()
	causes := []error{
		fileset.ErrAbandonedFileSetResidue,
		fmt.Errorf("inspect file-set state dir: %w", fileset.ErrFileSetAccessUnprovable),
	}
	for _, disclosed := range []bool{false, true} {
		for _, cause := range causes {
			got := staleApplyError(disclosed, cause)
			if got != cause {
				t.Fatalf("staleApplyError(%t, %v) = %v, want exact cause", disclosed, cause, got)
			}
			failure := ClassifyFailure(got, CommandResult{ExecutionAttempted: disclosed})
			if failure.Reason() != FailureReasonAbandonedFileSetResidue &&
				failure.Reason() != FailureReasonFileSetAccessUnprovable {
				t.Fatalf("reason = %q, want a file-set fence reason", failure.Reason())
			}
			if strings.Contains(failure.Detail(), "authoritative inputs changed") ||
				strings.Contains(failure.Detail(), "authorized apply plan changed") {
				t.Fatalf("detail = %q, want fence guidance not stale-plan retry", failure.Detail())
			}
		}
	}
}

type testMutationFailure mutation.ReasonCode

func (failure testMutationFailure) Error() string { return "private mutation failure" }

func (failure testMutationFailure) Code() mutation.ReasonCode {
	return mutation.ReasonCode(failure)
}
