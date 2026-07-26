package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	reconcilehostroute "github.com/isty2e/daem/internal/reconcile/build/hostroute"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
	"github.com/isty2e/daem/internal/subprocess"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestRunKeepsProjectionWhenDelegateAttemptFails(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	selection := applyMCPSelection(t)
	serverID := "context7"
	command := "must-not-run-daem-test"
	args := []string{"--serve", "context7"}
	resources := applyMCPEnvironment(
		t,
		serverID,
		targetpkg.TargetClaudeCode,
		command,
		args,
		map[string]string{"API_TOKEN": "DAEM_TEST_TOKEN"},
	)
	locked, canonical := applyMCPLockfile(t, serverID, command, args)
	assessment := buildAggregateApplyAssessment(t, paths, resources, locked, selection, false)
	delegateActions, err := reconcilehostroute.BuildDelegateActions(reconcilehostroute.DelegateInput{
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
	assessment = assessmentWithDelegates(t, assessment, reconcile.ContextApply, delegateActions)
	runnerCalled := false
	machinePath := filepath.Join(tempDir, "host-cache", "context7")

	result, err := runWithOptions(context.Background(), paths, resources, locked, selection, assessment, applyDelegateRunOptions(t, paths, runOptions{
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			LookupEnv: func(name string) (string, bool) {
				if name == "DAEM_TEST_TOKEN" {
					return "secret", true
				}
				return "", false
			},
			Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
				runnerCalled = true
				if _, statErr := os.Stat(filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath)); statErr != nil {
					t.Fatalf("delegate runner invoked before projection write: %v", statErr)
				}
				return subprocess.CommandResult{
					ExitCode:    9,
					HasExitCode: true,
					Stdout:      "token=secret cache=" + machinePath,
					Stderr:      "api_key=secret workdir=" + machinePath,
					Err:         errors.New("secret=secret path=" + machinePath),
				}
			},
		}),
	}))

	if err == nil {
		t.Fatal("runWithOptions error = nil, want delegate attempt failure")
	}
	if !runnerCalled {
		t.Fatal("delegate runner was not called")
	}
	if result.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want projection action recorded", result.ActionCount)
	}
	if len(result.DelegateAttempts) != 1 ||
		result.DelegateAttempts[0].Attempt().Reason() != delegate.ReasonNonZeroExit {
		t.Fatalf("delegate attempt record = %#v, want one nonzero exit row", result.DelegateAttempts)
	}
	currentAttempt := result.DelegateAttempts[0].Attempt()
	if !strings.Contains(currentAttempt.Stdout(), machinePath) ||
		!strings.Contains(currentAttempt.Stderr(), machinePath) ||
		!strings.Contains(currentAttempt.ErrorDetail(), machinePath) {
		t.Fatal("operation-local attempt did not retain the machine path needed to exercise durable exclusion")
	}
	mcpConfigPath := filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath)
	assertApplyMCPConfigEquivalent(t, mcpConfigPath, serverID, canonical)
	state := loadApplyStatefile(t, paths.StatefilePath)
	assertApplyMCPStateSubject(t, state, serverID, canonical)
	assertApplyMCPDelegateAttempts(
		t,
		state,
		delegateActions[0],
		durableattempt.DelegateStatusFailed,
		durableattempt.DelegateReasonNonZeroExit,
		observerelation.ObservationPresent,
		true,
	)
	record := state.DelegateAttempts()[0]
	exitCode, hasExitCode := record.ExitCode()
	if !hasExitCode || exitCode != 9 {
		t.Fatalf("delegate attempt record exit code = %d present=%t, want 9", exitCode, hasExitCode)
	}
	if !record.Redacted() {
		t.Fatalf("persisted delegate attempt record = %#v, want redaction fact", record)
	}
	stateContent, err := os.ReadFile(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"token=", "api_key=", "runner failed with secret", machinePath,
		`"stdout"`, `"stderr"`, `"error_detail"`,
	} {
		if strings.Contains(string(stateContent), forbidden) {
			t.Fatalf("statefile retained transient delegate diagnostic %q:\n%s", forbidden, stateContent)
		}
	}
	assertNoApplyRecoveryArtifacts(t, paths)
}

