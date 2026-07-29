package clipresent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
	"github.com/isty2e/daem/internal/subprocess"

	surfacedelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestPrintDelegateAttemptsMarksHistoryOnlyWithoutRawOutput(t *testing.T) {
	attempt := delegatePresentationAttempt(t, subprocess.CommandResult{
		Stdout: "installed and ready",
		Stderr: "success text from host",
	})

	var stdout bytes.Buffer
	PrintDelegateAttemptsWithOptions(&stdout, []DelegateAttemptInput{attempt}, HumanOptions{Verbose: true})
	rendered := stdout.String()

	for _, want := range []string{
		"history-only diagnostics",
		"evidence=last_attempt_diagnostics",
		"authority=history_only",
		"status=succeeded",
		"reason=none",
		"observation=not_observed",
		"postcondition=not_observed",
		"output_observed=true",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("delegate attempt output = %q, want %q", rendered, want)
		}
	}
	for _, forbidden := range []string{
		"installed",
		"ready",
		"converged",
		"success text",
		"%!(EXTRA",
		"stdout=",
		"stderr=",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("delegate attempt output = %q, want no %q", rendered, forbidden)
		}
	}
}

func TestPrintApplyResultJSONDelegateAttemptsAreHistoryOnlyDiagnostics(t *testing.T) {
	attempt := delegatePresentationAttempt(t, subprocess.CommandResult{
		Stdout:      `api_key="api-token-canary" host says installed`,
		Stderr:      "password=api-token-canary host says ready",
		ExitCode:    7,
		HasExitCode: true,
		Err:         errors.New("token=api-token-canary failed"),
	})

	var stdout bytes.Buffer
	err := PrintApplyResultJSON(&stdout, ApplyResultJSONInput{
		StatefilePath:    "/repo/.daem/state.json",
		DelegateAttempts: []DelegateAttemptInput{attempt},
	})
	if err != nil {
		t.Fatalf("PrintApplyResultJSON returned error: %v", err)
	}

	for _, forbidden := range []string{
		`"stdout"`,
		`"stderr"`,
		`"error_detail"`,
		"api-token-canary",
		"host says installed",
		"host says ready",
	} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("apply result json = %s, want no raw output marker %q", stdout.String(), forbidden)
		}
	}

	var payload struct {
		SchemaVersion    int `json:"schema_version"`
		DelegateAttempts []struct {
			EvidenceKind        string `json:"evidence_kind"`
			Authority           string `json:"authority"`
			Status              string `json:"status"`
			Reason              string `json:"reason"`
			Observation         string `json:"observation"`
			Postcondition       string `json:"postcondition"`
			ExitCode            *int   `json:"exit_code"`
			OutputObserved      bool   `json:"output_observed"`
			OutputTruncated     bool   `json:"output_truncated"`
			RunnerErrorObserved bool   `json:"runner_error_observed"`
			Redacted            bool   `json:"redacted"`
		} `json:"delegate_attempts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode apply result json: %v", err)
	}
	if len(payload.DelegateAttempts) != 1 {
		t.Fatalf("delegate_attempts = %#v, want one row", payload.DelegateAttempts)
	}
	if payload.SchemaVersion != 14 {
		t.Fatalf("schema_version = %d, want 14 after extension-order disclosure", payload.SchemaVersion)
	}
	got := payload.DelegateAttempts[0]
	if got.EvidenceKind != "last_attempt_diagnostics" ||
		got.Authority != "history_only" ||
		got.Status != "failed" ||
		got.Reason != "nonzero_exit" ||
		got.Observation != "not_observed" ||
		got.Postcondition != "not_observed" ||
		got.ExitCode == nil ||
		*got.ExitCode != 7 ||
		!got.OutputObserved ||
		!got.OutputTruncated ||
		!got.RunnerErrorObserved ||
		!got.Redacted {
		t.Fatalf("delegate attempt json = %#v, want history-only failed diagnostics", got)
	}
}

func TestPrintApplyResultJSONDelegateTimeoutDoesNotClaimReadiness(t *testing.T) {
	attempt := delegatePresentationAttempt(t, subprocess.CommandResult{
		TimedOut: true,
		Err:      context.DeadlineExceeded,
	})

	var stdout bytes.Buffer
	err := PrintApplyResultJSON(&stdout, ApplyResultJSONInput{
		StatefilePath:    "/repo/.daem/state.json",
		DelegateAttempts: []DelegateAttemptInput{attempt},
	})
	if err != nil {
		t.Fatalf("PrintApplyResultJSON returned error: %v", err)
	}

	for _, forbidden := range []string{
		`"ready"`,
		`"current"`,
		`"converged"`,
		`"installed"`,
	} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("apply result json = %s, want no readiness scalar %q", stdout.String(), forbidden)
		}
	}

	var payload struct {
		DelegateAttempts []struct {
			EvidenceKind        string `json:"evidence_kind"`
			Authority           string `json:"authority"`
			Status              string `json:"status"`
			Reason              string `json:"reason"`
			Observation         string `json:"observation"`
			Postcondition       string `json:"postcondition"`
			TimedOut            bool   `json:"timed_out"`
			RunnerErrorObserved bool   `json:"runner_error_observed"`
		} `json:"delegate_attempts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode apply result json: %v", err)
	}
	if len(payload.DelegateAttempts) != 1 {
		t.Fatalf("delegate_attempts = %#v, want one row", payload.DelegateAttempts)
	}
	got := payload.DelegateAttempts[0]
	if got.EvidenceKind != "last_attempt_diagnostics" ||
		got.Authority != "history_only" ||
		got.Status != "failed" ||
		got.Reason != "timeout" ||
		got.Observation != "not_observed" ||
		got.Postcondition != "not_observed" ||
		!got.TimedOut ||
		!got.RunnerErrorObserved {
		t.Fatalf("delegate attempt json = %#v, want history-only timeout diagnostics", got)
	}
}

