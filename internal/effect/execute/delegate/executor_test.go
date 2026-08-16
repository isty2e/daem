package delegate

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestExecutePreservesCanonicalSubjectID(t *testing.T) {
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateSkipped,
		mode:        delegatepolicy.ModeDryRun,
	})

	record := NewExecutor(Options{}).Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	if record.Subject() != action.Subject() {
		t.Fatalf("attempt subject = %#v, want canonical subject %#v", record.Subject(), action.Subject())
	}
}

func TestExecuteMissingEnvRefDoesNotLaunchRunner(t *testing.T) {
	called := false
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "missing-env-test",
		envRefs:     []string{"ZZ_TOKEN", "AA_TOKEN"},
	})
	executor := NewExecutor(Options{
		LookupEnv: func(name string) (string, bool) { return "", false },
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			called = true
			return subprocess.CommandResult{}
		},
	})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	if called {
		t.Fatal("runner was called for missing env ref")
	}
	if record.RunnerInvoked() {
		t.Fatal("missing env ref reported reaching the runner boundary")
	}
	if record.Status() != AttemptFailed || record.Reason() != ReasonMissingEnvRef {
		t.Fatalf("record = %#v, want missing env failure", record)
	}
	if !strings.Contains(record.ErrorDetail(), "AA_TOKEN, ZZ_TOKEN") {
		t.Fatalf("error detail = %q, want sorted missing env names", record.ErrorDetail())
	}
}

func TestExecuteMapsMultipleChildNamesFromOneHostSource(t *testing.T) {
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "mapped-env-test",
		env: map[string]string{
			"API_TOKEN":    "HOST_TOKEN",
			"SECOND_TOKEN": "HOST_TOKEN",
		},
	})
	lookups := 0
	executor := NewExecutor(Options{
		LookupEnv: func(name string) (string, bool) {
			lookups++
			if name != "HOST_TOKEN" {
				t.Fatalf("LookupEnv name = %q, want HOST_TOKEN", name)
			}
			return "secret-value", true
		},
		Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			if !envContains(request.Env, "API_TOKEN=secret-value") ||
				!envContains(request.Env, "SECOND_TOKEN=secret-value") {
				t.Fatalf("child env = %#v, want both mapped child names", request.Env)
			}
			if envContains(request.Env, "HOST_TOKEN=secret-value") {
				t.Fatalf("child env = %#v, host source name leaked as an undeclared child name", request.Env)
			}
			return subprocess.CommandResult{}
		},
	})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	if !record.RunnerInvoked() {
		t.Fatal("mapped environment attempt did not report reaching the runner boundary")
	}
	if record.Status() != AttemptSucceeded || record.Reason() != ReasonNone {
		t.Fatalf("record = %#v, want successful mapped environment attempt", record)
	}
	if lookups != 2 {
		t.Fatalf("host source lookups = %d, want one resolution per child binding", lookups)
	}
}

func TestExecuteReportsSharedMissingHostSourceOnce(t *testing.T) {
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "mapped-env-missing-test",
		env: map[string]string{
			"API_TOKEN":    "HOST_TOKEN",
			"SECOND_TOKEN": "HOST_TOKEN",
		},
	})
	executor := NewExecutor(Options{
		LookupEnv: func(string) (string, bool) { return "", false },
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			t.Fatal("runner called with missing shared host source")
			return subprocess.CommandResult{}
		},
	})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	if record.Reason() != ReasonMissingEnvRef ||
		!strings.Contains(record.ErrorDetail(), "missing env refs: HOST_TOKEN") ||
		strings.Contains(record.ErrorDetail(), "HOST_TOKEN, HOST_TOKEN") {
		t.Fatalf("record = %#v detail=%q, want one missing HOST_TOKEN report", record, record.ErrorDetail())
	}
}

func TestExecuteClassifiesMissingRunner(t *testing.T) {
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "definitely-missing-daem-test-runner",
	})
	executor := NewExecutor(Options{
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{MissingRunner: true, Err: errors.New("runner not found")}
		},
	})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	if record.Status() != AttemptFailed || record.Reason() != ReasonMissingRunner {
		t.Fatalf("record = %#v, want missing runner failure", record)
	}
	if !strings.Contains(record.ErrorDetail(), "runner not found") {
		t.Fatalf("error detail = %q, want runner detail", record.ErrorDetail())
	}
}