func TestRunPersistsDelegateFailureAttemptsForMissingEnvAndTimeout(t *testing.T) {
	tests := []struct {
		name             string
		executor         func(*testing.T, *bool) delegate.Executor
		wantReason       durableattempt.DelegateAttemptReason
		wantRunnerCalled bool
	}{
		{
			name: "missing env ref",
			executor: func(t *testing.T, runnerCalled *bool) delegate.Executor {
				t.Helper()
				return delegate.NewExecutor(delegate.Options{
					LookupEnv: func(name string) (string, bool) {
						return "", false
					},
					Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
						*runnerCalled = true
						return subprocess.CommandResult{}
					},
				})
			},
			wantReason: durableattempt.DelegateReasonMissingEnvRef,
		},
		{
			name: "timeout",
			executor: func(t *testing.T, runnerCalled *bool) delegate.Executor {
				t.Helper()
				return delegate.NewExecutor(delegate.Options{
					Timeout: time.Millisecond,
					LookupEnv: func(name string) (string, bool) {
						return "safe", true
					},
					Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
						*runnerCalled = true
						<-ctx.Done()
						return subprocess.CommandResult{TimedOut: true, Err: ctx.Err()}
					},
				})
			},
			wantReason:       durableattempt.DelegateReasonTimeout,
			wantRunnerCalled: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			paths := applyTestPaths(t, tempDir)
			selection := applyMCPSelection(t)
			command := "must-not-run-daem-test"
			args := []string{"--serve", "context7"}
			resources := applyMCPEnvironment(
				t, "context7", targetpkg.TargetClaudeCode, command, args,
				map[string]string{"API_TOKEN": "DAEM_TEST_TOKEN"},
			)
			locked, _ := applyMCPLockfile(t, "context7", command, args)
			assessment := buildAggregateApplyAssessment(t, paths, resources, locked, selection, false)
			delegateActions, err := reconcilehostroute.BuildDelegateActions(reconcilehostroute.DelegateInput{
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
			assessment = assessmentWithDelegates(t, assessment, reconcile.ContextApply, delegateActions)

			runnerCalled := false
			_, err = runWithOptions(
				context.Background(),
				paths,
				resources,
				locked,
				selection,
				assessment,
				applyDelegateRunOptions(t, paths, runOptions{DelegateExecutor: test.executor(t, &runnerCalled)}),
			)
			if err == nil {
				t.Fatal("runWithOptions returned nil error, want delegate failure")
			}
			if runnerCalled != test.wantRunnerCalled {
				t.Fatalf("runnerCalled = %t, want %t", runnerCalled, test.wantRunnerCalled)
			}
			state := loadApplyStatefile(t, paths.StatefilePath)
			assertApplyMCPDelegateAttempts(
				t,
				state,
				delegateActions[0],
				durableattempt.DelegateStatusFailed,
				test.wantReason,
				observerelation.ObservationPresent,
				true,
			)
		})
	}
}

