package delegate

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestInvocationPreservesExactRequestOptionsAndMechanicalResult(t *testing.T) {
	action := testAction(t, testActionInput{disposition: reconcile.DelegateScheduled, mode: delegatepolicy.ModeApply, command: "fixture", envRefs: []string{"TOKEN"}})
	executor := NewExecutor(Options{
		OutputLimit: 7, LookupEnv: func(string) (string, bool) { return "fixture-secret", true },
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{Started: true, HasExitCode: true, Stdout: "fixture-secret extra output"}
		},
	})
	expected, scheduled := executor.ScheduledRequest(action)
	if !scheduled || expected.OutputLimit != 7 {
		t.Fatal("scheduled request lost configured execution options")
	}
	boundary := errors.New("containing qualification failed")
	record, observed, err := executor.ExecuteWithInvocation(t.Context(), action, testWorkingDirectoryBinder(t), func(ctx context.Context, command subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, bind subprocess.WorkingDirectoryBinder) (subprocess.CommandAttemptResult, bool, error) {
		if !reflect.DeepEqual(request, expected) {
			t.Fatal("execution reconstructed a different request from reservation")
		}
		return command.ExecuteInWorkingDirectory(ctx, request, bind), true, boundary
	})
	if !observed || !errors.Is(err, boundary) || record.Status() != AttemptSucceeded || !record.RunnerInvoked() || record.Subject() != action.Subject() || !record.Redacted() {
		t.Fatal("qualification failure falsified or discarded a real sanitized attempt", err)
	}
}

func TestInvocationRefusalDoesNotInventAttemptOrInvokeStateOnlyActions(t *testing.T) {
	for _, disposition := range []reconcile.DelegateDisposition{reconcile.DelegateScheduled, reconcile.DelegateBlocked, reconcile.DelegateSkipped} {
		t.Run(string(disposition), func(t *testing.T) {
			input := testActionInput{disposition: disposition, mode: delegatepolicy.ModeApply, command: "fixture"}
			if disposition == reconcile.DelegateBlocked {
				input.runnerReadiness = delegatepolicy.RunnerMissing
			}
			action := testAction(t, input)
			called := false
			executor := NewExecutor(Options{})
			_, scheduled := executor.ScheduledRequest(action)
			if scheduled != action.SchedulesAttempt() {
				t.Fatal("state-only action acquired a subprocess request")
			}
			_, observed, err := executor.ExecuteWithInvocation(t.Context(), action, nil, func(context.Context, subprocess.CommandExecutor, subprocess.CommandAttemptRequest, subprocess.WorkingDirectoryBinder) (subprocess.CommandAttemptResult, bool, error) {
				called = true
				return subprocess.CommandAttemptResult{}, false, nil
			})
			if called != scheduled || observed == scheduled || (err != nil) != scheduled {
				t.Fatal("pre-launch refusal invented an attempt or state-only work invoked a command", err)
			}
		})
	}
}
