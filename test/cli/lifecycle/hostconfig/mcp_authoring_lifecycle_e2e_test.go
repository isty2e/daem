package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestMCPPublicCLIAddRemoveAuthoringLifecycleEndToEnd(t *testing.T) {
	canary := execcheck.New(t, "claude", "npx", "node")
	project := newMCPCLIProject(t)
	testkit.WithWorkingDirectory(t, project.root)
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")
	externalCacheMarker := filepath.Join(t.TempDir(), "npx-cache-marker")
	if err := os.WriteFile(externalCacheMarker, []byte("external cache residue"), 0o600); err != nil {
		t.Fatalf("write external cache marker: %v", err)
	}
	residueFiles := map[string]string{
		"bin/context7-mcp": "project executable residue",
		"node_modules/@example/mcp-server/package.json":     `{"name":"@example/mcp-server"}`,
		"node_modules/.cache/context7/package-cache-marker": "project package cache residue",
		".mcp/credentials/context7.json":                    `{"token":"must-stay-out-of-daem"}`,
		".mcp/trust/context7.json":                          `{"approved":true}`,
		".mcp/sessions/context7/session.json":               `{"session":"host-owned"}`,
	}
	for path, content := range residueFiles {
		testkit.WriteFile(t, project.root, path, content)
	}
	spec := mcpManifestSpec{
		Command: "npx",
		Args:    []string{"-y", "@example/mcp-server"},
	}

	addOutput := runMCPCLIExpect(
		t, 0, "add mcp-server write",
		"add", "mcp-server", "context7", spec.Command,
		"--arg", spec.Args[0],
		"--arg", spec.Args[1],
		"--json",
	)
	assertNoPublicMCPOutputLeaks(t, addOutput)
	execcheck.AssertClean(t, canary, "add mcp-server write")

	lockOutput := runMCPCLIExpect(t, 0, "lock dry-run after add", "lock", "--dry-run", "--json")
	assertNoPublicMCPOutputLeaks(t, lockOutput)
	execcheck.AssertClean(t, canary, "lock dry-run after add")

	writeMCPConfigWithSibling(t, project.root, "")
	delegateAttempts := 0
	applyOptions := clipkg.RunOptions{ApplyExecuteOptions: applyworkflow.ExecuteOptions{
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
				delegateAttempts++
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	}}
	exitCode, applyCreateOutput, applyCreateStderr := runMCPCLIWithOptions(t, []string{"apply", "--yes", "--json"}, applyOptions)
	if exitCode != 0 || applyCreateStderr != "" {
		t.Fatalf("apply write create exitCode=%d stdout=%q stderr=%q", exitCode, applyCreateOutput, applyCreateStderr)
	}
	assertNoPublicMCPOutputLeaks(t, applyCreateOutput)
	assertMCPConfigEquivalent(t, project.root, "context7", spec)
	assertMCPConfigPreservesHostFields(t, project.root)
	state := loadMCPStatefile(t, project.root)
	assertMCPStateSubject(t, state, "context7")
	execcheck.AssertClean(t, canary, "apply write create")

	statusOutput := runMCPCLIExpect(t, 0, "status check projected", "status", "--check", "--json")
	assertNoPublicMCPOutputLeaks(t, statusOutput)
	assertMCPJSONDimension(t, clijson.DecodePlan(t, []byte(statusOutput)), "project_projection", "projected", "")
	execcheck.AssertClean(t, canary, "status check projected")

	exitCode, repeatApplyOutput, repeatApplyStderr := runMCPCLIWithOptions(t, []string{"apply", "--yes", "--json"}, applyOptions)
	if exitCode != 0 || repeatApplyStderr != "" {
		t.Fatalf("apply write idempotent exitCode=%d stdout=%q stderr=%q", exitCode, repeatApplyOutput, repeatApplyStderr)
	}
	assertNoPublicMCPOutputLeaks(t, repeatApplyOutput)
	repeatApplyResult := clijson.DecodeApplyResult(t, []byte(repeatApplyOutput))
	if repeatApplyResult.HasErrors || repeatApplyResult.ActionCount != 0 {
		t.Fatalf("repeat apply payload = %#v, want clean idempotent no-op", repeatApplyResult)
	}
	if delegateAttempts != 2 {
		t.Fatalf("delegate attempts = %d, want one ordinary attempt per confirmed apply", delegateAttempts)
	}
	execcheck.AssertClean(t, canary, "apply write idempotent")

	removeOutput := runMCPCLIExpect(
		t, 0, "remove mcp-server write",
		"remove", "mcp-server", "context7",
		"--json",
	)
	assertNoPublicMCPOutputLeaks(t, removeOutput)
	if strings.Contains(removeOutput, "next_steps") {
		t.Fatalf("remove output = %q, did not want human next steps in JSON", removeOutput)
	}
	assertMCPConfigEquivalent(t, project.root, "context7", spec)
	execcheck.AssertClean(t, canary, "remove mcp-server write")

	lockAfterRemove := runMCPCLIExpect(t, 0, "lock dry-run after remove", "lock", "--dry-run", "--json")
	assertNoPublicMCPOutputLeaks(t, lockAfterRemove)
	execcheck.AssertClean(t, canary, "lock dry-run after remove")

	applyRemoveOutput := runMCPCLIExpect(t, 0, "apply write removal", "apply", "--target", "claude-code", "--yes", "--json")
	assertNoPublicMCPOutputLeaks(t, applyRemoveOutput)
	removeResult := clijson.DecodeApplyResult(t, []byte(applyRemoveOutput))
	assertApplyResultSubjectActionReason(t, removeResult, "delete", "removed_from_manifest", "context7")
	assertMCPConfigMissing(t, project.root, "context7")
	assertMCPConfigPreservesHostFields(t, project.root)
	state = loadMCPStatefile(t, project.root)
	assertMCPStateSubjectMissing(t, state, "context7")
	if content, err := os.ReadFile(externalCacheMarker); err != nil || string(content) != "external cache residue" {
		t.Fatalf("external cache marker = %q, err=%v; remove must not prune external cache", content, err)
	}
	for path, content := range residueFiles {
		testkit.AssertFileContent(t, filepath.Join(project.root, path), content)
	}
	execcheck.AssertClean(t, canary, "apply write removal")

	postRemoveStatus := runMCPCLIExpect(t, 0, "status check after removal", "status", "--check", "--json")
	assertNoPublicMCPOutputLeaks(t, postRemoveStatus)
	if got := clijson.DecodePlan(t, []byte(postRemoveStatus)); len(got.MCPStatuses) != 0 {
		t.Fatalf("post-remove MCP statuses = %#v, want none", got.MCPStatuses)
	}
	execcheck.AssertClean(t, canary, "status check after removal")
}
