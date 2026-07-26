package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestStatusModesReportDeclaredMCPSubjectMissingFromExistingLock(t *testing.T) {
	manifestPath := writeCLIPartialMCPLockFixture(t)

	for _, test := range []struct {
		name     string
		args     []string
		wantExit int
	}{
		{
			name:     "report only",
			args:     []string{"status", "--manifest", manifestPath, "--target", string(target.TargetClaudeCode)},
			wantExit: 0,
		},
		{
			name:     "check",
			args:     []string{"status", "--manifest", manifestPath, "--target", string(target.TargetClaudeCode), "--check"},
			wantExit: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := runWithoutTerminal(test.args, &stdout, &stderr)
			if exitCode != test.wantExit || stderr.Len() != 0 {
				t.Fatalf("exitCode = %d stdout = %q stderr = %q, want %d and empty stderr", exitCode, stdout.String(), stderr.String(), test.wantExit)
			}
			if !strings.Contains(stdout.String(), "subject=\"projection/claude-code.project.mcp-server/filesystem\"") ||
				!strings.Contains(stdout.String(), "blocked: missing lock") ||
				!strings.Contains(stdout.String(), "next: run daem lock --manifest") {
				t.Fatalf("stdout = %q, want missing subject blocker and lock remediation", stdout.String())
			}
		})
	}
}

func TestStatusCheckJSONReportsDeclaredMCPSubjectMissingFromExistingLock(t *testing.T) {
	manifestPath := writeCLIPartialMCPLockFixture(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{
		"status",
		"--manifest", manifestPath,
		"--target", string(target.TargetClaudeCode),
		"--check",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 1 || stderr.Len() != 0 {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q, want 1 and empty stderr", exitCode, stdout.String(), stderr.String())
	}
	var payload struct {
		Mode      string `json:"mode"`
		HasErrors bool   `json:"has_errors"`
		Actions   []struct {
			Kind    string `json:"kind"`
			Reason  string `json:"reason"`
			Target  string `json:"target"`
			Scope   string `json:"scope"`
			Subject *struct {
				Kind      string `json:"kind"`
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"subject"`
			Projection *struct {
				ConfigPath string `json:"config_path"`
			} `json:"projection"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal status JSON: %v\n%s", err, stdout.String())
	}
	if payload.Mode != "check" || !payload.HasErrors {
		t.Fatalf("payload mode/errors = %q/%t, want check/true", payload.Mode, payload.HasErrors)
	}
	missing := 0
	for _, action := range payload.Actions {
		if action.Reason != string(reconcile.ReasonMissingLock) {
			continue
		}
		missing++
		if action.Kind != string(reconcile.ActionKindError) ||
			action.Target != string(target.TargetClaudeCode) ||
			action.Scope != string(target.ScopeProject) ||
			action.Subject == nil ||
			action.Subject.Kind != "projection" ||
			action.Subject.Namespace != "claude-code.project.mcp-server" ||
			action.Subject.Name != "filesystem" ||
			action.Projection != nil {
			t.Fatalf("missing-lock action = %#v", action)
		}
	}
	if missing != 1 {
		t.Fatalf("missing-lock action count = %d, want 1; payload=%s", missing, stdout.String())
	}
}

func TestApplyDryRunBlocksDeclaredMCPSubjectMissingFromExistingLock(t *testing.T) {
	manifestPath := writeCLIPartialMCPLockFixture(t)
	root := filepath.Dir(manifestPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{
		"apply",
		"--manifest", manifestPath,
		"--target", string(target.TargetClaudeCode),
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 1 || stderr.Len() != 0 {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q, want 1 and empty stderr", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "subject=\"projection/claude-code.project.mcp-server/filesystem\"") ||
		!strings.Contains(stdout.String(), "blocked: missing lock") ||
		!strings.Contains(stdout.String(), "next: run daem lock --manifest") {
		t.Fatalf("stdout = %q, want missing subject blocker and lock remediation", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("host MCP config stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".daem", "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("statefile stat error = %v, want not exist", err)
	}
}

func TestApplyWriteRefusesDeclaredMCPSubjectMissingFromExistingLockBeforeMutation(t *testing.T) {
	manifestPath := writeCLIPartialMCPLockFixture(t)
	root := filepath.Dir(manifestPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{
		"apply",
		"--manifest", manifestPath,
		"--target", string(target.TargetClaudeCode),
		"--yes",
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q, want 1", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "missing_lock") ||
		!strings.Contains(stderr.String(), "next: run daem lock --manifest") {
		t.Fatalf("stderr = %q, want missing-lock refusal and lock remediation", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("host MCP config stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".daem", "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("statefile stat error = %v, want not exist", err)
	}
}

func writeCLIPartialMCPLockFixture(t *testing.T) string {
	t.Helper()
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
	writeCLILockCoverageManifest(t, manifestPath, true)
	return manifestPath
}

func writeCLILockCoverageManifest(t *testing.T, path string, includeFilesystem bool) {
	t.Helper()
	content := `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`
	if includeFilesystem {
		content += `
[[mcp_server]]
name = "filesystem"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "."]
`
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
