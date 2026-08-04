package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestMCPPublicCLIApplyDelegatedRouteDryRunDisclosesDelegateActionWithoutAttempt(t *testing.T) {
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp"},
		Env:     map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)

	human := runMCPCLIExpect(t, 0, "delegated route dry-run human", "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--dry-run")
	for _, want := range []string{
		"delegate actions: 1 plans",
		`status=skipped`,
		`outcome=skip`,
		`schedules_attempt=false`,
		`runner=npx`,
		`command="npx"`,
		`args=["-y","@upstash/context7-mcp"]`,
		`env_bindings=[API_TOKEN<-CONTEXT7_API_TOKEN]`,
		`environment=inherit`,
		`pin=floating`,
		`timeout=30s`,
		`packages=["npm:@upstash/context7-mcp"]`,
		`dry_run_disclosure`,
		`external_store`,
		`floating_package`,
	} {
		if !strings.Contains(human, want) {
			t.Fatalf("human dry-run output = %q, want %q", human, want)
		}
	}

	jsonOut := runMCPCLIExpect(t, 0, "delegated route dry-run json", "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--dry-run", "--json")
	payload := clijson.DecodePlan(t, []byte(jsonOut))
	if len(payload.DelegateActions) != 1 {
		t.Fatalf("delegate_actions = %#v, want one dry-run disclosure", payload.DelegateActions)
	}
	action := payload.DelegateActions[0]
	assertMCPDelegateActionDisclosure(t, action, "skipped", "skip", false, "npx", spec)
	assertMCPDelegateActionRisk(t, action, "dry_run_disclosure", "info")
	assertMCPDelegateActionRisk(t, action, "external_store", "warn")
	assertMCPDelegateActionRisk(t, action, "floating_package", "warn")
	assertMCPStatefileMissing(t, project.root, "delegated route dry-run")
}

func TestMCPPublicCLIApplyDelegatedRouteDryRunDisclosesPackageBackedWarnings(t *testing.T) {
	tests := []struct {
		name          string
		spec          mcpManifestSpec
		wantRunner    string
		wantEcosystem string
		wantPackage   string
		wantSelector  string
	}{
		{
			name: "uvx floating python package",
			spec: mcpManifestSpec{
				Command: "uvx",
				Args:    []string{"context7-mcp"},
			},
			wantRunner:    "uvx",
			wantEcosystem: "python",
			wantPackage:   "context7-mcp",
		},
		{
			name: "docker latest image",
			spec: mcpManifestSpec{
				Command: "docker",
				Args:    []string{"run", "ghcr.io/acme/context7-mcp:latest"},
			},
			wantRunner:    "docker",
			wantEcosystem: "container",
			wantPackage:   "ghcr.io/acme/context7-mcp",
			wantSelector:  "latest",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project := newMCPCLIProject(t)
			writeMCPManifest(t, project.root, test.spec)
			runMCPLock(t, project)

			jsonOut := runMCPCLIExpect(t, 0, "delegated route dry-run json", "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--dry-run", "--json")
			payload := clijson.DecodePlan(t, []byte(jsonOut))
			if len(payload.DelegateActions) != 1 {
				t.Fatalf("delegate_actions = %#v, want one dry-run disclosure", payload.DelegateActions)
			}
			action := payload.DelegateActions[0]
			assertMCPDelegateActionDisclosure(t, action, "skipped", "skip", false, test.wantRunner, test.spec)
			if len(action.Packages) != 1 ||
				action.Packages[0].Ecosystem != test.wantEcosystem ||
				action.Packages[0].Name != test.wantPackage ||
				action.Packages[0].Selector != test.wantSelector ||
				action.PinPolicy != "floating" {
				t.Fatalf("delegate action packages = %#v pin=%q, want %s:%s@%s floating", action.Packages, action.PinPolicy, test.wantEcosystem, test.wantPackage, test.wantSelector)
			}
			assertMCPDelegateActionRisk(t, action, "dry_run_disclosure", "info")
			assertMCPDelegateActionRisk(t, action, "external_store", "warn")
			assertMCPDelegateActionRisk(t, action, "floating_package", "warn")
			assertMCPStatefileMissing(t, project.root, test.name)
		})
	}
}

