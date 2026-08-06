package execute

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
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
		progress         hostEffectProgress
		rollbackEligible bool
		requiresRecovery bool
	}{
		{name: "untouched", progress: hostEffectNotStarted},
		{name: "touched expected-after", progress: hostEffectExpectedAfter, rollbackEligible: true},
		{name: "indeterminate visibility", progress: hostEffectIndeterminate, requiresRecovery: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.progress.rollbackEligible(); got != test.rollbackEligible {
				t.Fatalf("rollbackEligible = %t, want %t", got, test.rollbackEligible)
			}
			if got := test.progress.requiresRecovery(); got != test.requiresRecovery {
				t.Fatalf("requiresRecovery = %t, want %t", got, test.requiresRecovery)
			}
		})
	}
}

func TestHostActionProgressSelectsOnlySuccessfullyTouchedEntries(t *testing.T) {
	selections := make([]journal.EntrySelection, 3)
	progress := newHostActionProgress(len(selections))
	progress.record(1, hostEffectExpectedAfter)

	selected, err := progress.rollbackEntries(selections)
	if err != nil {
		t.Fatalf("rollbackEntries returned error: %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("selected entries = %d, want one", len(selected))
	}
}

func TestHostActionProgressRejectsExpectedStateCardinalityDrift(t *testing.T) {
	progress := newHostActionProgress(2)

	_, err := progress.rollbackEntries([]journal.EntrySelection{{}})
	if err == nil || !strings.Contains(err.Error(), "progress count 2 does not match entry selection count 1") {
		t.Fatalf("rollbackEntries error = %v, want cardinality mismatch", err)
	}
}

func TestProgressAfterUntypedMutationErrorRequiresDurableRecovery(t *testing.T) {
	if got := progressAfterMutationError(errors.New("unknown mutation outcome")); got != hostEffectIndeterminate {
		t.Fatalf("progressAfterMutationError = %v, want indeterminate", got)
	}
}

func TestPostMutationVerificationFailureCannotBeClassifiedUncommitted(t *testing.T) {
	_, err := storagecommit.CaptureEntryIdentity(context.Background(), filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("CaptureEntryIdentity unexpectedly found missing path")
	}
	wrapped := markHostEffectIndeterminate(err)
	if !errors.Is(wrapped, err) {
		t.Fatal("post-mutation verification wrapper did not preserve its cause")
	}
	if got := progressAfterMutationError(wrapped); got != hostEffectIndeterminate {
		t.Fatalf("progressAfterMutationError = %v, want indeterminate", got)
	}
}

func TestClassifiedPreCommitFailureIsNotMarkedAsStarted(t *testing.T) {
	err := classifiedMutationFailure{kind: mutationfs.FailureUncommitted}
	if got := progressAfterMutationError(err); got != hostEffectNotStarted {
		t.Fatalf("progress = %v, want hostEffectNotStarted", got)
	}
}

func TestRootedRemovalOutcomeControlsEffectProgressWithoutRecursion(t *testing.T) {
	for _, test := range []struct {
		name  string
		state mutationfs.CommitOutcomeState
		want  hostEffectProgress
	}{
		{name: "uncommitted", state: mutationfs.CommitOutcomeUncommitted, want: hostEffectNotStarted},
		{name: "indeterminate", state: mutationfs.CommitOutcomeIndeterminate, want: hostEffectIndeterminate},
		{name: "retained recoverable", state: mutationfs.CommitOutcomeRetainedRecoverable, want: hostEffectIndeterminate},
		{name: "complete", state: mutationfs.CommitOutcomeComplete, want: hostEffectIndeterminate},
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
			failure := &rootedRemovalCommitError{outcome: outcome, cause: errors.New("private storage cause")}
			if got := progressAfterMutationError(failure); got != test.want {
				t.Fatalf("progressAfterMutationError = %v, want %v", got, test.want)
			}
		})
	}
}
