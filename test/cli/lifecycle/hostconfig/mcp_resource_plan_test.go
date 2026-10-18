package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/platformsupport"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestMCPPublicCLIResourcePlanKeepsConfigurationAndCommandFactsSeparate(t *testing.T) {
	for _, test := range []struct {
		name        string
		managed     bool
		conflict    bool
		separate    bool
		wantCurrent int
		wantPending int
		wantBlocked int
	}{
		{name: "create and conflict", conflict: true, wantPending: 1, wantBlocked: 1},
		{name: "no-op and conflict", managed: true, conflict: true, wantCurrent: 1, wantBlocked: 1},
		{name: "all configuration current", managed: true, wantCurrent: 2},
		{name: "conflict in another document", managed: true, conflict: true, separate: true, wantCurrent: 1, wantBlocked: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			project := newMCPCLIProject(t)
			home := filepath.Join(project.root, "home")
			t.Setenv("HOME", home)
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
			t.Setenv("PATH", "")
			filesystemScope := "project"
			if test.separate {
				filesystemScope = "global"
			}
			testkit.WriteFile(t, project.root, "daem.toml", fmt.Sprintf(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "must-not-run-daem-test"
scope = "project"

[[mcp_server]]
name = "filesystem"
transport = "stdio"
command = "must-not-run-daem-test"
scope = %q
`, filesystemScope))
			config := `{"mcpServers":{"filesystem":{"type":"stdio","command":"manual-not-executed","args":[],"env":{}}}}`
			if test.managed {
				config = `{"mcpServers":{"context7":{"type":"stdio","command":"must-not-run-daem-test","args":[],"env":{}},"filesystem":{"type":"stdio","command":"manual-not-executed","args":[],"env":{}}}}`
			}
			if !test.conflict {
				config = `{"mcpServers":{"context7":{"type":"stdio","command":"must-not-run-daem-test","args":[],"env":{}},"filesystem":{"type":"stdio","command":"must-not-run-daem-test","args":[],"env":{}}}}`
			}
			if test.separate {
				config = `{"mcpServers":{"context7":{"type":"stdio","command":"must-not-run-daem-test","args":[],"env":{}}}}`
				testkit.WriteFile(t, home, ".claude.json", `{"mcpServers":{"filesystem":{"type":"stdio","command":"manual-not-executed","args":[],"env":{}}}}`)
			}
			testkit.WriteFile(t, project.root, ".mcp.json", config)

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
			run := func(wantExit int, args ...string) string {
				t.Helper()
				var stdout, stderr bytes.Buffer
				invocation := options
				invocation.Stdout, invocation.Stderr = &stdout, &stderr
				exit := testkit.RunCLIWithOptions(append(args, "--manifest", project.manifestPath), invocation)
				if exit != wantExit || stderr.Len() != 0 || attempts.Load() != 0 || probe.called {
					t.Fatalf("%v exit=%d want=%d stdout=%q stderr=%q attempts=%d probe=%t", args, exit, wantExit, stdout.String(), stderr.String(), attempts.Load(), probe.called)
				}
				return stdout.String()
			}
			run(0, "lock")
			if test.managed {
				seedResourcePlanMCPState(t, project, !test.conflict)
			}
			before := sharedMCPFixtureFiles(t, project.root)

			status := run(0, "status")
			wantCounts := fmt.Sprintf("2 output entries (current=%d pending=%d blocked=%d)", test.wantCurrent, test.wantPending, test.wantBlocked)
			if !strings.HasPrefix(status, "status: "+wantCounts+"\n") {
				t.Fatalf("status lacks scoped configuration counts: %s", status)
			}
			checkExit := 0
			if test.conflict {
				checkExit = 1
			}
			assessment := clijson.DecodePlan(t, []byte(run(checkExit, "status", "--check", "--json")))
			if assessment.CheckClean == nil || *assessment.CheckClean == test.conflict || assessment.HasErrors != test.conflict {
				t.Fatalf("check assessment = %#v", assessment)
			}

			preview := run(checkExit, "apply", "--dry-run")
			if !strings.HasPrefix(preview, "dry-run: "+wantCounts+"\n") {
				t.Fatalf("preview lacks scoped configuration counts: %s", preview)
			}
			for _, name := range []string{"context7", "filesystem"} {
				scope := "project"
				if name == "filesystem" {
					scope = filesystemScope
				}
				block := mcpResourcePlanBlock(t, preview, name, scope)
				if test.separate && name == "filesystem" {
					if strings.Contains(block, "command plan:") {
						t.Fatalf("configuration-only binding gained a command plan: %s", block)
					}
					continue
				}
				for _, want := range []string{"command plan: skipped (dry-run)", `command="must-not-run-daem-test" args=[] env_bindings=[] environment=inherit`} {
					if !strings.Contains(block, want) {
						t.Fatalf("resource %q omits command fact %q: %s", name, want, block)
					}
				}
				if strings.Contains(block, "prerequisite blocked") != (test.conflict && !test.separate) {
					t.Fatalf("resource %q prerequisite disclosure does not match its plan: %s", name, block)
				}
			}
			contextBlock := mcpResourcePlanBlock(t, preview, "context7", "project")
			if test.managed && !strings.Contains(contextBlock, "config: up to date") {
				t.Fatalf("converged context7 lost its own delta: %s", contextBlock)
			}
			if test.conflict && !test.separate && (!strings.Contains(contextBlock, "shared configuration: blocked") || !strings.Contains(contextBlock, "mcp-server/filesystem")) {
				t.Fatalf("context7 lost the shared-document cause: %s", contextBlock)
			}
			if test.separate && strings.Contains(contextBlock, "configuration: blocked") {
				t.Fatalf("another document contaminated context7's prerequisite: %s", contextBlock)
			}
			if got := strings.Count(preview, "package, cache, server, auth, and future readiness are not guaranteed"); got != 1 {
				t.Fatalf("common command non-guarantee appears %d times: %s", got, preview)
			}

			plan := clijson.DecodePlan(t, []byte(run(checkExit, "apply", "--dry-run", "--json")))
			wantPlans := 2
			if test.separate {
				wantPlans = 1
			}
			if plan.ActionCount != 2 || len(plan.Actions) != 2 || len(plan.DelegateActions) != wantPlans || plan.CheckClean != nil || plan.HasErrors != test.conflict {
				t.Fatalf("preview JSON changed meaning: %#v", plan)
			}
			for _, action := range plan.DelegateActions {
				if action.Status != "skipped" || action.PolicyOutcome != "skip" || action.SchedulesAttempt {
					t.Fatalf("preview delegated an attempt: %#v", action)
				}
				if test.conflict && !test.separate {
					assertMCPDelegateActionRisk(t, action, "precondition_blocked", "block")
				}
			}
			if !reflect.DeepEqual(before, sharedMCPFixtureFiles(t, project.root)) {
				t.Fatal("passive output inspection changed fixture state")
			}
		})
	}
}

func seedResourcePlanMCPState(t *testing.T, project mcpCLIProject, includeFilesystem bool) {
	t.Helper()
	locked, err := lockfile.Load(t.Context(), project.lockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	var states []durable.ManagedAggregateState
	for _, contract := range locked.Locked.Subjects() {
		if !includeFilesystem && contract.SubjectID().Key() != "context7" {
			continue
		}
		contribution := testkit.LockedManagedAggregateContribution(t, contract)
		state, err := durable.NewManagedAggregateState(contract.SubjectID(), contribution)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: states})
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteStatefile(t, filepath.Join(project.root, ".daem", "state.json"), snapshot)
}

func mcpResourcePlanBlock(t *testing.T, output string, name string, scope string) string {
	t.Helper()
	header := fmt.Sprintf("  resource=\"mcp-server/%s\" target=claude-code scope=%s\n", name, scope)
	if strings.Count(output, header) != 1 {
		t.Fatalf("expected exactly one placement block for %q: %s", name, output)
	}
	_, body, _ := strings.Cut(output, header)
	block, _, _ := strings.Cut(body, "\n  resource=")
	return block
}
