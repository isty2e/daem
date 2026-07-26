package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestRemovedMCPDeclarationRequiresRelockBeforeCleanup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project with spaces")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	manifestPath := filepath.Join(root, "daem.toml")
	writeCLILockCoverageManifest(t, manifestPath, false)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}

	delegateAttempts := 0
	applyOptions := testMCPApplyOptions(&delegateAttempts)
	assertCLIExitCode(t, []string{
		"apply", "--manifest", manifestPath, "--target", string(target.TargetClaudeCode), "--yes",
	}, applyOptions, 0)
	delegateAttemptsBeforeBlock := delegateAttempts
	hostPath := filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)
	statePath := filepath.Join(root, ".daem", "state.json")
	hostBefore := readCLIFile(t, hostPath)
	stateBefore := readCLIFile(t, statePath)

	if err := os.WriteFile(manifestPath, []byte("version = 1\ntargets = [\"claude-code\"]\n"), 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	for _, test := range []struct {
		name     string
		args     []string
		options  RunOptions
		wantExit int
		stream   string
	}{
		{
			name:     "status report only",
			args:     []string{"status", "--manifest", manifestPath, "--target", string(target.TargetClaudeCode)},
			wantExit: 0,
			stream:   "stdout",
		},
		{
			name:     "status check",
			args:     []string{"status", "--manifest", manifestPath, "--target", string(target.TargetClaudeCode), "--check"},
			wantExit: 1,
			stream:   "stdout",
		},
		{
			name:     "apply dry run",
			args:     []string{"apply", "--manifest", manifestPath, "--target", string(target.TargetClaudeCode), "--dry-run"},
			wantExit: 1,
			stream:   "stdout",
		},
		{
			name:     "apply write",
			args:     []string{"apply", "--manifest", manifestPath, "--target", string(target.TargetClaudeCode), "--yes"},
			options:  applyOptions,
			wantExit: 1,
			stream:   "stderr",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, exitCode := runCLIEdge(t, test.args, test.options)
			if exitCode != test.wantExit {
				t.Fatalf("exitCode = %d stdout = %q stderr = %q, want %d", exitCode, stdout, stderr, test.wantExit)
			}
			selected := stdout
			if test.stream == "stderr" {
				selected = stderr
			}
			if !strings.Contains(selected, string(reconcile.ReasonUnexpectedLockSubject)) &&
				!strings.Contains(selected, "blocked: unexpected lock subject") {
				t.Fatalf("selected output = %q, want unexpected lock subject blocker", selected)
			}
			if !strings.Contains(selected, "next: run daem lock --manifest") {
				t.Fatalf("selected output = %q, want relock remediation", selected)
			}
			assertCLIFileUnchanged(t, hostPath, hostBefore)
			assertCLIFileUnchanged(t, statePath, stateBefore)
			if delegateAttempts != delegateAttemptsBeforeBlock {
				t.Fatalf("delegate attempts = %d, want unchanged at %d while blocked", delegateAttempts, delegateAttemptsBeforeBlock)
			}
		})
	}

	stdout, stderr, exitCode := runCLIEdge(t, []string{
		"status", "--manifest", manifestPath, "--target", string(target.TargetClaudeCode), "--check", "--json",
	}, RunOptions{})
	if exitCode != 1 || stderr != "" {
		t.Fatalf("JSON status exitCode=%d stdout=%q stderr=%q, want 1 and empty stderr", exitCode, stdout, stderr)
	}
	var payload struct {
		Actions []struct {
			Kind    string `json:"kind"`
			Reason  string `json:"reason"`
			Subject *struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"subject"`
			Projection *struct {
				ConfigPath string `json:"config_path"`
			} `json:"projection"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("Unmarshal status JSON: %v\n%s", err, stdout)
	}
	if len(payload.Actions) != 1 ||
		payload.Actions[0].Kind != string(reconcile.ActionKindError) ||
		payload.Actions[0].Reason != string(reconcile.ReasonUnexpectedLockSubject) ||
		payload.Actions[0].Subject == nil ||
		payload.Actions[0].Subject.Namespace != "claude-code.project.mcp-server" ||
		payload.Actions[0].Subject.Name != "context7" ||
		payload.Actions[0].Projection == nil ||
		payload.Actions[0].Projection.ConfigPath != aggregate.ClaudeProjectMCPConfigPath {
		t.Fatalf("JSON actions = %#v, want exact old locked projection blocker", payload.Actions)
	}

	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock after removal returned error: %v", err)
	}
	assertCLIExitCode(t, []string{
		"apply", "--manifest", manifestPath, "--target", string(target.TargetClaudeCode), "--yes",
	}, applyOptions, 0)
	stdout, stderr, exitCode = runCLIEdge(t, []string{
		"status", "--manifest", manifestPath, "--check",
	}, RunOptions{})
	if exitCode != 0 || stderr != "" {
		t.Fatalf("post-relock status exitCode=%d stdout=%q stderr=%q, want clean", exitCode, stdout, stderr)
	}
}

func testMCPApplyOptions(delegateAttempts *int) RunOptions {
	return RunOptions{ApplyExecuteOptions: applyworkflow.ExecuteOptions{
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
				*delegateAttempts++
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	}}
}

func assertCLIExitCode(t *testing.T, args []string, options RunOptions, want int) {
	t.Helper()
	stdout, stderr, got := runCLIEdge(t, args, options)
	if got != want {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q, want %d", got, stdout, stderr, want)
	}
}

func runCLIEdge(t *testing.T, args []string, options RunOptions) (string, string, int) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options.Stdout = &stdout
	options.Stderr = &stderr
	exitCode := RunWithOptions(args, options)
	return stdout.String(), stderr.String(), exitCode
}

func readCLIFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %q: %v", path, err)
	}
	return content
}

func assertCLIFileUnchanged(t *testing.T, path string, before []byte) {
	t.Helper()
	after := readCLIFile(t, path)
	if !bytes.Equal(after, before) {
		t.Fatalf("file %q changed while lock readiness was blocked", path)
	}
}