func TestRunPersistsDelegateAttemptsWhenProjectionAlreadyConverged(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	selection := applyMCPSelection(t)
	command := "must-not-run-daem-test"
	args := []string{"--serve", "context7"}
	resources := applyMCPEnvironment(
		t, "context7", targetpkg.TargetClaudeCode, command, args,
		map[string]string{"API_TOKEN": "DAEM_TEST_TOKEN"},
	)
	locked, _ := applyMCPLockfile(t, "context7", command, args)
	if _, err := run(
		t,
		context.Background(),
		paths,
		resources,
		locked,
		selection,
		buildAggregateApplyAssessment(t, paths, resources, locked, selection, false),
	); err != nil {
		t.Fatalf("initial projection run: %v", err)
	}
	assessment := buildAggregateApplyAssessment(t, paths, resources, locked, selection, false)
	delegateActions, err := reconcilehostroute.BuildDelegateActions(reconcilehostroute.DelegateInput{
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
	assessment = assessmentWithDelegates(t, assessment, reconcile.ContextApply, delegateActions)

	result, err := runWithOptions(
		context.Background(),
		paths,
		resources,
		locked,
		selection,
		assessment,
		applyDelegateRunOptions(t, paths, runOptions{
			DelegateExecutor: delegate.NewExecutor(delegate.Options{
				LookupEnv: func(name string) (string, bool) {
					return "safe", true
				},
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					return subprocess.CommandResult{Stdout: "installed"}
				},
			}),
		}),
	)
	if err != nil {
		t.Fatalf("runWithOptions returned error: %v", err)
	}
	if result.ActionCount != 0 {
		t.Fatalf("ActionCount = %d, want no projection mutation", result.ActionCount)
	}
	state := loadApplyStatefile(t, paths.StatefilePath)
	if len(state.ManagedAggregates()) != 1 {
		t.Fatalf("managed aggregates = %#v, want converged projection ownership", state.ManagedAggregates())
	}
	assertApplyMCPDelegateAttempts(
		t,
		state,
		delegateActions[0],
		durableattempt.DelegateStatusSucceeded,
		durableattempt.DelegateReasonNone,
		observerelation.ObservationPresent,
		true,
	)
}

func TestRunPersistsBlockedDelegateAttemptsWithoutRunnerLaunch(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	selection := applyMCPSelection(t)
	command := "must-not-run-daem-test"
	args := []string{"--serve", "context7"}
	resources := applyMCPEnvironment(
		t, "context7", targetpkg.TargetClaudeCode, command, args,
		map[string]string{"API_TOKEN": "DAEM_TEST_TOKEN"},
	)
	locked, _ := applyMCPLockfile(t, "context7", command, args)
	assessment := buildAggregateApplyAssessment(t, paths, resources, locked, selection, false)
	delegateActions, err := reconcilehostroute.BuildDelegateActions(reconcilehostroute.DelegateInput{
		Locked:          locked,
		SelectedTargets: applySelectedTargets(t, selection),
		Context:         reconcile.ContextApply,
		Readiness: []reconcilehostroute.DelegateReadinessFact{
			{
				Subject: locked.Locked.Subjects()[0].SubjectID(),
				Runner:  delegatepolicy.RunnerMissing,
			},
		},
	})
	if err != nil {
		t.Fatalf("Build delegate actions: %v", err)
	}
	assessment = assessmentWithDelegates(t, assessment, reconcile.ContextApply, delegateActions)

	runnerCalled := false
	_, err = runWithOptions(
		context.Background(),
		paths,
		resources,
		locked,
		selection,
		assessment,
		applyDelegateRunOptions(t, paths, runOptions{
			DelegateExecutor: delegate.NewExecutor(delegate.Options{
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					runnerCalled = true
					return subprocess.CommandResult{}
				},
			}),
		}),
	)
	if err == nil {
		t.Fatal("runWithOptions returned nil error, want blocked delegate error")
	}
	if runnerCalled {
		t.Fatal("runner was called for blocked delegate action")
	}
	state := loadApplyStatefile(t, paths.StatefilePath)
	assertApplyMCPDelegateAttempts(
		t,
		state,
		delegateActions[0],
		durableattempt.DelegateStatusBlocked,
		durableattempt.DelegateReasonPolicyBlocked,
		observerelation.ObservationPresent,
		true,
	)
}

func assertApplyMCPDelegateAttempts(
	t *testing.T,
	snapshot durable.Snapshot,
	action reconcile.DelegateAction,
	status durableattempt.DelegateAttemptStatus,
	reason durableattempt.DelegateAttemptReason,
	observation observerelation.ObservationSummary,
	matchesPlanIdentity bool,
) {
	t.Helper()

	attempts := snapshot.DelegateAttempts()
	if len(attempts) != 1 {
		t.Fatalf("delegate attempt record = %#v, want one row", attempts)
	}
	record := attempts[0]
	subject := action.Subject()
	if record.Subject() != subject ||
		record.Target() != action.Target() ||
		record.Scope() != action.Scope() ||
		record.PlanIdentityKey() != action.PlanIdentity().IdentityKey ||
		record.Status() != status ||
		record.Reason() != reason ||
		record.ObservationSummary() != observation ||
		record.PostconditionSummary() != observerelation.PostconditionNotObserved {
		t.Fatalf(
			"delegate attempt record = %#v, want %s/%s %q status=%q reason=%q identity=%q",
			record,
			subject.Kind(),
			subject.Namespace(),
			subject.Key(),
			status,
			reason,
			action.PlanIdentity().IdentityKey,
		)
	}
	if record.MatchesPlanIdentity(action.PlanIdentity().IdentityKey) != matchesPlanIdentity {
		t.Fatalf(
			"MatchesPlanIdentity = %t, want %t for %#v",
			record.MatchesPlanIdentity(action.PlanIdentity().IdentityKey),
			matchesPlanIdentity,
			record,
		)
	}
}
