package cli_test

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestMCPPublicCLIManageExistingAdoptsExactProjectEntry(t *testing.T) {
	t.Setenv("CONTEXT7_API_TOKEN", "test-token")
	canary := execcheck.New(t, "claude", "npx", "headers-helper-daem-test")
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: "npx",
		Args:    []string{"-y", "@example/mcp-server"},
		Env:     map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	}
	writeMCPManifest(t, project.root, spec)
	canonical := canonicalMCPEntryForSpec(t, "context7", spec)
	writeMCPConfigWithSibling(t, project.root, `"context7":{"env":{"API_TOKEN":"${CONTEXT7_API_TOKEN}"},"args":["-y","@example/mcp-server"],"command":"npx","type":"stdio"}`)
	beforeConfig := readMCPConfig(t, project.root)
	runMCPLock(t, project)
	execcheck.AssertClean(t, canary, "lock")

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--dry-run", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("apply without manage-existing exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	ordinary := clijson.DecodePlan(t, []byte(stdout))
	assertPlanSubjectActionReason(t, ordinary, "error", "unmanaged_output_exists", "context7")
	assertMCPJSONDimension(t, ordinary, "project_projection", "unmanaged_same_name", "ROUTE_PREEXISTING_UNOWNED")
	assertMCPJSONDimension(t, ordinary, "same_scope_ownership", "unmanaged_same_name", "ROUTE_PREEXISTING_UNOWNED")
	assertNoPublicMCPOutputLeaks(t, stdout)
	execcheck.AssertClean(t, canary, "ordinary apply dry-run")

	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--manage-existing", "--dry-run", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply manage-existing dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	manageDryRun := clijson.DecodePlan(t, []byte(stdout))
	assertPlanSubjectActionReason(t, manageDryRun, "record", "managed_existing", "context7")
	assertMCPJSONDimension(t, manageDryRun, "project_projection", "unmanaged_same_name", "ROUTE_PREEXISTING_UNOWNED")
	assertNoPublicMCPOutputLeaks(t, stdout)
	assertMCPConfigBytesEqual(t, project.root, beforeConfig, "manage-existing dry-run")
	assertMCPStatefileMissing(t, project.root, "manage-existing dry-run")
	execcheck.AssertClean(t, canary, "manage-existing dry-run")

	stdout = runMCPCLIWithSuccessfulDelegate(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--manage-existing", "--yes", "--json")
	manageResult := clijson.DecodeApplyResult(t, []byte(stdout))
	assertApplyResultSubjectActionReason(t, manageResult, "record", "managed_existing", "context7")
	if len(manageResult.MCPStatuses) != 0 {
		t.Fatalf("mcp_statuses = %#v, want no stale pre-apply status on successful write", manageResult.MCPStatuses)
	}
	assertNoPublicMCPOutputLeaks(t, stdout)
	assertMCPConfigBytesEqual(t, project.root, beforeConfig, "manage-existing write")
	state := loadMCPStatefile(t, project.root)
	assertMCPStateSubject(t, state, "context7")
	assertMCPStateSubjectHash(t, state, "context7", string(artifact.HashFileContent(canonical)))
	execcheck.AssertClean(t, canary, "manage-existing write")

	exitCode, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--target", "claude-code", "--check", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("post-adoption status exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	statusPayload := clijson.DecodePlan(t, []byte(stdout))
	assertPlanSubjectActionReason(t, statusPayload, "noop", "already_current", "context7")
	assertMCPJSONDimension(t, statusPayload, "project_projection", "projected", "")
	assertMCPJSONDimension(t, statusPayload, "same_scope_ownership", "managed", "")
	assertMCPJSONDimension(t, statusPayload, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertMCPJSONDimension(t, statusPayload, "protocol_initialize", "not_probed", "RUNTIME_NOT_PROBED")
	assertMCPJSONDimension(t, statusPayload, "adoption_orphan_residue", "not_applicable", "")
	assertNoPublicMCPOutputLeaks(t, stdout)
	execcheck.AssertClean(t, canary, "post-adoption status")

	writeMCPManifestWithoutServers(t, project.root)
	runMCPLock(t, project)
	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("post-adoption removal exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	removeResult := clijson.DecodeApplyResult(t, []byte(stdout))
	assertApplyResultSubjectActionReason(t, removeResult, "delete", "removed_from_manifest", "context7")
	assertMCPConfigMissing(t, project.root, "context7")
	assertMCPConfigPreservesHostFields(t, project.root)
	state = loadMCPStatefile(t, project.root)
	assertMCPStateSubjectMissing(t, state, "context7")
	execcheck.AssertClean(t, canary, "post-adoption removal")
}

func TestMCPPublicCLIManageExistingBlocksFirstMutationAfterBaselineDrift(t *testing.T) {
	project := newMCPCLIProject(t)
	initial := mcpManifestSpec{Command: "npx", Args: []string{"-y", "@example/mcp-server"}}
	writeMCPManifest(t, project.root, initial)
	writeMCPConfigWithSibling(t, project.root, `"context7":`+string(canonicalMCPEntryForSpec(t, "context7", initial)))
	runMCPLock(t, project)
	stdout := runMCPCLIWithSuccessfulDelegate(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--manage-existing", "--yes", "--json")

	updated := mcpManifestSpec{Command: "npx", Args: []string{"-y", "@example/mcp-server", "--verbose"}}
	writeMCPManifest(t, project.root, updated)
	runMCPLock(t, project)
	writeMCPConfigWithSibling(t, project.root, `"context7":{"type":"stdio","command":"drifted-daem-test","args":[],"env":{}}`)
	driftedConfig := readMCPConfig(t, project.root)

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("drifted update exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	drifted := clijson.DecodeApplyResult(t, []byte(stdout))
	assertApplyResultSubjectActionReason(t, drifted, "error", "drifted_output", "context7")
	assertMCPConfigBytesEqual(t, project.root, driftedConfig, "drifted update")
}

func TestMCPPublicCLIManageExistingRejectsUnsupportedOrNonEquivalentEntries(t *testing.T) {
	tests := []struct {
		name            string
		config          string
		projectionState string
		reason          string
	}{
		{
			name:            "malformed",
			config:          `{"mcpServers":`,
			projectionState: "malformed",
			reason:          "CONFIG_MALFORMED",
		},
		{
			name:            "unsupported managed field",
			config:          `{"mcpServers":{"context7":{"type":"stdio","command":"npx","args":[],"env":{},"cwd":"/tmp"}}}`,
			projectionState: "unsupported",
			reason:          "UNSUPPORTED_MANAGED_FIELD",
		},
		{
			name:            "non equivalent same name",
			config:          `{"mcpServers":{"context7":{"type":"stdio","command":"node","args":["server.js"],"env":{}}}}`,
			projectionState: "unmanaged_same_name",
			reason:          "ROUTE_PREEXISTING_UNOWNED",
		},
		{
			name:            "literal secret",
			config:          `{"mcpServers":{"context7":{"type":"stdio","command":"npx","args":[],"env":{"API_TOKEN":"literal-secret-canary"}}}}`,
			projectionState: "unsupported",
			reason:          "SECRET_LITERAL_FORBIDDEN",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project := newMCPCLIProject(t)
			writeMCPManifest(t, project.root, mcpManifestSpec{
				Command: "npx",
				Args:    []string{"-y", "@example/mcp-server"},
			})
			runMCPLock(t, project)
			testkit.WriteFile(t, project.root, aggregate.ClaudeProjectMCPConfigPath, test.config)

			exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--manage-existing", "--dry-run", "--json")
			if exitCode != 1 || stderr != "" {
				t.Fatalf("manage-existing dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			payload := clijson.DecodePlan(t, []byte(stdout))
			assertPlanSubjectActionReason(t, payload, "error", "unmanaged_output_exists", "context7")
			assertMCPJSONDimension(t, payload, "project_projection", test.projectionState, test.reason)
			assertNoPublicMCPOutputLeaks(t, stdout)
			assertMCPStatefileMissing(t, project.root, "failed manage-existing")
		})
	}
}
