package cli_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/platformsupport"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestMCPPublicCLISharedDocumentBlockReturnsPreviewWithoutEffects(t *testing.T) {
	project := newMCPCLIProject(t)
	home := filepath.Join(project.root, "home")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("PATH", "")

	testkit.WriteFile(t, project.root, "daem.toml", `version = 1
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
scope = "project"
`)
	testkit.WriteFile(t, project.root, ".mcp.json", `{"mcpServers":{"filesystem":{"type":"stdio","command":"manual-not-executed","args":[],"env":{}}}}`)

	var attempts atomic.Int32
	deny := func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
		attempts.Add(1)
		return subprocess.CommandResult{}
	}

	version, err := platformsupport.ParseMacOSProductVersion("26.0")
	if err != nil {
		t.Fatal(err)
	}
	observation, err := platformsupport.NewRuntimeObservation(version)
	if err != nil {
		t.Fatal(err)
	}

	probe := &fakeCLIRuntimeProbeExecutor{}
	options := clipkg.RunOptions{
		PlatformObserver: func(context.Context) (platformsupport.RuntimeObservation, error) { return observation, nil },
		ProbeExecutor:    probe,
		ApplyExecuteOptions: applyworkflow.ExecuteOptions{
			DelegateExecutor:  delegate.NewExecutor(delegate.Options{Runner: deny}),
			HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: deny}),
		},
	}

	run := func(args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		invocation := options
		invocation.Stdout, invocation.Stderr = &stdout, &stderr
		exit := testkit.RunCLIWithOptions(append(args, "--manifest", project.manifestPath), invocation)
		if attempts.Load() != 0 || probe.called {
			t.Fatalf("command %v attempted host execution or a probe", args)
		}
		return exit, stdout.String(), stderr.String()
	}

	if exit, stdout, stderr := run("lock"); exit != 0 || stderr != "" {
		t.Fatalf("lock exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}

	before := sharedMCPFixtureFiles(t, project.root)
	exit, stdout, stderr := run("apply", "--dry-run", "--json")
	if exit != 1 || stderr != "" {
		t.Fatalf("blocked preview exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}

	plan := clijson.DecodePlan(t, []byte(stdout))
	if !plan.HasErrors || len(plan.DelegateActions) != 2 {
		t.Fatalf("blocked preview = %#v", plan)
	}
	for _, action := range plan.DelegateActions {
		if action.Status != "skipped" || action.PolicyOutcome != "skip" || action.SchedulesAttempt {
			t.Fatalf("blocked preview delegate = %#v", action)
		}
		assertMCPDelegateActionRisk(t, action, "precondition_blocked", "block")
	}

	if !reflect.DeepEqual(before, sharedMCPFixtureFiles(t, project.root)) {
		t.Fatal("dry-run changed fixture files or state")
	}

	exit, stdout, stderr = run("apply", "--yes", "--json")
	if exit != 1 || stderr != "" {
		t.Fatalf("blocked JSON apply exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
	refusal := clijson.DecodeApplyResult(t, []byte(stdout))
	if !refusal.HasErrors || len(refusal.MCPStatuses) != 2 {
		t.Fatalf("ready-plan refusal lost MCP evidence: %#v", refusal)
	}
	for _, status := range refusal.MCPStatuses {
		assertMCPJSONDimension(t, clijson.Plan{MCPStatuses: []clijson.MCPStatus{status}}, "effective_shadowing", "unobserved", "EFFECTIVE_OBSERVER_UNSUPPORTED")
	}

	exit, stdout, stderr = run("apply", "--yes")
	if exit != 1 {
		t.Fatalf("blocked apply exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
	t.Logf("blocked apply default stdout=%q stderr=%q", stdout, stderr)
	if !reflect.DeepEqual(before, sharedMCPFixtureFiles(t, project.root)) {
		t.Fatal("blocked apply changed fixture files or state")
	}
}

func sharedMCPFixtureFiles(t *testing.T, root string) map[string]string {
	t.Helper()

	files := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return files
}
