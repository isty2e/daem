package cli_test

import (
	"path/filepath"
	"testing"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	"github.com/isty2e/daem/internal/realization/aggregate"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestMCPPublicCLIDoesNotInvokeRuntimeProbesOnLifecyclePaths(t *testing.T) {
	canary := execcheck.New(t, "claude", "npx", "headers-helper-daem-test")
	project := newMCPCLIProject(t)
	writeMCPProbeManifest(t, project.root)

	execcheck.AssertClean(t, canary, "initial setup")
	runMCPCLIExpect(t, 0, "lock dry-run", "lock", "--manifest", project.manifestPath, "--dry-run", "--json")
	execcheck.AssertClean(t, canary, "lock dry-run")
	runMCPCLIExpect(t, 0, "lock write", "lock", "--manifest", project.manifestPath)
	execcheck.AssertClean(t, canary, "lock write")
	runMCPCLIExpect(t, 0, "doctor passive prerequisite diagnostics", "doctor", "--manifest", project.manifestPath)
	execcheck.AssertClean(t, canary, "doctor passive prerequisite diagnostics")
	runMCPCLIExpect(t, 0, "outdated read-only", "outdated", "--manifest", project.manifestPath, "--json")
	execcheck.AssertClean(t, canary, "outdated read-only")

	dryRun := runMCPCLIExpect(t, 0, "apply dry-run create", "apply", "--manifest", project.manifestPath, "--dry-run", "--json")
	dryRunPayload := clijson.DecodePlan(t, []byte(dryRun))
	assertEveryMCPJSONDimension(t, dryRunPayload, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertEveryMCPJSONDimension(t, dryRunPayload, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	execcheck.AssertClean(t, canary, "apply dry-run create")
	attemptDryRun := runMCPCLIExpect(t, 0, "apply delegated route dry-run create", "apply", "--manifest", project.manifestPath, "--dry-run", "--json")
	attemptDryRunPayload := clijson.DecodePlan(t, []byte(attemptDryRun))
	assertEveryMCPJSONDimension(t, attemptDryRunPayload, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertEveryMCPJSONDimension(t, attemptDryRunPayload, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	execcheck.AssertClean(t, canary, "apply delegated route dry-run create")

	missingStatus := runMCPCLIExpect(t, 1, "status check before apply", "status", "--manifest", project.manifestPath, "--check", "--json")
	missingStatusPayload := clijson.DecodePlan(t, []byte(missingStatus))
	assertEveryMCPJSONDimension(t, missingStatusPayload, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertEveryMCPJSONDimension(t, missingStatusPayload, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	execcheck.AssertClean(t, canary, "status check before apply")

	runMCPCLIWithSuccessfulDelegate(t, "apply", "--manifest", project.manifestPath, "--yes", "--json")
	execcheck.AssertClean(t, canary, "apply write create")

	cleanStatus := runMCPCLIExpect(t, 0, "status check after apply", "status", "--manifest", project.manifestPath, "--check", "--json")
	cleanStatusPayload := clijson.DecodePlan(t, []byte(cleanStatus))
	assertEveryMCPJSONDimension(t, cleanStatusPayload, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertEveryMCPJSONDimension(t, cleanStatusPayload, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	assertEveryMCPJSONDimension(t, cleanStatusPayload, "delegate_last_attempt", "succeeded", "")
	execcheck.AssertClean(t, canary, "status check after apply")

	writeMCPManifestWithoutServers(t, project.root)
	runMCPCLIExpect(t, 0, "lock removal", "lock", "--manifest", project.manifestPath)
	execcheck.AssertClean(t, canary, "lock removal")
	runMCPCLIExpect(t, 0, "apply dry-run removal", "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--dry-run", "--json")
	execcheck.AssertClean(t, canary, "apply dry-run removal")
	runMCPCLIExpect(t, 0, "apply write removal", "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	execcheck.AssertClean(t, canary, "apply write removal")
	runMCPCLIExpect(t, 0, "status check after removal", "status", "--manifest", project.manifestPath, "--check", "--json")
	execcheck.AssertClean(t, canary, "status check after removal")
}

func TestMCPPublicCLIDoesNotInvokeOpenCodeRuntimeProbeOnLifecyclePaths(t *testing.T) {
	canary := execcheck.New(t, "opencode-probe-daem-test")
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "opencode",
		Command: "opencode-probe-daem-test",
		Args:    []string{"--serve", "context7"},
	})

	execcheck.AssertClean(t, canary, "initial setup")
	runMCPCLIExpect(t, 0, "opencode lock dry-run", "lock", "--manifest", project.manifestPath, "--dry-run", "--json")
	execcheck.AssertClean(t, canary, "opencode lock dry-run")
	runMCPCLIExpect(t, 0, "opencode lock write", "lock", "--manifest", project.manifestPath)
	execcheck.AssertClean(t, canary, "opencode lock write")
	runMCPCLIExpect(t, 0, "opencode doctor passive diagnostics", "doctor", "--manifest", project.manifestPath, "--target", "opencode")
	execcheck.AssertClean(t, canary, "opencode doctor passive diagnostics")
	runMCPCLIExpect(t, 0, "opencode outdated read-only", "outdated", "--manifest", project.manifestPath, "--json")
	execcheck.AssertClean(t, canary, "opencode outdated read-only")

	dryRun := runMCPCLIExpect(t, 0, "opencode apply dry-run create", "apply", "--manifest", project.manifestPath, "--target", "opencode", "--dry-run", "--json")
	dryRunPayload := clijson.DecodePlan(t, []byte(dryRun))
	assertEveryMCPJSONDimension(t, dryRunPayload, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertEveryMCPJSONDimension(t, dryRunPayload, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	execcheck.AssertClean(t, canary, "opencode apply dry-run create")
	attemptDryRun := runMCPCLIExpect(t, 0, "opencode apply delegated route dry-run create", "apply", "--manifest", project.manifestPath, "--target", "opencode", "--dry-run", "--json")
	attemptDryRunPayload := clijson.DecodePlan(t, []byte(attemptDryRun))
	assertEveryMCPJSONDimension(t, attemptDryRunPayload, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertEveryMCPJSONDimension(t, attemptDryRunPayload, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	execcheck.AssertClean(t, canary, "opencode apply delegated route dry-run create")

	missingStatus := runMCPCLIExpect(t, 1, "opencode status check before apply", "status", "--manifest", project.manifestPath, "--target", "opencode", "--check", "--json")
	missingStatusPayload := clijson.DecodePlan(t, []byte(missingStatus))
	assertEveryMCPJSONDimension(t, missingStatusPayload, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertEveryMCPJSONDimension(t, missingStatusPayload, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	execcheck.AssertClean(t, canary, "opencode status check before apply")

	runMCPCLIExpect(t, 0, "opencode apply write create", "apply", "--manifest", project.manifestPath, "--target", "opencode", "--yes", "--json")
	execcheck.AssertClean(t, canary, "opencode apply write create")
	cleanStatus := runMCPCLIExpect(t, 0, "opencode status check after apply", "status", "--manifest", project.manifestPath, "--target", "opencode", "--check", "--json")
	cleanStatusPayload := clijson.DecodePlan(t, []byte(cleanStatus))
	assertEveryMCPJSONDimension(t, cleanStatusPayload, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertEveryMCPJSONDimension(t, cleanStatusPayload, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	execcheck.AssertClean(t, canary, "opencode status check after apply")
}

func TestMCPPublicCLIDoesNotInvokeCommandsOnGlobalProjectionLifecyclePaths(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		command string
	}{
		{name: "claude global", target: "claude-code", command: "claude-global-daem-test"},
		{name: "codex global", target: "codex", command: "codex-global-daem-test"},
		{name: "opencode global", target: "opencode", command: "opencode-global-daem-test"},
		{name: "antigravity global", target: "antigravity-cli", command: "antigravity-global-daem-test"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			canary := execcheck.New(t, test.command)
			project := newMCPCLIProject(t)
			t.Setenv("HOME", filepath.Join(project.root, "home"))
			writeMCPManifest(t, project.root, mcpManifestSpec{
				Target:  test.target,
				Scope:   "global",
				Command: test.command,
				Args:    []string{"--serve", "context7"},
			})

			execcheck.AssertClean(t, canary, "initial setup")
			runMCPCLIExpect(t, 0, test.name+" lock dry-run", "lock", "--manifest", project.manifestPath, "--dry-run", "--json")
			execcheck.AssertClean(t, canary, test.name+" lock dry-run")
			runMCPCLIExpect(t, 0, test.name+" lock write", "lock", "--manifest", project.manifestPath)
			execcheck.AssertClean(t, canary, test.name+" lock write")
			runMCPCLIExpect(t, 0, test.name+" outdated read-only", "outdated", "--manifest", project.manifestPath, "--json")
			execcheck.AssertClean(t, canary, test.name+" outdated read-only")
			runMCPCLIExpect(t, 0, test.name+" apply dry-run create", "apply", "--manifest", project.manifestPath, "--target", test.target, "--dry-run", "--json")
			execcheck.AssertClean(t, canary, test.name+" apply dry-run create")
			runMCPCLIExpect(t, 1, test.name+" status check before apply", "status", "--manifest", project.manifestPath, "--target", test.target, "--check", "--json")
			execcheck.AssertClean(t, canary, test.name+" status check before apply")
			runMCPCLIExpect(t, 0, test.name+" apply write create", "apply", "--manifest", project.manifestPath, "--target", test.target, "--yes", "--json")
			execcheck.AssertClean(t, canary, test.name+" apply write create")
			runMCPCLIExpect(t, 0, test.name+" status check after apply", "status", "--manifest", project.manifestPath, "--target", test.target, "--check", "--json")
			execcheck.AssertClean(t, canary, test.name+" status check after apply")

			testkit.WriteFile(t, project.root, "daem.toml", `version = 1
targets = ["`+test.target+`"]
`)
			runMCPCLIExpect(t, 0, test.name+" lock removal", "lock", "--manifest", project.manifestPath)
			execcheck.AssertClean(t, canary, test.name+" lock removal")
			runMCPCLIExpect(t, 0, test.name+" apply dry-run removal", "apply", "--manifest", project.manifestPath, "--dry-run", "--json")
			execcheck.AssertClean(t, canary, test.name+" apply dry-run removal")
			runMCPCLIExpect(t, 0, test.name+" apply write removal", "apply", "--manifest", project.manifestPath, "--yes", "--json")
			execcheck.AssertClean(t, canary, test.name+" apply write removal")
		})
	}
}

func TestMCPPublicCLIApplyDelegatedRouteInvokesLockedCommandOnlyAfterConfirmation(t *testing.T) {
	canary := execcheck.New(t, "must-not-run-daem-test")
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: "must-not-run-daem-test",
		Args:    []string{"--serve", "context7"},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("ordinary apply attempt exitCode=%d stdout=%q stderr=%q, want failed attempt JSON only", exitCode, stdout, stderr)
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	clijson.RequireApplyFailure(
		t,
		payload,
		applyworkflow.FailureReasonDelegateAttemptFailed,
		applyworkflow.FailurePhaseExecution,
		applyworkflow.FailureOutcomeIncomplete,
	)
	if payload.ActionCount != 1 {
		t.Fatalf("payload action_count = %d, want committed projection action before failed attempt", payload.ActionCount)
	}
	execcheck.AssertInvoked(t, canary, "must-not-run-daem-test")
	assertMCPConfigEquivalent(t, project.root, "context7", spec)
	state := loadMCPStatefile(t, project.root)
	attempts := state.DelegateAttempts()
	if len(attempts) != 1 {
		t.Fatalf("delegate attempts = %#v, want one ordinary apply attempt record", attempts)
	}
	record := attempts[0]
	if record.Status() != durableattempt.DelegateStatusFailed || record.Reason() != durableattempt.DelegateReasonNonZeroExit {
		t.Fatalf("delegate attempt record = %#v, want failed nonzero diagnostic", record)
	}
}

func TestMCPPublicCLIApplyDelegatedRouteDoesNotRunWhenLockIsStale(t *testing.T) {
	canary := execcheck.New(t, "must-not-run-daem-test")
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "must-not-run-daem-test",
		Args:    []string{"--serve", "context7"},
	})
	runMCPLock(t, project)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "must-not-run-daem-test",
		Args:    []string{"--serve", "context7", "--changed"},
	})

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("delegated route stale apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	assertApplyResultSubjectAction(t, payload, "error", "context7")
	clijson.RequireApplyFailure(
		t,
		payload,
		applyworkflow.FailureReasonApplyRefused,
		applyworkflow.FailurePhasePreflight,
		applyworkflow.FailureOutcomeRefused,
	)
	if len(payload.Actions) != 1 || payload.Actions[0].Reason != "stale_lock" {
		t.Fatalf("payload actions = %#v, want stale lock decision", payload.Actions)
	}
	if len(payload.DelegateActions) != 1 {
		t.Fatalf("delegate_actions = %#v, want one blocked stale-lock delegate action", payload.DelegateActions)
	}
	delegateAction := payload.DelegateActions[0]
	if delegateAction.Status != "blocked" ||
		delegateAction.PolicyOutcome != "block" ||
		delegateAction.SchedulesAttempt {
		t.Fatalf("delegate action = %#v, want blocked stale-lock action", delegateAction)
	}
	assertMCPDelegateActionRisk(t, delegateAction, "precondition_blocked", "block")
	if len(payload.DelegateAttempts) != 0 {
		t.Fatalf("delegate_attempts = %#v, want no attempt before stale projection is fixed", payload.DelegateAttempts)
	}
	execcheck.AssertClean(t, canary, "delegated route stale apply")
}

func TestMCPPublicCLIApplyDelegatedRouteDoesNotRunWhenProjectionPreconditionFails(t *testing.T) {
	tests := []struct {
		name   string
		config string
		state  string
		reason string
	}{
		{
			name:   "malformed",
			config: `{"mcpServers":`,
			state:  "malformed",
			reason: "CONFIG_MALFORMED",
		},
		{
			name:   "unsupported managed field",
			config: `{"mcpServers":{"context7":{"type":"stdio","command":"npx","args":[],"env":{},"cwd":"/tmp"}}}`,
			state:  "unsupported",
			reason: "UNSUPPORTED_MANAGED_FIELD",
		},
		{
			name:   "unmanaged same name",
			config: `{"mcpServers":{"context7":{"type":"stdio","command":"node","args":["server.js"],"env":{}}}}`,
			state:  "unmanaged_same_name",
			reason: "ROUTE_PREEXISTING_UNOWNED",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			canary := execcheck.New(t, "must-not-run-daem-test")
			project := newMCPCLIProject(t)
			writeMCPManifest(t, project.root, mcpManifestSpec{
				Command: "must-not-run-daem-test",
				Args:    []string{"--serve", "context7"},
			})
			runMCPLock(t, project)
			testkit.WriteFile(t, project.root, aggregate.ClaudeProjectMCPConfigPath, test.config)

			exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
			if exitCode != 1 || stderr != "" {
				t.Fatalf("delegated route blocked apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			payload := clijson.DecodeApplyResult(t, []byte(stdout))
			assertApplyResultSubjectAction(t, payload, "error", "context7")
			assertApplyResultMCPJSONDimension(t, payload, "project_projection", test.state, test.reason)
			if len(payload.DelegateActions) != 1 {
				t.Fatalf("delegate_actions = %#v, want one blocked delegate action", payload.DelegateActions)
			}
			delegateAction := payload.DelegateActions[0]
			if delegateAction.Status != "blocked" ||
				delegateAction.PolicyOutcome != "block" ||
				delegateAction.SchedulesAttempt {
				t.Fatalf("delegate action = %#v, want blocked precondition action", delegateAction)
			}
			assertMCPDelegateActionRisk(t, delegateAction, "precondition_blocked", "block")
			if len(payload.DelegateAttempts) != 0 {
				t.Fatalf("delegate_attempts = %#v, want no attempt before projection precondition passes", payload.DelegateAttempts)
			}
			execcheck.AssertClean(t, canary, "delegated route blocked apply")
		})
	}
}

func TestMCPPublicCLIDoesNotInvokeHostProbesOnBlockedPaths(t *testing.T) {
	canary := execcheck.New(t, "claude", "npx", "headers-helper-daem-test")
	project := newMCPCLIProject(t)
	writeMCPProbeManifest(t, project.root)
	runMCPCLIExpect(t, 0, "lock write", "lock", "--manifest", project.manifestPath)
	execcheck.AssertClean(t, canary, "lock write")

	testkit.WriteFile(t, project.root, aggregate.ClaudeProjectMCPConfigPath, `{"mcpServers":`)
	runMCPCLIExpect(t, 1, "apply dry-run malformed", "apply", "--manifest", project.manifestPath, "--dry-run", "--json")
	execcheck.AssertClean(t, canary, "apply dry-run malformed")
	runMCPCLIExpect(t, 1, "status check malformed", "status", "--manifest", project.manifestPath, "--check", "--json")
	execcheck.AssertClean(t, canary, "status check malformed")

	testkit.WriteFile(t, project.root, aggregate.ClaudeProjectMCPConfigPath, `{"mcpServers":{"package-probe":{"type":"stdio","command":"npx","args":[],"env":{},"cwd":"/tmp"}}}`)
	runMCPCLIExpect(t, 1, "apply dry-run unsupported field", "apply", "--manifest", project.manifestPath, "--dry-run", "--json")
	execcheck.AssertClean(t, canary, "apply dry-run unsupported field")
	runMCPCLIExpect(t, 1, "status check unsupported field", "status", "--manifest", project.manifestPath, "--check", "--json")
	execcheck.AssertClean(t, canary, "status check unsupported field")
}

func writeMCPProbeManifest(t *testing.T, root string) {
	t.Helper()
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "claude-probe"
transport = "stdio"
command = "claude"
args = ["mcp", "list"]

[[mcp_server]]
name = "package-probe"
transport = "stdio"
command = "npx"
args = ["-y", "@example/mcp-server"]

[[mcp_server]]
name = "helper-probe"
transport = "stdio"
command = "headers-helper-daem-test"
args = ["--emit"]
`)
}

func runMCPCLIExpect(t *testing.T, wantExitCode int, label string, args ...string) string {
	t.Helper()
	exitCode, stdout, stderr := runMCPCLI(t, args...)
	if exitCode != wantExitCode {
		t.Fatalf("%s exitCode=%d stdout=%q stderr=%q, want %d", label, exitCode, stdout, stderr, wantExitCode)
	}
	if stderr != "" {
		t.Fatalf("%s stderr=%q, want empty structured/no-probe path", label, stderr)
	}
	return stdout
}

func assertEveryMCPJSONDimension(t *testing.T, payload clijson.Plan, dimension string, state string, reason string) {
	t.Helper()
	if len(payload.MCPStatuses) == 0 {
		t.Fatalf("mcp_statuses = %#v, want at least one", payload.MCPStatuses)
	}
	for _, status := range payload.MCPStatuses {
		dimensions := status.Dimensions()
		found := false
		for _, got := range dimensions {
			if got.Dimension != dimension {
				continue
			}
			found = true
			if got.State != state || got.Reason != reason {
				t.Fatalf("%s dimension for %#v = %#v, want state=%q reason=%q", dimension, status.Subject, got, state, reason)
			}
		}
		if !found {
			t.Fatalf("dimensions for %#v = %#v, want %s", status.Subject, dimensions, dimension)
		}
	}
}