func TestMCPPublicCLIApplyRetriesDelegateAfterCleanProjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell success fixture is not portable to Windows")
	}
	command := "success-daem-test"
	prependMCPDelegateExecutableToPath(t, command, `#!/bin/sh
printf 'ok\n'
exit 0
`)
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: command,
		Args:    []string{"--serve", "context7"},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)

	initial := runMCPCLIExpect(t, 0, "initial projection apply", "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	initialPayload := clijson.DecodeApplyResult(t, []byte(initial))
	if len(initialPayload.DelegateActions) != 1 || len(initialPayload.DelegateAttempts) != 1 {
		t.Fatalf("ordinary apply delegate fields = %#v/%#v, want scheduled attempt", initialPayload.DelegateActions, initialPayload.DelegateAttempts)
	}
	assertMCPDelegateAttemptJSON(t, initialPayload.DelegateAttempts[0], "succeeded", "none")

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply success exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	if payload.ActionCount != 0 || payload.HasErrors {
		t.Fatalf("payload = %#v, want clean projection plus attempt-only result", payload)
	}
	if len(payload.DelegateActions) != 1 || len(payload.DelegateAttempts) != 1 {
		t.Fatalf("delegate json = %#v/%#v, want one action and one attempt", payload.DelegateActions, payload.DelegateAttempts)
	}
	assertMCPDelegateActionDisclosure(t, payload.DelegateActions[0], "scheduled", "allow", true, "plain", spec)
	assertMCPDelegateAttemptJSON(t, payload.DelegateAttempts[0], "succeeded", "none")
	state := loadMCPStatefile(t, project.root)
	assertSingleMCPDelegateAttempt(t, state, durableattempt.DelegateStatusSucceeded, durableattempt.DelegateReasonNone)
}

func TestMCPPublicCLIApplyDelegatedRouteHumanWriteSummarizesAttemptOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell success fixture is not portable to Windows")
	}
	const hiddenOutput = "hidden-command-output"
	command := "human-success-daem-test"
	prependMCPDelegateExecutableToPath(t, command, `#!/bin/sh
printf '`+hiddenOutput+`\n'
exit 0
`)
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: command,
		Args:    []string{"--serve", "context7"},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)
	runMCPCLIWithSuccessfulDelegate(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply human success exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	for _, want := range []string{
		"applied: 0 actions",
		"delegate attempts: 1 history-only diagnostics",
		"evidence=last_attempt_diagnostics",
		"authority=history_only",
		`status=succeeded`,
		`reason=none`,
		`observation=present`,
		`postcondition=not_observed`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("human write output = %q, want %q", stdout, want)
		}
	}
	if strings.Contains(stdout, hiddenOutput) {
		t.Fatalf("human write output = %q, must not dump delegate stdout", stdout)
	}
}