func TestPrintPlanJSONIncludesPlannedDelegateAction(t *testing.T) {
	action := delegatePresentationAction(t)
	reconciliation, err := reconcile.NewResult(reconcile.ResultInput{
		Context:   reconcile.ContextApply,
		Delegates: []reconcile.DelegateAction{action},
	})
	if err != nil {
		t.Fatalf("NewResult returned error: %v", err)
	}

	var stdout bytes.Buffer
	if err := PrintPlanJSON(&stdout, PlanJSONInput{
		Command:        "apply",
		Mode:           "write",
		Reconciliation: reconciliation,
	}); err != nil {
		t.Fatalf("PrintPlanJSON returned error: %v", err)
	}

	var payload struct {
		DelegateActions []struct {
			Subject struct {
				Kind      string `json:"kind"`
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"subject"`
			Status           string `json:"status"`
			PolicyOutcome    string `json:"policy_outcome"`
			SchedulesAttempt bool   `json:"schedules_attempt"`
			Command          string `json:"command"`
		} `json:"delegate_actions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode plan JSON: %v", err)
	}
	if len(payload.DelegateActions) != 1 {
		t.Fatalf("delegate_actions = %#v, want one planned action", payload.DelegateActions)
	}
	got := payload.DelegateActions[0]
	if got.Subject.Kind != "projection" ||
		got.Subject.Namespace != "claude-code.project.mcp-server" ||
		got.Subject.Name != "context7" ||
		got.Status != "scheduled" ||
		got.PolicyOutcome != "allow" ||
		!got.SchedulesAttempt ||
		got.Command != "npx" {
		t.Fatalf("delegate action = %#v, want scheduled Claude project MCP attempt", got)
	}
}

func delegatePresentationAttempt(t *testing.T, result subprocess.CommandResult) DelegateAttemptInput {
	t.Helper()
	action := delegatePresentationAction(t)
	executor := delegate.NewExecutor(delegate.Options{
		OutputLimit: 4,
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			return result
		},
	})
	attempt := executor.Execute(context.Background(), action, delegatePresentationWorkingDirectoryBinder(t))
	return DelegateAttemptInput{
		Attempt:       attempt,
		Observation:   observerelation.ObservationNotObserved,
		Postcondition: observerelation.PostconditionNotObserved,
	}
}

func delegatePresentationAction(t *testing.T) reconcile.DelegateAction {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "claude-code.project.mcp-server", "context7")
	if err != nil {
		t.Fatalf("ProjectionSubjectID returned error: %v", err)
	}
	plan := delegatePresentationPlan(t)
	decision, err := delegatepolicy.Evaluate(delegatepolicy.Input{
		Plan:   plan,
		Mode:   delegatepolicy.ModeApply,
		Runner: delegatepolicy.RunnerAvailable,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	action, err := reconcile.NewDelegateAction(reconcile.DelegateActionInput{
		Subject:     subject,
		Target:      target.TargetClaudeCode,
		Scope:       target.ScopeProject,
		Plan:        plan,
		Disposition: reconcile.DelegateScheduled,
		Risks:       decision.Risks(),
	})
	if err != nil {
		t.Fatalf("NewDelegateAction returned error: %v", err)
	}
	return action
}

func delegatePresentationPlan(t *testing.T) surfacedelegate.DelegatePlan {
	t.Helper()
	runner, err := surfacedelegate.NewRunner(surfacedelegate.RunnerPlain)
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	command, err := surfacedelegate.NewCommandSpec("npx", []string{"-y", "@upstash/context7-mcp"})
	if err != nil {
		t.Fatalf("NewCommandSpec returned error: %v", err)
	}
	env, err := surfacedelegate.NewEnvBindingSet(nil)
	if err != nil {
		t.Fatalf("NewEnvBindingSet returned error: %v", err)
	}
	plan, err := surfacedelegate.NewDelegatePlan(surfacedelegate.DelegatePlanSpec{
		Runner:    runner,
		Command:   command,
		Env:       env,
		PinPolicy: surfacedelegate.PinNotApplicable,
	})
	if err != nil {
		t.Fatalf("NewDelegatePlan returned error: %v", err)
	}
	return plan
}

func delegatePresentationWorkingDirectoryBinder(t *testing.T) subprocess.WorkingDirectoryBinder {
	t.Helper()
	root, err := rootedpath.CaptureRoot(t.TempDir())
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close delegate presentation root: %v", err)
		}
	})
	return func() (subprocess.WorkingDirectoryBinding, error) {
		return root.AcquireWorkingDirectory()
	}
}
