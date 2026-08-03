package apply

import (
	"context"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/reconcile"
	reconcilehostroute "github.com/isty2e/daem/internal/reconcile/build/hostroute"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestPostAttemptSummariesDoNotPromoteAggregateProcessSuccessWithoutEquivalentObservation(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	selection := applyMCPSelection(t)
	locked, _ := applyMCPLockfile(t, "context7", "success-daem-test", []string{"--serve", "context7"})
	actions, err := reconcilehostroute.BuildDelegateActions(reconcilehostroute.DelegateInput{
		Locked:          locked,
		SelectedTargets: applySelectedTargets(t, selection),
		Context:         reconcile.ContextApply,
		Readiness: []reconcilehostroute.DelegateReadinessFact{
			{
				Subject: locked.Locked.Subjects()[0].SubjectID(),
				Runner:  delegatepolicy.RunnerAvailable,
			},
		},
	})
	if err != nil {
		t.Fatalf("Build delegate actions: %v", err)
	}

	executor := delegate.NewExecutor(delegate.Options{
		LookupEnv: func(name string) (string, bool) {
			return "safe", true
		},
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{Stdout: "host says installed"}
		},
	})
	attempt := executor.Execute(context.Background(), actions[0], delegateSummaryWorkingDirectoryBinder(t))

	summaries := postAttemptSummaries(
		context.Background(),
		paths,
		locked,
		selection,
		durable.EmptySnapshot(),
		[]delegate.AttemptRecord{attempt},
	)
	if len(summaries) != 1 {
		t.Fatalf("post-attempt summaries = %#v, want one summary", summaries)
	}
	if attempt.Status() != delegate.AttemptSucceeded {
		t.Fatalf("attempt status = %q, want succeeded", attempt.Status())
	}
	got := summaries[delegateAttemptKeyForAttempt(attempt)]
	if got.observation != observerelation.ObservationMissing ||
		got.postcondition != observerelation.PostconditionNotObserved {
		t.Fatalf("post-attempt summary = %#v, want missing observation and not_observed postcondition", got)
	}
	results, err := delegateAttemptResults([]delegate.AttemptRecord{attempt}, summaries)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 ||
		results[0].Attempt().Status() != delegate.AttemptSucceeded ||
		results[0].ObservationSummary() != observerelation.ObservationMissing ||
		results[0].PostconditionSummary() != observerelation.PostconditionNotObserved {
		t.Fatalf("delegate attempt results = %#v, want one history-only result", results)
	}
}

func TestDelegateAttemptResultRejectsZeroEffectIdentity(t *testing.T) {
	t.Parallel()

	if _, err := newDelegateAttemptResult(
		delegate.AttemptRecord{},
		observerelation.ObservationNotObserved,
		observerelation.PostconditionNotObserved,
	); err == nil {
		t.Fatal("zero delegate effect identity accepted")
	}
}

func TestDelegateAttemptResultRejectsUnknownAssuranceSummaries(t *testing.T) {
	t.Parallel()

	attempt := delegateAttemptForSummaryTest(t)
	tests := []struct {
		name          string
		observation   observerelation.ObservationSummary
		postcondition observerelation.PostconditionSummary
	}{
		{
			name:          "unknown observation vocabulary",
			observation:   observerelation.ObservationSummary("ready"),
			postcondition: observerelation.PostconditionNotObserved,
		},
		{
			name:          "unknown postcondition vocabulary",
			observation:   observerelation.ObservationNotObserved,
			postcondition: observerelation.PostconditionSummary("installed"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newDelegateAttemptResult(attempt, test.observation, test.postcondition); err == nil {
				t.Fatal("unknown assurance summary accepted")
			}
		})
	}
}

func delegateAttemptForSummaryTest(t *testing.T) delegate.AttemptRecord {
	t.Helper()
	selection := applyMCPSelection(t)
	locked, _ := applyMCPLockfile(t, "context7", "success-daem-test", []string{"--serve", "context7"})
	actions, err := reconcilehostroute.BuildDelegateActions(reconcilehostroute.DelegateInput{
		Locked:          locked,
		SelectedTargets: applySelectedTargets(t, selection),
		Context:         reconcile.ContextApply,
		Readiness: []reconcilehostroute.DelegateReadinessFact{
			{
				Subject: locked.Locked.Subjects()[0].SubjectID(),
				Runner:  delegatepolicy.RunnerAvailable,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := delegate.NewExecutor(delegate.Options{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{Stdout: "host says installed"}
		},
	})
	return executor.Execute(context.Background(), actions[0], delegateSummaryWorkingDirectoryBinder(t))
}

func delegateSummaryWorkingDirectoryBinder(t *testing.T) subprocess.WorkingDirectoryBinder {
	t.Helper()
	root, err := rootedpath.CaptureRoot(t.TempDir())
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close delegate summary root: %v", err)
		}
	})
	return func() (subprocess.WorkingDirectoryBinding, error) {
		return root.AcquireWorkingDirectory()
	}
}
