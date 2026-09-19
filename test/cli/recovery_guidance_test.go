package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
)

func TestRecoveryGuidanceManagedDriftJourneys(t *testing.T) {
	for _, keepEdit := range []bool{false, true} {
		name := "restore baseline"
		if keepEdit {
			name = "accept intentional edit"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
			project := filepath.Join(root, "selected project's files")
			manifest := filepath.Join(project, "daem.toml")
			testkit.WriteFile(t, project, "instructions/project.md", "managed baseline\n")
			testkit.WriteFile(t, project, "daem.toml", "version = 1\ntargets = [\"codex\"]\n[instructions.project]\nsource = \"instructions/project.md\"\n")
			runRecoveryGuidanceCLI(t, manifest, 0, "lock")
			runRecoveryGuidanceCLI(t, manifest, 0, "apply", "--yes")

			testkit.WriteFile(t, project, "AGENTS.md", "intentional local edit\n")
			testkit.WriteFile(t, project, "instructions/project.md", "desired next revision\n")
			runRecoveryGuidanceCLI(t, manifest, 0, "lock")
			for _, args := range [][]string{
				{"status", "--check", "--target", "codex"},
				{"apply", "--dry-run", "--diff", "--target", "codex"},
				{"apply", "--yes", "--target", "codex"},
			} {
				output := runRecoveryGuidanceCLI(t, manifest, 1, args...)
				for _, want := range []string{"preserve the local edits", "source or declaration", "same --manifest and --target selection"} {
					if !strings.Contains(output, want) {
						t.Fatalf("%v output=%q, want %q", args, output, want)
					}
				}
				if strings.Contains(output, project) || strings.Contains(output, "+++ desired/") {
					t.Fatalf("default blocked output disclosed a path or content diff: %q", output)
				}
				testkit.AssertFileContent(t, filepath.Join(project, "AGENTS.md"), "intentional local edit\n")
			}

			inspection := []string{"status", "--target", "codex", "--verbose"}
			wantHint := testkit.ExpectedShellCommand(t, "daem", "status", "--manifest", manifest, "--target", "codex", "--verbose")
			output := runRecoveryGuidanceCLI(t, manifest, 1, "apply", "--dry-run", "--target", "codex", "--verbose")
			if !strings.Contains(output, "next: inspect with "+wantHint) {
				t.Fatalf("verbose output=%q, want scoped command %q", output, wantHint)
			}
			inspected := runRecoveryGuidanceCLI(t, manifest, 0, inspection...)
			if !strings.Contains(inspected, "reason=drifted_output") || !strings.Contains(inspected, "preserve the local edits") {
				t.Fatalf("suggested inspection lost the selected manifest/target or recovery guidance: %q", inspected)
			}
			if strings.Contains(inspected, "next: inspect") {
				t.Fatalf("verbose status recommends itself: %q", inspected)
			}

			var stdout, stderr bytes.Buffer
			code := testkit.RunCLI([]string{"apply", "--manifest", manifest, "--dry-run", "--json"}, &stdout, &stderr)
			var payload struct {
				HasErrors bool `json:"has_errors"`
			}
			if code != 1 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &payload) != nil || !payload.HasErrors {
				t.Fatalf("JSON boundary changed: code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}

			// The user preserves the edit before choosing either manual reconciliation path.
			testkit.WriteFile(t, project, "saved-edit.md", "intentional local edit\n")
			wantLive := "desired next revision\n"
			if keepEdit {
				wantLive = "intentional local edit\n"
				testkit.WriteFile(t, project, "instructions/project.md", wantLive)
				runRecoveryGuidanceCLI(t, manifest, 0, "lock")
			} else {
				testkit.WriteFile(t, project, "AGENTS.md", "managed baseline\n")
			}
			runRecoveryGuidanceCLI(t, manifest, 0, "apply", "--dry-run", "--target", "codex")
			runRecoveryGuidanceCLI(t, manifest, 0, "apply", "--yes", "--target", "codex")
			runRecoveryGuidanceCLI(t, manifest, 0, "status", "--check", "--target", "codex")
			testkit.AssertFileContent(t, filepath.Join(project, "AGENTS.md"), wantLive)
			testkit.AssertFileContent(t, filepath.Join(project, "saved-edit.md"), "intentional local edit\n")
		})
	}
}

func TestRecoveryGuidanceUnmanagedConfigDoesNotPromiseAdoption(t *testing.T) {
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
	manifest := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n[[mcp_server]]\nname = \"demo\"\ntransport = \"stdio\"\ncommand = \"must-not-run\"\nargs = []\n")
	const live = `{"mcpServers":{"demo":{"type":"stdio","command":"different","args":[],"env":{}},"sibling":{"type":"stdio","command":"keep","args":[],"env":{}}},"sentinel":"keep"}`
	testkit.WriteFile(t, root, ".mcp.json", live)
	runRecoveryGuidanceCLI(t, manifest, 0, "lock")
	for _, args := range [][]string{
		{"status", "--check"},
		{"apply", "--dry-run", "--manage-existing"},
		{"apply", "--yes", "--manage-existing"},
	} {
		output := runRecoveryGuidanceCLI(t, manifest, 1, args...)
		if !strings.Contains(output, "unmanaged content is preserved") || !strings.Contains(output, "exact match") || !strings.Contains(output, "other disclosed effects") {
			t.Fatalf("%v output=%q, want conditional adoption guidance", args, output)
		}
		testkit.AssertFileContent(t, filepath.Join(root, ".mcp.json"), live)
	}

	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n[[mcp_server]]\nname = \"demo\"\ntransport = \"stdio\"\ncommand = \"different\"\nargs = []\n")
	runRecoveryGuidanceCLI(t, manifest, 0, "lock")
	runRecoveryGuidanceCLI(t, manifest, 0, "apply", "--dry-run", "--manage-existing")
	var stdout, stderr bytes.Buffer
	calls := 0
	code := testkit.RunCLIWithOptions([]string{"apply", "--manifest", manifest, "--yes", "--manage-existing"}, clipkg.RunOptions{
		Stdout: &stdout, Stderr: &stderr,
		ApplyExecuteOptions: applyworkflow.ExecuteOptions{
			DelegateExecutor: delegate.NewExecutor(delegate.Options{
				Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					calls++
					if request.Command != "different" {
						t.Fatalf("unexpected delegate: %#v", request)
					}
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			}),
		},
	})
	if code != 0 || calls != 1 || stderr.Len() != 0 {
		t.Fatalf("adoption code=%d calls=%d stdout=%q stderr=%q", code, calls, &stdout, &stderr)
	}
	testkit.AssertFileContent(t, filepath.Join(root, ".mcp.json"), live)
	runRecoveryGuidanceCLI(t, manifest, 0, "status", "--check")
}

func runRecoveryGuidanceCLI(t *testing.T, manifest string, wantCode int, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	argv := append(append([]string(nil), args...), "--manifest", manifest)
	code := testkit.RunCLI(argv, &stdout, &stderr)
	if code != wantCode {
		t.Fatalf("%v code=%d want=%d stdout=%q stderr=%q", argv, code, wantCode, &stdout, &stderr)
	}
	return stdout.String() + stderr.String()
}