func TestMCPPublicCLIApplyDelegatedRouteRetriesDespitePreviousSuccess(t *testing.T) {
	canary := execcheck.New(t, "retry-daem-test")
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: "retry-daem-test",
		Args:    []string{"--serve", "context7"},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)
	runMCPCLIWithSuccessfulDelegate(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")

	record := loadMCPDelegateStatusRecord(t, project.lockfilePath, "context7")
	delegatePlan, ok := record.DelegatePlan()
	if !ok {
		t.Fatal("locked MCP record missing delegate plan")
	}
	state := loadMCPStatefile(t, project.root)
	subject, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"claude-code.project.mcp-server",
		"context7",
	)
	if err != nil {
		t.Fatal(err)
	}
	priorAttempt, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
		Subject:         subject,
		Target:          target.TargetClaudeCode,
		Scope:           target.ScopeProject,
		PlanIdentityKey: delegatePlan.IdentityKey(),
		ObservedAt:      time.Date(2026, time.June, 30, 10, 0, 0, 0, time.UTC),
		Status:          durableattempt.DelegateStatusSucceeded,
		Reason:          durableattempt.DelegateReasonNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err = state.WithDelegateAttempts([]durableattempt.DelegateAttempt{priorAttempt})
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteStatefile(t, filepath.Join(project.root, ".daem", "state.json"), state)

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("retry apply attempt exitCode=%d stdout=%q stderr=%q, want failed attempt JSON only", exitCode, stdout, stderr)
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	if payload.ActionCount != 0 || !payload.HasErrors {
		t.Fatalf("payload = %#v, want clean projection plus failed retry attempt", payload)
	}
	if len(payload.DelegateActions) != 1 || len(payload.DelegateAttempts) != 1 {
		t.Fatalf("delegate json = %#v/%#v, want one action and one retry attempt", payload.DelegateActions, payload.DelegateAttempts)
	}
	assertMCPDelegateActionDisclosure(t, payload.DelegateActions[0], "scheduled", "allow", true, "plain", spec)
	assertMCPDelegateAttemptJSON(t, payload.DelegateAttempts[0], "failed", durableattempt.DelegateReasonNonZeroExit)
	execcheck.AssertInvoked(t, canary, "retry-daem-test")
	state = loadMCPStatefile(t, project.root)
	assertSingleMCPDelegateAttempt(t, state, durableattempt.DelegateStatusFailed, durableattempt.DelegateReasonNonZeroExit)
}

func TestMCPPublicCLIApplyDelegatedRouteTimeoutPersistsDiagnostic(t *testing.T) {
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: "timeout-daem-test",
		Args:    []string{"--serve", "context7"},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)
	runMCPCLIWithSuccessfulDelegate(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")

	exitCode, stdout, stderr := runMCPCLIWithOptions(t, []string{"apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json"}, clipkg.RunOptions{
		ApplyExecuteOptions: applyworkflow.ExecuteOptions{
			DelegateExecutor: delegate.NewExecutor(delegate.Options{
				Timeout: time.Millisecond,
				Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					<-ctx.Done()
					return subprocess.CommandResult{TimedOut: true, Err: ctx.Err()}
				},
			}),
		},
	})
	if exitCode != 1 || stderr != "" {
		t.Fatalf("timeout apply attempt exitCode=%d stdout=%q stderr=%q, want failed attempt JSON only", exitCode, stdout, stderr)
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	if payload.ActionCount != 0 || !payload.HasErrors {
		t.Fatalf("payload = %#v, want clean projection plus failed timeout attempt", payload)
	}
	if len(payload.DelegateActions) != 1 || len(payload.DelegateAttempts) != 1 {
		t.Fatalf("delegate json = %#v/%#v, want one action and one timeout attempt", payload.DelegateActions, payload.DelegateAttempts)
	}
	assertMCPDelegateActionDisclosure(t, payload.DelegateActions[0], "scheduled", "allow", true, "plain", spec)
	attempt := payload.DelegateAttempts[0]
	assertMCPDelegateAttemptJSON(t, attempt, "failed", durableattempt.DelegateReasonTimeout)
	if !attempt.TimedOut {
		t.Fatalf("delegate attempt json = %#v, want timed_out=true", attempt)
	}
	record := assertSingleMCPDelegateAttempt(t, loadMCPStatefile(t, project.root), durableattempt.DelegateStatusFailed, durableattempt.DelegateReasonTimeout)
	if !record.TimedOut() {
		t.Fatalf("delegate attempt record = %#v, want timed_out=true", record)
	}
}

