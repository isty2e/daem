package hostmutation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/removaloperation"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

type classifiedMutationFailure struct {
	kind mutationfs.FailureKind
}

func (failure classifiedMutationFailure) Error() string                { return string(failure.kind) }
func (failure classifiedMutationFailure) Kind() mutationfs.FailureKind { return failure.kind }

func TestHostEffectProgressSeparatesUntouchedTouchedAndIndeterminate(t *testing.T) {
	tests := []struct {
		name             string
		progress         EffectProgress
		rollbackEligible bool
		requiresRecovery bool
	}{
		{name: "untouched", progress: EffectNotStarted},
		{name: "touched expected-after", progress: EffectExpectedAfter, rollbackEligible: true},
		{name: "indeterminate visibility", progress: EffectIndeterminate, requiresRecovery: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.progress.RollbackEligible(); got != test.rollbackEligible {
				t.Fatalf("rollbackEligible = %t, want %t", got, test.rollbackEligible)
			}
			if got := test.progress.RequiresRecovery(); got != test.requiresRecovery {
				t.Fatalf("requiresRecovery = %t, want %t", got, test.requiresRecovery)
			}
		})
	}
}

func TestProgressAfterUntypedMutationErrorRequiresDurableRecovery(t *testing.T) {
	if got := ProgressAfterMutationError(errors.New("unknown mutation outcome")); got != EffectIndeterminate {
		t.Fatalf("progressAfterMutationError = %v, want indeterminate", got)
	}
}

func TestPostMutationVerificationFailureCannotBeClassifiedUncommitted(t *testing.T) {
	_, err := storagecommit.CaptureEntryIdentity(context.Background(), filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("CaptureEntryIdentity unexpectedly found missing path")
	}
	wrapped := mutationfs.MarkEffectIndeterminate(err)
	if !errors.Is(wrapped, err) {
		t.Fatal("post-mutation verification wrapper did not preserve its cause")
	}
	if got := ProgressAfterMutationError(wrapped); got != EffectIndeterminate {
		t.Fatalf("progressAfterMutationError = %v, want indeterminate", got)
	}
}

func TestClassifiedPreCommitFailureIsNotMarkedAsStarted(t *testing.T) {
	err := classifiedMutationFailure{kind: mutationfs.FailureUncommitted}
	if got := ProgressAfterMutationError(err); got != EffectNotStarted {
		t.Fatalf("progress = %v, want hostEffectNotStarted", got)
	}
}

func TestPostEffectUncertaintyDominatesNestedUncommittedRemoval(t *testing.T) {
	outcome, err := mutationfs.NewCommitOutcome(mutationfs.CommitOutcomeUncommitted, nil)
	if err != nil {
		t.Fatal(err)
	}
	cause := removaloperation.NewCommitError(outcome, classifiedMutationFailure{kind: mutationfs.FailureUncommitted})
	failure := mutationfs.MarkEffectIndeterminate(cause)
	if !errors.Is(failure, cause) || ProgressAfterMutationError(failure) != EffectIndeterminate {
		t.Fatal("nested removal erased post-effect uncertainty")
	}
}

func TestRootedRemovalOutcomeControlsEffectProgressWithoutRecursion(t *testing.T) {
	for _, test := range []struct {
		name  string
		state mutationfs.CommitOutcomeState
		want  EffectProgress
	}{
		{name: "uncommitted", state: mutationfs.CommitOutcomeUncommitted, want: EffectNotStarted},
		{name: "indeterminate", state: mutationfs.CommitOutcomeIndeterminate, want: EffectIndeterminate},
		{name: "retained recoverable", state: mutationfs.CommitOutcomeRetainedRecoverable, want: EffectIndeterminate},
		{name: "complete", state: mutationfs.CommitOutcomeComplete, want: EffectIndeterminate},
	} {
		t.Run(test.name, func(t *testing.T) {
			var retained []string
			if test.state == mutationfs.CommitOutcomeRetainedRecoverable {
				retained = []string{".daem-tombstone-test"}
			}
			outcome, err := mutationfs.NewCommitOutcome(test.state, retained)
			if err != nil {
				t.Fatalf("NewCommitOutcome returned error: %v", err)
			}
			failure := removaloperation.NewCommitError(outcome, errors.New("private storage cause"))
			if got := ProgressAfterMutationError(failure); got != test.want {
				t.Fatalf("progressAfterMutationError = %v, want %v", got, test.want)
			}
		})
	}
}
