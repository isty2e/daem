package delegate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/topology"
)

func TestExecuteRedactsEnvSecretsAndSecretLookingFragments(t *testing.T) {
	const secret = "super-secret-value"
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "redact-test",
		envRefs:     []string{"API_TOKEN"},
	})
	executor := NewExecutor(Options{
		LookupEnv: func(name string) (string, bool) {
			if name == "API_TOKEN" {
				return secret, true
			}
			return "", false
		},
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			if !envContains(request.Env, "API_TOKEN="+secret) {
				t.Fatalf("request env missing API_TOKEN")
			}
			return subprocess.CommandResult{
				Stdout: "token=" + secret + " api_key=abc123",
				Stderr: `password: hunter2 secret="quoted secret"`,
				Err:    errors.New("runner saw " + secret),
			}
		},
	})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	combined := record.Stdout() + record.Stderr() + record.ErrorDetail()
	for _, forbidden := range []string{secret, "abc123", "hunter2", "quoted secret"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("record leaked %q in %q", forbidden, combined)
		}
	}
	if !record.Redacted() {
		t.Fatal("Redacted = false, want true")
	}
}

func TestExecuteReplacesExistingEnvRefValue(t *testing.T) {
	t.Setenv("API_TOKEN", "old-secret")
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "env-replace-test",
		envRefs:     []string{"API_TOKEN"},
	})
	executor := NewExecutor(Options{
		LookupEnv: func(name string) (string, bool) {
			if name == "API_TOKEN" {
				return "new-secret", true
			}
			return "", false
		},
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			if !envContains(request.Env, "API_TOKEN=new-secret") {
				t.Fatalf("request env missing replacement value")
			}
			if envContains(request.Env, "API_TOKEN=old-secret") {
				t.Fatalf("request env retained old secret value")
			}
			return subprocess.CommandResult{}
		},
	})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	if record.Status() != AttemptSucceeded {
		t.Fatalf("record = %#v, want success", record)
	}
}

func TestExecutionErrorSummaryDoesNotIncludeSecretOutput(t *testing.T) {
	const secret = "secret-from-env"
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "secret-failure-test",
		envRefs:     []string{"API_TOKEN"},
	})
	executor := NewExecutor(Options{
		LookupEnv: func(name string) (string, bool) {
			if name == "API_TOKEN" {
				return secret, true
			}
			return "", false
		},
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{
				Stderr:      "token=" + secret,
				ExitCode:    3,
				HasExitCode: true,
				Err:         errors.New("failed with " + secret),
			}
		},
	})

	record, err := executeAllForTest(
		context.Background(),
		executor,
		[]reconcile.DelegateAction{action},
		testWorkingDirectoryBinderForAction(t),
	)

	if err == nil {
		t.Fatal("ExecuteAll error = nil, want secret-bearing failure")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error summary leaked secret: %q", err.Error())
	}
	if len(record) != 1 || strings.Contains(record[0].Stderr()+record[0].ErrorDetail(), secret) {
		t.Fatalf("record leaked secret: %#v", record)
	}
}

func TestExecutionErrorBoundedEvidenceStopsBeforeFormattingEveryRecord(t *testing.T) {
	subject, err := topology.NewSubjectID(
		topology.SubjectResource,
		"delegate.test",
		strings.Repeat("x", 256),
	)
	if err != nil {
		t.Fatal(err)
	}
	record := AttemptRecord{
		subject: subject,
		reason:  ReasonNonZeroExit,
	}
	records := make([]AttemptRecord, 10000)
	for index := range records {
		records[index] = record
	}

	evidence, truncated := (ExecutionError{records: records}).BoundedErrorEvidence(128)
	if !truncated {
		t.Fatal("bounded evidence was not truncated")
	}
	if utf8.RuneCountInString(evidence) > 128 {
		t.Fatalf("evidence contains %d runes, want at most 128", utf8.RuneCountInString(evidence))
	}

	largeSubject, err := topology.NewSubjectID(
		topology.SubjectResource,
		"delegate.test",
		strings.Repeat("x", 1<<20),
	)
	if err != nil {
		t.Fatal(err)
	}
	evidence, truncated = (ExecutionError{records: []AttemptRecord{{
		subject: largeSubject,
		reason:  ReasonNonZeroExit,
	}}}).BoundedErrorEvidence(128)
	if !truncated || utf8.RuneCountInString(evidence) > 128 {
		t.Fatalf("large-subject evidence = %q truncated=%t", evidence, truncated)
	}
}