func TestMCPPublicCLIOrdinaryApplyRunsAdmittedDelegate(t *testing.T) {
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: "ordinary-apply-daem-test",
		Args:    []string{"--serve", "context7"},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)

	attempts := 0
	exitCode, stdout, stderr := runMCPCLIWithOptions(t, []string{"apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json"}, clipkg.RunOptions{
		ApplyExecuteOptions: applyworkflow.ExecuteOptions{
			DelegateExecutor: delegate.NewExecutor(delegate.Options{
				Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					attempts++
					if request.Command != spec.Command {
						t.Fatalf("delegate command = %q, want %q", request.Command, spec.Command)
					}
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			}),
		},
	})
	if exitCode != 0 || stderr != "" {
		t.Fatalf("ordinary apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	if attempts != 1 || len(payload.DelegateActions) != 1 || len(payload.DelegateAttempts) != 1 {
		t.Fatalf("attempts=%d delegate json=%#v/%#v, want one ordinary attempt", attempts, payload.DelegateActions, payload.DelegateAttempts)
	}
	state := loadMCPStatefile(t, project.root)
	assertSingleMCPDelegateAttempt(t, state, durableattempt.DelegateStatusSucceeded, durableattempt.DelegateReasonNone)
}

func TestMCPPublicCLIApplyDelegatedRouteMissingRunnerPersistsDiagnostic(t *testing.T) {
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: "definitely-missing-daem-test-runner",
		Args:    []string{"--serve", "context7"},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)
	t.Setenv("PATH", t.TempDir())

	payload, state := runMCPApplyDelegatedRouteExpectFailed(t, project)

	if payload.ActionCount != 1 {
		t.Fatalf("action_count = %d, want projection committed before missing-runner diagnostic", payload.ActionCount)
	}
	assertMCPConfigEquivalent(t, project.root, "context7", spec)
	assertMCPStateSubject(t, state, "context7")
	assertMCPDelegateAttemptJSON(t, payload.DelegateAttempts[0], "failed", durableattempt.DelegateReasonMissingRunner)
	assertSingleMCPDelegateAttempt(t, state, durableattempt.DelegateStatusFailed, durableattempt.DelegateReasonMissingRunner)
}

func TestMCPPublicCLIApplyDelegatedRouteRunnerErrorPersistsDiagnostic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shebang runner-error fixture is not portable to Windows")
	}
	command := "runner-error-daem-test"
	prependMCPDelegateExecutableToPath(t, command, "#!/definitely/missing/daem-test-interpreter\n")
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: command,
		Args:    []string{"--serve", "context7"},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)

	payload, state := runMCPApplyDelegatedRouteExpectFailed(t, project)

	if payload.ActionCount != 1 {
		t.Fatalf("action_count = %d, want projection committed before runner-error diagnostic", payload.ActionCount)
	}
	assertMCPConfigEquivalent(t, project.root, "context7", spec)
	assertMCPDelegateAttemptJSON(t, payload.DelegateAttempts[0], "failed", durableattempt.DelegateReasonRunnerError)
	if len(state.DelegateAttempts()) == 0 {
		t.Fatalf("delegate attempts absent from state; apply errors = %#v", payload.Errors)
	}
	assertSingleMCPDelegateAttempt(t, state, durableattempt.DelegateStatusFailed, durableattempt.DelegateReasonRunnerError)
}