func TestExecuteClassifiesNonZeroExit(t *testing.T) {
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "nonzero-test",
	})
	executor := NewExecutor(Options{
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{
				Stdout:      "started",
				Stderr:      "failed",
				ExitCode:    7,
				HasExitCode: true,
				Err:         errors.New("exit status 7"),
			}
		},
	})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	exitCode, ok := record.ExitCode()
	if record.Status() != AttemptFailed ||
		record.Reason() != ReasonNonZeroExit ||
		!ok ||
		exitCode != 7 {
		t.Fatalf("record = %#v, exit=(%d,%t); want nonzero exit 7", record, exitCode, ok)
	}
	if record.Stdout() != "started" || record.Stderr() != "failed" {
		t.Fatalf("captured output = %q/%q, want started/failed", record.Stdout(), record.Stderr())
	}
}

func TestExecuteClassifiesTimeout(t *testing.T) {
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "timeout-test",
	})
	executor := NewExecutor(Options{
		Timeout: time.Millisecond,
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			<-ctx.Done()
			return subprocess.CommandResult{TimedOut: true, Err: ctx.Err()}
		},
	})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	if record.Status() != AttemptFailed ||
		record.Reason() != ReasonTimeout ||
		!record.TimedOut() {
		t.Fatalf("record = %#v, want timeout failure", record)
	}
}

func TestExecuteFailsClosedWithoutWorkingDirectoryAuthority(t *testing.T) {
	called := false
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "workdir-authority-test",
	})
	executor := NewExecutor(Options{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			called = true
			return subprocess.CommandResult{}
		},
	})

	record := executor.Execute(context.Background(), action, nil)

	if called {
		t.Fatal("runner called without working-directory authority")
	}
	if record.RunnerInvoked() {
		t.Fatal("workdir authority failure reported reaching the runner boundary")
	}
	if record.Status() != AttemptFailed ||
		record.Reason() != ReasonWorkDirAuthority ||
		record.ProcessReason() != subprocess.CommandReasonNone ||
		!record.WorkDirAuthorityFailed() ||
		!strings.Contains(record.ErrorDetail(), "working-directory binding is required") {
		t.Fatalf("record = %#v detail=%q, want workdir authority failure", record, record.ErrorDetail())
	}
}

func TestExecutePreservesTimeoutAcrossPostAttemptAuthorityLoss(t *testing.T) {
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "timeout-authority-test",
	})
	root := t.TempDir()
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	binding := &driftingDelegateWorkingDirectory{directory: directory}
	executor := NewExecutor(Options{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{
				Started:  true,
				TimedOut: true,
				Err:      context.DeadlineExceeded,
			}
		},
	})

	record := executor.Execute(context.Background(), action, func() (subprocess.WorkingDirectoryBinding, error) {
		return binding, nil
	})
	if record.Status() != AttemptFailed ||
		record.Reason() != ReasonWorkDirAuthority ||
		record.ProcessReason() != subprocess.CommandReasonTimeout ||
		!record.WorkDirAuthorityFailed() ||
		!record.TimedOut() {
		t.Fatalf("record = %#v, want independent timeout and workdir authority facts", record)
	}
}

type driftingDelegateWorkingDirectory struct {
	directory   *os.File
	validations int
}

func (binding *driftingDelegateWorkingDirectory) Validate() error {
	binding.validations++
	if binding.validations > 1 {
		return errors.New("injected post-attempt authority loss")
	}
	return nil
}

func (binding *driftingDelegateWorkingDirectory) OpenDirectory() (*os.File, error) {
	return os.Open(binding.directory.Name())
}

func (binding *driftingDelegateWorkingDirectory) Close() error {
	return binding.directory.Close()
}

func TestExecuteBoundsRunnerOutput(t *testing.T) {
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "truncate-test",
	})
	executor := NewExecutor(Options{
		OutputLimit: 4,
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{Stdout: "abcdef", Stderr: "uvwxyz"}
		},
	})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	if record.Status() != AttemptSucceeded {
		t.Fatalf("status = %q, want succeeded", record.Status())
	}
	if !record.StdoutTruncated() || !record.StderrTruncated() {
		t.Fatalf("truncation flags = %t/%t, want both true", record.StdoutTruncated(), record.StderrTruncated())
	}
	if record.Stdout() != "abcd\n[truncated]" || record.Stderr() != "uvwx\n[truncated]" {
		t.Fatalf("bounded output = %q/%q", record.Stdout(), record.Stderr())
	}
}

func TestExecuteSkippedActionDoesNotLaunchRunner(t *testing.T) {
	called := false
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateSkipped,
		mode:        delegatepolicy.ModeDryRun,
		command:     "skip-test",
	})
	executor := NewExecutor(Options{
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			called = true
			return subprocess.CommandResult{}
		},
	})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	if called {
		t.Fatal("runner was called for skipped action")
	}
	if record.Status() != AttemptSkipped || record.Reason() != ReasonNotScheduled {
		t.Fatalf("record = %#v, want skipped not-scheduled record", record)
	}
}

