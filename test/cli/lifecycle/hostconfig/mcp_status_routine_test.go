package cli_test

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/platformsupport"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestMCPPublicCLIConvergedStatusKeepsEvidenceWithoutRoutineRows(t *testing.T) {
	for _, history := range []string{"none", "succeeded", "failed", "stale"} {
		t.Run(history, func(t *testing.T) {
			project := newMCPCLIProject(t)
			testkit.WithWorkingDirectory(t, project.root)
			t.Setenv("PATH", "")
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(project.root, "home"))
			writeMCPManifest(t, project.root, mcpManifestSpec{Command: "must-not-run-daem-test", Args: []string{}})
			testkit.WriteFile(t, project.root, ".mcp.json", `{"mcpServers":{"context7":{"type":"stdio","command":"must-not-run-daem-test","args":[],"env":{}}}}`)

			version, err := platformsupport.ParseMacOSProductVersion("26.0")
			if err != nil {
				t.Fatal(err)
			}
			platform, err := platformsupport.NewRuntimeObservation(version)
			if err != nil {
				t.Fatal(err)
			}
			var attempts atomic.Int32
			deny := func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				attempts.Add(1)
				return subprocess.CommandResult{}
			}
			probe := &fakeCLIRuntimeProbeExecutor{}
			options := clipkg.RunOptions{
				PlatformObserver: func(context.Context) (platformsupport.RuntimeObservation, error) { return platform, nil },
				ProbeExecutor:    probe,
				ApplyExecuteOptions: applyworkflow.ExecuteOptions{
					DelegateExecutor:  delegate.NewExecutor(delegate.Options{Runner: deny}),
					HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: deny}),
				},
			}
			run := func(args ...string) string {
				t.Helper()
				var stdout, stderr bytes.Buffer
				invocation := options
				invocation.Stdout, invocation.Stderr = &stdout, &stderr
				code := testkit.RunCLIWithOptions(append(args, "--manifest", project.manifestPath), invocation)
				if code != 0 || stderr.Len() != 0 || attempts.Load() != 0 || probe.called {
					t.Fatalf("%v code=%d stderr=%q stdout=%q attempts=%d probe=%t", args, code, stderr.String(), stdout.String(), attempts.Load(), probe.called)
				}
				return stdout.String()
			}
			run("lock")
			seedResourcePlanMCPState(t, project, false)
			if history != "none" {
				contract := loadMCPDelegateStatusRecord(t, project.lockfilePath, "context7")
				plan, present := contract.DelegatePlan()
				if !present {
					t.Fatal("fixture requires a locked delegate plan")
				}
				identity := plan.IdentityKey()
				if history == "stale" {
					identity += "-previous"
				}
				status, reason := durableattempt.DelegateStatusSucceeded, durableattempt.DelegateReasonNone
				if history == "failed" {
					status, reason = durableattempt.DelegateStatusFailed, durableattempt.DelegateReasonNonZeroExit
				}
				state := snapshotWithMCPDelegateStatusAttempt(t, loadMCPStatefile(t, project.root), contract.SubjectID(), identity, status, reason)
				testkit.WriteStatefile(t, filepath.Join(project.root, ".daem", "state.json"), state)
			}
			before := sharedMCPFixtureFiles(t, project.root)

			basic := run("status", "--check")
			if history == "none" || history == "succeeded" {
				if basic != "status: 1 output entry (current=1 pending=0 blocked=0)\n" {
					t.Fatalf("routine status remains noisy: %s", basic)
				}
			} else if !strings.Contains(basic, "delegate_last_attempt: "+history) || !strings.Contains(basic, `resource="mcp-server/context7"`) {
				t.Fatalf("non-routine history was hidden: %s", basic)
			}
			if strings.Contains(basic, "effective_shadowing") {
				t.Fatalf("structurally unsupported observation became a warning: %s", basic)
			}
			verbose := run("status", "--check", "--verbose")
			if !strings.Contains(verbose, "effective_shadowing: unobserved reason=EFFECTIVE_OBSERVER_UNSUPPORTED") {
				t.Fatalf("verbose lost capability evidence: %s", verbose)
			}
			assessment := clijson.DecodePlan(t, []byte(run("status", "--check", "--json")))
			if assessment.ActionCount != 1 || assessment.HasErrors || assessment.CheckClean == nil || !*assessment.CheckClean || len(assessment.Actions) != 1 || assessment.Actions[0].Kind != "noop" {
				t.Fatalf("visibility changed convergence or error policy: %#v", assessment)
			}
			assertMCPJSONDimension(t, assessment, "effective_shadowing", "unobserved", "EFFECTIVE_OBSERVER_UNSUPPORTED")
			wantHistory := history
			if history == "none" {
				wantHistory = "not_observed"
			}
			foundHistory := false
			for _, dimension := range assessment.MCPStatuses[0].Delegate {
				if dimension.Dimension == "delegate_last_attempt" && dimension.State == wantHistory {
					foundHistory = true
				}
			}
			if !foundHistory {
				t.Fatalf("JSON lost history %q: %#v", wantHistory, assessment.MCPStatuses)
			}

			preview := run("apply", "--dry-run")
			if !strings.Contains(preview, `resource="mcp-server/context7"`) || !strings.Contains(preview, "config: up to date") || !strings.Contains(preview, "command plan: skipped (dry-run)") {
				t.Fatalf("quiet status hid a selected command plan: %s", preview)
			}
			plan := clijson.DecodePlan(t, []byte(run("apply", "--dry-run", "--json")))
			assertMCPJSONDimension(t, plan, "effective_shadowing", "unobserved", "EFFECTIVE_OBSERVER_UNSUPPORTED")
			if len(plan.DelegateActions) != 1 || plan.DelegateActions[0].Status != "skipped" || plan.DelegateActions[0].SchedulesAttempt {
				t.Fatalf("preview changed execution semantics: %#v", plan.DelegateActions)
			}
			if !reflect.DeepEqual(before, sharedMCPFixtureFiles(t, project.root)) {
				t.Fatal("passive status/preview changed fixture files")
			}
		})
	}
}