func TestMCPPublicCLIApplyDelegatedRouteRedactsPublicRouteOutputAndState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell redaction fixture is not portable to Windows")
	}
	const secretEnvName = "DAEM_TEST_PUBLIC_SECRET"
	const secretValue = "super-secret-value"
	command := "leaky-daem-test"
	prependMCPDelegateExecutableToPath(t, command, `#!/bin/sh
printf 'token=%s api_key=literal-token raw=%s\n' "$DAEM_TEST_PUBLIC_SECRET" "$DAEM_TEST_PUBLIC_SECRET"
printf 'password: hunter2\n' >&2
exit 9
`)
	t.Setenv(secretEnvName, secretValue)
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: command,
		Args:    []string{"--serve", "context7"},
		Env:     map[string]string{"API_TOKEN": secretEnvName},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)

	payload, state, stdout := runMCPApplyDelegatedRouteExpectFailedWithStdout(t, project)

	if payload.ActionCount != 1 {
		t.Fatalf("action_count = %d, want projection committed before redacted failed attempt", payload.ActionCount)
	}
	assertMCPConfigEquivalent(t, project.root, "context7", spec)
	assertMCPDelegateAttemptJSON(t, payload.DelegateAttempts[0], "failed", durableattempt.DelegateReasonNonZeroExit)
	record := assertSingleMCPDelegateAttempt(t, state, durableattempt.DelegateStatusFailed, durableattempt.DelegateReasonNonZeroExit)
	if !record.Redacted() {
		t.Fatalf("delegate attempt record = %#v, want redacted=true", record)
	}
	assertMCPDelegateNoSecretLeak(t, "apply json stdout", stdout)
	stateContent, err := os.ReadFile(filepath.Join(project.root, ".daem", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertMCPDelegateNoSecretLeak(t, "statefile", string(stateContent))
	for _, forbidden := range []string{`"stdout"`, `"stderr"`, `"error_detail"`} {
		if strings.Contains(string(stateContent), forbidden) {
			t.Fatalf("statefile retained transient delegate diagnostic field %q:\n%s", forbidden, stateContent)
		}
	}
}

func runMCPApplyDelegatedRouteExpectFailed(
	t *testing.T,
	project mcpCLIProject,
) (clijson.ApplyResult, durable.Snapshot) {
	t.Helper()
	payload, state, _ := runMCPApplyDelegatedRouteExpectFailedWithStdout(t, project)
	return payload, state
}

func runMCPApplyDelegatedRouteExpectFailedWithStdout(
	t *testing.T,
	project mcpCLIProject,
) (clijson.ApplyResult, durable.Snapshot, string) {
	t.Helper()
	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("ordinary apply attempt exitCode=%d stdout=%q stderr=%q, want failed attempt JSON only", exitCode, stdout, stderr)
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	if !payload.HasErrors || len(payload.Errors) != 1 || !strings.Contains(payload.Errors[0].Message, "delegate attempt failed") {
		t.Fatalf("payload errors = %#v, want delegate attempt failure", payload.Errors)
	}
	return payload, loadMCPStatefile(t, project.root), stdout
}

func runMCPCLIWithOptions(t *testing.T, args []string, options clipkg.RunOptions) (int, string, string) {
	t.Helper()
	var stdout strings.Builder
	var stderr strings.Builder
	options.Stdout = &stdout
	options.Stderr = &stderr
	exitCode := testkit.RunVerboseCLIWithOptions(args, options)
	return exitCode, stdout.String(), stderr.String()
}

func runMCPCLIWithSuccessfulDelegate(t *testing.T, args ...string) string {
	t.Helper()
	exitCode, stdout, stderr := runMCPCLIWithOptions(t, args, clipkg.RunOptions{
		ApplyExecuteOptions: applyworkflow.ExecuteOptions{
			DelegateExecutor: delegate.NewExecutor(delegate.Options{
				Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			}),
		},
	})
	if exitCode != 0 || stderr != "" {
		t.Fatalf("successful delegate apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	return stdout
}

func assertSingleMCPDelegateAttempt(
	t *testing.T,
	snapshot durable.Snapshot,
	wantStatus durableattempt.DelegateAttemptStatus,
	wantReason durableattempt.DelegateAttemptReason,
) durableattempt.DelegateAttempt {
	t.Helper()
	attempts := snapshot.DelegateAttempts()
	if len(attempts) != 1 {
		t.Fatalf("delegate attempts = %#v, want one ordinary apply attempt record", attempts)
	}
	record := attempts[0]
	if record.Status() != wantStatus || record.Reason() != wantReason {
		t.Fatalf("delegate attempt record = %#v, want %s/%s", record, wantStatus, wantReason)
	}
	if strings.TrimSpace(record.PlanIdentityKey()) == "" {
		t.Fatalf("delegate attempt record = %#v, want locked plan identity key", record)
	}
	if record.ObservationSummary() != observerelation.ObservationPresent ||
		record.PostconditionSummary() != observerelation.PostconditionNotObserved {
		t.Fatalf("delegate attempt record = %#v, want observation present and postcondition not_observed", record)
	}
	return record
}

func assertMCPDelegateActionDisclosure(
	t *testing.T,
	action clijson.DelegateAction,
	wantStatus string,
	wantOutcome string,
	wantSchedules bool,
	wantRunner string,
	spec mcpManifestSpec,
) {
	t.Helper()
	if action.Subject.Name != "context7" ||
		action.Target != "claude-code" ||
		action.Scope != "project" ||
		action.Status != wantStatus ||
		action.PolicyOutcome != wantOutcome ||
		action.SchedulesAttempt != wantSchedules ||
		action.RunnerKind != wantRunner ||
		action.Command != spec.Command ||
		!slices.Equal(action.Args, spec.Args) ||
		action.Environment != "inherit" ||
		action.PinPolicy == "" ||
		action.TimeoutSeconds != 30 ||
		strings.TrimSpace(action.PlanIdentityKey) == "" {
		t.Fatalf("delegate action = %#v, want %s/%s %s disclosure for %#v", action, wantStatus, wantOutcome, wantRunner, spec)
	}
	for name, sourceName := range spec.Env {
		if !slices.ContainsFunc(action.EnvBindings, func(binding clijson.EnvBinding) bool {
			return binding.Name == name && binding.SourceName == sourceName
		}) {
			t.Fatalf("delegate action env bindings = %#v, want %s<-%s", action.EnvBindings, name, sourceName)
		}
	}
	if spec.Command == "npx" {
		if len(action.Packages) != 1 ||
			action.Packages[0].Ecosystem != "npm" ||
			action.Packages[0].Name != "@upstash/context7-mcp" ||
			action.PinPolicy != "floating" {
			t.Fatalf("delegate action packages = %#v pin=%q, want floating npm package", action.Packages, action.PinPolicy)
		}
	}
}

func assertMCPDelegateActionRisk(t *testing.T, action clijson.DelegateAction, code string, severity string) {
	t.Helper()
	for _, risk := range action.Risks {
		if risk.Code == code && risk.Severity == severity {
			return
		}
	}
	t.Fatalf("delegate action risks = %#v, want %s/%s", action.Risks, code, severity)
}

func assertMCPDelegateAttemptJSON(
	t *testing.T,
	attempt clijson.DelegateAttempt,
	wantStatus string,
	wantReason durableattempt.DelegateAttemptReason,
) {
	t.Helper()
	if attempt.Subject.Name != "context7" ||
		attempt.EvidenceKind != "last_attempt_diagnostics" ||
		attempt.Authority != "history_only" ||
		attempt.Target != "claude-code" ||
		attempt.Scope != "project" ||
		attempt.Status != wantStatus ||
		attempt.Reason != string(wantReason) ||
		attempt.Observation != string(observerelation.ObservationPresent) ||
		attempt.Postcondition != string(observerelation.PostconditionNotObserved) ||
		strings.TrimSpace(attempt.PlanIdentityKey) == "" {
		t.Fatalf("delegate attempt json = %#v, want %s/%s", attempt, wantStatus, wantReason)
	}
}

func prependMCPDelegateExecutableToPath(t *testing.T, command string, script string) {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("create delegate fixture bin dir: %v", err)
	}
	path := filepath.Join(binDir, command)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write delegate fixture executable %q: %v", command, err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func unsetEnvForMCPDelegateTest(t *testing.T, name string) {
	t.Helper()
	previous, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset env %q: %v", name, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, previous)
			return
		}
		_ = os.Unsetenv(name)
	})
}

func assertMCPDelegateNoSecretLeak(t *testing.T, label string, value string) {
	t.Helper()
	for _, forbidden := range []string{"super-secret-value", "literal-token", "hunter2"} {
		if strings.Contains(value, forbidden) {
			t.Fatalf("%s = %q, leaked forbidden value %q", label, value, forbidden)
		}
	}
}
