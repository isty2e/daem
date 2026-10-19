package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/platformsupport"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
)

func TestMCPSharedDocumentKeepsPlanAndResultEntryUnits(t *testing.T) {
	project := newMCPCLIProject(t)
	home := filepath.Join(project.root, "home")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("PATH", "")
	testkit.WriteFile(t, project.root, "daem.toml", `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "one"
transport = "stdio"
command = "must-not-run-daem-test"
scope = "global"

[[mcp_server]]
name = "two"
transport = "stdio"
command = "must-not-run-daem-test"
scope = "global"
`)
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
		if exit := testkit.RunCLIWithOptions(append(args, "--manifest", project.manifestPath), invocation); exit != 0 || stderr.Len() != 0 || attempts.Load() != 0 || probe.called {
			t.Fatalf("%v exit=%d stdout=%s stderr=%s attempts=%d probe=%t", args, exit, stdout.String(), stderr.String(), attempts.Load(), probe.called)
		}
		return stdout.String()
	}
	run("lock")
	preview := run("apply", "--dry-run")
	result := run("apply", "--yes")
	if !strings.HasPrefix(preview, "dry-run: 2 output entries (current=0 pending=2 blocked=0)\n") ||
		!strings.Contains(result, "output results: 2 output entries\n") {
		t.Fatalf("MCP subjects collapsed into one file count: preview=%s result=%s", preview, result)
	}
	var document struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(testkit.ReadFile(t, filepath.Join(home, ".claude.json")), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Servers) != 2 || document.Servers["one"] == nil || document.Servers["two"] == nil {
		t.Fatalf("shared configuration does not contain both outputs: %#v", document.Servers)
	}
}
