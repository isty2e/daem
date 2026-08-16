package apply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
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

type testMutationFailure mutation.ReasonCode

func (failure testMutationFailure) Error() string { return "private mutation failure" }

func (failure testMutationFailure) Code() mutation.ReasonCode {
	return mutation.ReasonCode(failure)
}
