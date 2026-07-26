package cli_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestMCPPublicCLIProjectionLifecycleCoversApplyStatusUpdateRemoval(t *testing.T) {
	t.Setenv("CONTEXT7_API_TOKEN", "test-token")
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "must-not-run-daem-test",
		Args:    []string{"--serve", "context7"},
		Env:     map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	})
	writeMCPConfigWithSibling(t, project.root, "")
	runMCPLock(t, project)

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--dry-run", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply dry-run json exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	dryRun := clijson.DecodePlan(t, []byte(stdout))
	assertPlanSubjectAction(t, dryRun, "create", "context7")
	assertMCPJSONDimension(t, dryRun, "project_projection", "missing", "")
	assertMCPJSONDimension(t, dryRun, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertMCPJSONDimension(t, dryRun, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	assertNoPublicMCPOutputLeaks(t, stdout)

	stdout = runMCPCLIWithSuccessfulDelegate(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	applyResult := clijson.DecodeApplyResult(t, []byte(stdout))
	if applyResult.ActionCount != 1 || len(applyResult.Actions) != 1 || applyResult.Actions[0].Subject == nil {
		t.Fatalf("apply result actions = %#v", applyResult.Actions)
	}
	if len(applyResult.MCPStatuses) != 0 {
		t.Fatalf("mcp_statuses = %#v, want no stale pre-apply status on successful write", applyResult.MCPStatuses)
	}
	assertMCPConfigEquivalent(t, project.root, "context7", mcpManifestSpec{
		Command: "must-not-run-daem-test",
		Args:    []string{"--serve", "context7"},
		Env:     map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	})
	assertMCPConfigPreservesHostFields(t, project.root)
	state := loadMCPStatefile(t, project.root)
	assertMCPStateSubject(t, state, "context7")
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--target", "claude-code", "--check", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("status check json exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	statusPayload := clijson.DecodePlan(t, []byte(stdout))
	if statusPayload.HasErrors || statusPayload.ActionCount != 1 || statusPayload.Actions[0].Kind != "noop" {
		t.Fatalf("status payload = %#v, want clean noop projection", statusPayload)
	}
	assertMCPJSONDimension(t, statusPayload, "project_projection", "projected", "")
	assertMCPJSONDimension(t, statusPayload, "same_scope_ownership", "managed", "")
	assertMCPJSONDimension(t, statusPayload, "effective_shadowing", "unobserved", "EFFECTIVE_STATE_UNOBSERVED")
	assertMCPJSONDimension(t, statusPayload, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertMCPJSONDimension(t, statusPayload, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	assertMCPJSONDimension(t, statusPayload, "runtime_authentication", "not_probed", "RUNTIME_NOT_PROBED")
	assertMCPJSONDimension(t, statusPayload, "endpoint_health", "not_probed", "RUNTIME_NOT_PROBED")
	assertMCPJSONDimension(t, statusPayload, "tool_inventory", "not_probed", "RUNTIME_NOT_PROBED")
	assertMCPJSONDimension(t, statusPayload, "adoption_orphan_residue", "not_applicable", "")
	assertNoPublicMCPOutputLeaks(t, stdout)

	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "must-not-run-daem-test-v2",
		Args:    []string{"--serve", "context7", "--verbose"},
		Env:     map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	})
	runMCPLock(t, project)
	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--dry-run", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("update dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	updatePlan := clijson.DecodePlan(t, []byte(stdout))
	assertPlanSubjectAction(t, updatePlan, "update", "context7")

	stdout = runMCPCLIWithSuccessfulDelegate(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes")
	if strings.Count(stdout, "applied: 1 actions") != 1 ||
		!strings.Contains(stdout, `update subject="projection/`+"claude-code.project.mcp-server"+`/context7"`) {
		t.Fatalf("stdout = %q, want one update subject action", stdout)
	}
	if strings.Contains(stdout, "mcp status:") {
		t.Fatalf("stdout = %q, want no stale pre-apply MCP status on successful write", stdout)
	}
	assertMCPConfigEquivalent(t, project.root, "context7", mcpManifestSpec{
		Command: "must-not-run-daem-test-v2",
		Args:    []string{"--serve", "context7", "--verbose"},
		Env:     map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	})
	assertMCPConfigPreservesHostFields(t, project.root)
	state = loadMCPStatefile(t, project.root)
	assertMCPStateSubject(t, state, "context7")
	assertNoPublicMCPOutputLeaks(t, stdout)

	writeMCPConfigWithSibling(t, project.root, `"context7":{"type":"stdio","command":"drifted-daem-test","args":[],"env":{}}`)
	exitCode, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--check", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("drift status exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	driftStatus := clijson.DecodePlan(t, []byte(stdout))
	assertMCPJSONDimension(t, driftStatus, "project_projection", "drifted", "")
	assertNoPublicMCPOutputLeaks(t, stdout)

	cleanUpdateSpec := mcpManifestSpec{
		Command: "must-not-run-daem-test-v2",
		Args:    []string{"--serve", "context7", "--verbose"},
		Env:     map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	}
	writeMCPConfigWithSibling(t, project.root, `"context7":`+string(canonicalMCPEntryForSpec(t, "context7", cleanUpdateSpec)))
	assertMCPConfigEquivalent(t, project.root, "context7", cleanUpdateSpec)

	writeMCPManifestWithoutServers(t, project.root)
	runMCPLock(t, project)
	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--dry-run", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("remove dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	removePlan := clijson.DecodePlan(t, []byte(stdout))
	assertPlanSubjectAction(t, removePlan, "delete", "context7")
	if removePlan.Actions[0].PreviousState == nil || removePlan.Actions[0].PreviousState.Subject == nil {
		t.Fatalf("remove action previous_state = %#v, want subject-owned state", removePlan.Actions[0].PreviousState)
	}

	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("remove apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	removeResult := clijson.DecodeApplyResult(t, []byte(stdout))
	if removeResult.ActionCount != 1 || len(removeResult.Actions) != 1 || removeResult.Actions[0].Kind != "delete" {
		t.Fatalf("remove result actions = %#v", removeResult.Actions)
	}
	assertMCPConfigMissing(t, project.root, "context7")
	assertMCPConfigPreservesHostFields(t, project.root)
	state = loadMCPStatefile(t, project.root)
	assertMCPStateSubjectMissing(t, state, "context7")
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--check", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("post-remove status exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	postRemoveStatus := clijson.DecodePlan(t, []byte(stdout))
	if len(postRemoveStatus.MCPStatuses) != 0 || postRemoveStatus.HasErrors {
		t.Fatalf("post-remove status = %#v, want no selected MCP projection status", postRemoveStatus)
	}
}