func TestExecuteAllContinuesAfterFailureAndReturnsAllAttemptRecords(t *testing.T) {
	failingAction := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "first-delegate",
	})
	succeedingAction := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "second-delegate",
	})
	commands := make([]string, 0, 2)
	executor := NewExecutor(Options{
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			commands = append(commands, request.Command)
			if request.Command == "first-delegate" {
				return subprocess.CommandResult{
					ExitCode:    2,
					HasExitCode: true,
					Err:         errors.New("exit status 2"),
				}
			}
			return subprocess.CommandResult{}
		},
	})

	record, err := executeAllForTest(
		context.Background(),
		executor,
		[]reconcile.DelegateAction{failingAction, succeedingAction},
		testWorkingDirectoryBinderForAction(t),
	)

	if err == nil {
		t.Fatal("ExecuteAll error = nil, want failure after executing all actions")
	}
	if len(commands) != 2 || commands[0] != "first-delegate" || commands[1] != "second-delegate" {
		t.Fatalf("commands = %#v, want both actions in order", commands)
	}
	if len(record) != 2 ||
		record[0].Reason() != ReasonNonZeroExit ||
		record[1].Status() != AttemptSucceeded {
		t.Fatalf("record = %#v, want failure and success rows", record)
	}
}

func TestExecuteAllReportsBlockedActionAsError(t *testing.T) {
	action := testAction(t, testActionInput{
		disposition:     reconcile.DelegateBlocked,
		mode:            delegatepolicy.ModeApply,
		command:         "blocked-test",
		runnerReadiness: delegatepolicy.RunnerMissing,
	})
	executor := NewExecutor(Options{})

	record, err := executeAllForTest(
		context.Background(),
		executor,
		[]reconcile.DelegateAction{action},
		testWorkingDirectoryBinderForAction(t),
	)

	if err == nil {
		t.Fatal("ExecuteAll error = nil, want blocked execution error")
	}
	if len(record) != 1 ||
		record[0].Status() != AttemptBlocked ||
		record[0].Reason() != ReasonPolicyBlocked {
		t.Fatalf("record = %#v, want blocked policy record", record)
	}
	var executionErr ExecutionError
	if !errors.As(err, &executionErr) || len(executionErr.AttemptRecords()) != 1 {
		t.Fatalf("err = %v, want ExecutionError with one record row", err)
	}
}

func TestExecuteAllSelectsWorkingDirectoryAuthorityPerAction(t *testing.T) {
	actions := []reconcile.DelegateAction{
		testAction(t, testActionInput{
			disposition: reconcile.DelegateScheduled,
			mode:        delegatepolicy.ModeApply,
			command:     "first",
		}),
		testAction(t, testActionInput{
			disposition: reconcile.DelegateScheduled,
			mode:        delegatepolicy.ModeApply,
			command:     "second",
		}),
	}
	runnerCalls := 0
	executor := NewExecutor(Options{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			runnerCalls++
			return subprocess.CommandResult{}
		},
	})
	binderCalls := 0
	firstBinder := testWorkingDirectoryBinder(t)
	bindForAction := func(reconcile.DelegateAction) subprocess.WorkingDirectoryBinder {
		binderCalls++
		if binderCalls == 1 {
			return firstBinder
		}
		return nil
	}

	records, err := executeAllForTest(
		context.Background(),
		executor,
		actions,
		bindForAction,
	)

	if err == nil {
		t.Fatal("ExecuteAll error = nil, want second action authority failure")
	}
	if binderCalls != 2 {
		t.Fatalf("binder selections = %d, want one per action", binderCalls)
	}
	if runnerCalls != 1 {
		t.Fatalf("runner calls = %d, want only first action", runnerCalls)
	}
	if records[0].Status() != AttemptSucceeded ||
		records[1].Reason() != ReasonWorkDirAuthority {
		t.Fatalf("records = %#v, want success then workdir authority failure", records)
	}
}

func executeAllForTest(
	ctx context.Context,
	executor Executor,
	actions []reconcile.DelegateAction,
	bindForAction BinderForAction,
) ([]AttemptRecord, error) {
	records := make([]AttemptRecord, 0, len(actions))
	for _, action := range actions {
		var bind subprocess.WorkingDirectoryBinder
		if bindForAction != nil {
			bind = bindForAction(action)
		}
		records = append(records, executor.Execute(ctx, action, bind))
	}
	return records, FailedAttemptError(records)
}
