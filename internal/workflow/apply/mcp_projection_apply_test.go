package apply

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestRunProjectsClaudeProjectMCPSubjectThroughWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	selection := applyMCPSelection(t)
	serverID := "context7"

	createCommand := "must-not-run-daem-test"
	createArgs := []string{"--serve", "context7"}
	createResources := applyMCPEnvironment(
		t,
		serverID,
		targetpkg.TargetClaudeCode,
		createCommand,
		createArgs,
		map[string]string{"API_TOKEN": "DAEM_TEST_TOKEN"},
	)
	createLocked, createCanonical := applyMCPLockfile(t, serverID, createCommand, createArgs)
	createResult, err := run(
		t,
		context.Background(),
		paths,
		createResources,
		createLocked,
		selection,
		buildAggregateApplyAssessment(t, paths, createResources, createLocked, selection, false),
	)
	if err != nil {
		t.Fatalf("create Run returned error: %v", err)
	}
	if createResult.ActionCount != 1 {
		t.Fatalf("create ActionCount = %d, want 1", createResult.ActionCount)
	}
	mcpConfigPath := filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath)
	assertApplyMCPConfigEquivalent(t, mcpConfigPath, serverID, createCanonical)
	state := loadApplyStatefile(t, paths.StatefilePath)
	assertApplyMCPStateSubject(t, state, serverID, createCanonical)

	writeApplyFile(t, mcpConfigPath, `{
	  "project": "keep",
	  "mcpServers": {
	    "context7": {
	      "type": "stdio",
	      "command": "must-not-run-daem-test",
	      "args": ["--serve", "context7"],
	      "env": {"API_TOKEN": "${DAEM_TEST_TOKEN}"}
	    },
	    "manual": {
	      "type": "stdio",
	      "command": "node",
	      "args": ["manual.js"],
	      "env": {}
	    }
	  }
	}`)
	updateCommand := "must-not-run-daem-test-v2"
	updateArgs := []string{"--serve", "context7", "--verbose"}
	updateResources := applyMCPEnvironment(
		t,
		serverID,
		targetpkg.TargetClaudeCode,
		updateCommand,
		updateArgs,
		map[string]string{"API_TOKEN": "DAEM_TEST_TOKEN"},
	)
	updateLocked, updateCanonical := applyMCPLockfile(t, serverID, updateCommand, updateArgs)
	updateResult, err := run(
		t,
		context.Background(),
		paths,
		updateResources,
		updateLocked,
		selection,
		buildAggregateApplyAssessment(t, paths, updateResources, updateLocked, selection, false),
	)
	if err != nil {
		t.Fatalf("update Run returned error: %v", err)
	}
	if updateResult.ActionCount != 1 {
		t.Fatalf("update ActionCount = %d, want 1", updateResult.ActionCount)
	}
	assertApplyMCPConfigEquivalent(t, mcpConfigPath, serverID, updateCanonical)
	assertApplyMCPConfigPreservesManualFields(t, mcpConfigPath)
	state = loadApplyStatefile(t, paths.StatefilePath)
	assertApplyMCPStateSubject(t, state, serverID, updateCanonical)

	removeResources := applyEmptyEnvironment(t, targetpkg.TargetClaudeCode)
	removeLocked := lock.File{Version: lock.CurrentVersion}
	removeResult, err := run(
		t,
		context.Background(),
		paths,
		removeResources,
		removeLocked,
		selection,
		buildAggregateApplyAssessment(t, paths, removeResources, removeLocked, selection, false),
	)
	if err != nil {
		t.Fatalf("remove Run returned error: %v", err)
	}
	if removeResult.ActionCount != 1 {
		t.Fatalf("remove ActionCount = %d, want 1", removeResult.ActionCount)
	}
	assertApplyMCPConfigMissing(t, mcpConfigPath, serverID)
	assertApplyMCPConfigPreservesManualFields(t, mcpConfigPath)
	state = loadApplyStatefile(t, paths.StatefilePath)
	assertApplyMCPStateSubjectMissing(t, state, serverID)
	assertNoApplyRecoveryArtifacts(t, paths)
}

func TestRunProjectsOpenCodeProjectMCPSubjectThroughWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	selection := applyOpenCodeMCPSelection(t)
	serverID := "context7"

	command := "must-not-run-daem-test"
	args := []string{"--serve", "context7"}
	resources := applyMCPEnvironment(t, serverID, targetpkg.TargetOpenCode, command, args, nil)
	createLocked, createCanonical := applyOpenCodeMCPLockfile(t, serverID, command, args)
	createResult, err := run(
		t,
		context.Background(),
		paths,
		resources,
		createLocked,
		selection,
		buildAggregateApplyAssessment(t, paths, resources, createLocked, selection, false),
	)
	if err != nil {
		t.Fatalf("create Run returned error: %v", err)
	}
	if createResult.ActionCount != 1 {
		t.Fatalf("create ActionCount = %d, want 1", createResult.ActionCount)
	}
	mcpConfigPath := filepath.Join(tempDir, aggregate.OpenCodeProjectMCPConfigPath)
	assertApplyOpenCodeMCPConfigEquivalent(t, mcpConfigPath, serverID, createCanonical)
	state := loadApplyStatefile(t, paths.StatefilePath)
	assertApplyOpenCodeMCPStateSubject(t, state, serverID, createCanonical)
	assertNoApplyRecoveryArtifacts(t, paths)
}

func TestRunBlocksOpenCodeProjectMCPProjectionWhenJSONCAlternateConfigExists(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	selection := applyOpenCodeMCPSelection(t)
	serverID := "context7"
	command := "must-not-run-daem-test"
	args := []string{"--serve", "context7"}
	resources := applyMCPEnvironment(t, serverID, targetpkg.TargetOpenCode, command, args, nil)
	locked, _ := applyOpenCodeMCPLockfile(t, serverID, command, args)
	mcpConfigPath := filepath.Join(tempDir, aggregate.OpenCodeProjectMCPConfigPath)
	existing := `{"mcp":{"manual":{"type":"remote","url":"https://example.invalid/mcp"}},"model":"keep"}`
	writeApplyFile(t, mcpConfigPath, existing)
	writeApplyFile(t, mcpConfigPath+"c", `{"mcp":{"context7":{"type":"local","command":["node"]}}}`)

	_, err := run(
		t,
		context.Background(),
		paths,
		resources,
		locked,
		selection,
		buildAggregateApplyAssessment(t, paths, resources, locked, selection, false),
	)

	if err == nil || !strings.Contains(err.Error(), "unsupported alternate config") {
		t.Fatalf("Run error = %v, want unsupported alternate config block", err)
	}
	assertApplyFileContent(t, mcpConfigPath, existing)
	if _, err := os.Stat(paths.StatefilePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("statefile stat err = %v, want no statefile after blocked OpenCode MCP apply", err)
	}
}

func TestRunManagesExistingOpenCodeProjectMCPProjectionOnlyOnExactMatch(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	selection := applyOpenCodeMCPSelection(t)
	serverID := "context7"
	command := "must-not-run-daem-test"
	args := []string{"--serve", "context7"}
	resources := applyMCPEnvironment(t, serverID, targetpkg.TargetOpenCode, command, args, nil)
	locked, canonical := applyOpenCodeMCPLockfile(t, serverID, command, args)
	mcpConfigPath := filepath.Join(tempDir, aggregate.OpenCodeProjectMCPConfigPath)
	writeApplyFile(t, mcpConfigPath, `{"mcp":{"context7":`+string(canonical)+`}}`)

	withoutManage := buildAggregateApplyAssessment(t, paths, resources, locked, selection, false)
	withoutDecision := requireApplyMCPAggregateDecision(t, withoutManage.Reconciliation, serverID)
	if withoutDecision.Kind() != reconcile.AggregateBlocked ||
		withoutDecision.Reason() != reconcile.ReasonUnmanagedOutputExists {
		t.Fatalf("plan without manage-existing = %#v, want unmanaged error", withoutDecision)
	}
	managedResult, err := run(
		t,
		context.Background(),
		paths,
		resources,
		locked,
		selection,
		buildAggregateApplyAssessment(t, paths, resources, locked, selection, true),
	)
	if err != nil {
		t.Fatalf("manage-existing Run returned error: %v", err)
	}
	if managedResult.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want one record action", managedResult.ActionCount)
	}
	state := loadApplyStatefile(t, paths.StatefilePath)
	assertApplyOpenCodeMCPStateSubject(t, state, serverID, canonical)
	assertApplyOpenCodeMCPConfigEquivalent(t, mcpConfigPath, serverID, canonical)

	mismatchDir := t.TempDir()
	mismatchPaths := applyTestPaths(t, mismatchDir)
	writeApplyFile(t, filepath.Join(mismatchDir, aggregate.OpenCodeProjectMCPConfigPath), `{"mcp":{"context7":{"type":"local","command":["node","server.js"]}}}`)
	mismatchPlan := buildAggregateApplyAssessment(t, mismatchPaths, resources, locked, selection, true)
	mismatchDecision := requireApplyMCPAggregateDecision(t, mismatchPlan.Reconciliation, serverID)
	if mismatchDecision.Kind() != reconcile.AggregateBlocked ||
		mismatchDecision.Reason() != reconcile.ReasonUnmanagedOutputExists {
		t.Fatalf("mismatch plan = %#v, want unmanaged error", mismatchDecision)
	}
}

func TestRunProjectsCodexProjectMCPSubjectThroughWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	selection := applyCodexMCPSelection(t)
	serverID := "context7"

	command := "must-not-run-daem-test"
	args := []string{"--serve", "context7"}
	resources := applyMCPEnvironment(t, serverID, targetpkg.TargetCodex, command, args, nil)
	locked, canonical := applyCodexMCPLockfile(t, serverID, command, args)
	result, err := run(
		t,
		context.Background(),
		paths,
		resources,
		locked,
		selection,
		buildAggregateApplyAssessment(t, paths, resources, locked, selection, false),
	)
	if err != nil {
		t.Fatalf("create Run returned error: %v", err)
	}
	if result.ActionCount != 1 {
		t.Fatalf("create ActionCount = %d, want 1", result.ActionCount)
	}
	mcpConfigPath := filepath.Join(tempDir, aggregate.CodexProjectMCPConfigPath)
	assertApplyCodexMCPConfigEquivalent(t, mcpConfigPath, serverID, canonical)
	state := loadApplyStatefile(t, paths.StatefilePath)
	assertApplyCodexMCPStateSubject(t, state, serverID, canonical)
	assertNoApplyRecoveryArtifacts(t, paths)
}

func TestRunManagesExistingCodexProjectMCPProjectionOnlyOnExactMatch(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	selection := applyCodexMCPSelection(t)
	serverID := "context7"
	command := "must-not-run-daem-test"
	args := []string{"--serve", "context7"}
	resources := applyMCPEnvironment(t, serverID, targetpkg.TargetCodex, command, args, nil)
	locked, canonical := applyCodexMCPLockfile(t, serverID, command, args)
	mcpConfigPath := filepath.Join(tempDir, aggregate.CodexProjectMCPConfigPath)
	writeApplyFile(t, mcpConfigPath, `[mcp_servers.context7]
`+string(canonical))

	withoutManage := buildAggregateApplyAssessment(t, paths, resources, locked, selection, false)
	withoutDecision := requireApplyMCPAggregateDecision(t, withoutManage.Reconciliation, serverID)
	if withoutDecision.Kind() != reconcile.AggregateBlocked ||
		withoutDecision.Reason() != reconcile.ReasonUnmanagedOutputExists {
		t.Fatalf("plan without manage-existing = %#v, want unmanaged error", withoutDecision)
	}
	managedResult, err := run(
		t,
		context.Background(),
		paths,
		resources,
		locked,
		selection,
		buildAggregateApplyAssessment(t, paths, resources, locked, selection, true),
	)
	if err != nil {
		t.Fatalf("manage-existing Run returned error: %v", err)
	}
	if managedResult.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want one record action", managedResult.ActionCount)
	}
	state := loadApplyStatefile(t, paths.StatefilePath)
	assertApplyCodexMCPStateSubject(t, state, serverID, canonical)
	assertApplyCodexMCPConfigEquivalent(t, mcpConfigPath, serverID, canonical)

	mismatchDir := t.TempDir()
	mismatchPaths := applyTestPaths(t, mismatchDir)
	writeApplyFile(t, filepath.Join(mismatchDir, aggregate.CodexProjectMCPConfigPath), `[mcp_servers.context7]
command = "node"
args = ["server.js"]
`)
	mismatchPlan := buildAggregateApplyAssessment(t, mismatchPaths, resources, locked, selection, true)
	mismatchDecision := requireApplyMCPAggregateDecision(t, mismatchPlan.Reconciliation, serverID)
	if mismatchDecision.Kind() != reconcile.AggregateBlocked ||
		mismatchDecision.Reason() != reconcile.ReasonUnmanagedOutputExists {
		t.Fatalf("mismatch plan = %#v, want unmanaged error", mismatchDecision)
	}
}

func applyOpenCodeMCPSelection(t *testing.T) targetselection.Selection {
	t.Helper()
	selection, err := targetselection.ForAvailableTargets([]targetpkg.Target{targetpkg.TargetOpenCode}, []string{string(targetpkg.TargetOpenCode)})
	if err != nil {
		t.Fatalf("build OpenCode MCP selection: %v", err)
	}
	return selection
}

func applyCodexMCPSelection(t *testing.T) targetselection.Selection {
	t.Helper()
	selection, err := targetselection.ForAvailableTargets([]targetpkg.Target{targetpkg.TargetCodex}, []string{string(targetpkg.TargetCodex)})
	if err != nil {
		t.Fatalf("build Codex MCP selection: %v", err)
	}
	return selection
}

func applyOpenCodeMCPLockfile(t *testing.T, serverID string, command string, args []string) (lock.File, []byte) {
	t.Helper()
	projection := mcpcodec.MCPNoEnvServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            args,
		AdapterContract: aggregate.OpenCodeProjectMCPLocalCommandV1,
	}
	canonical, err := mcpcodec.CanonicalOpenCodeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalOpenCodeProjectMCPServerEntry returned error: %v", err)
	}
	server, binding := applyMCPStdioServer(t, serverID, targetpkg.TargetOpenCode, command, args, nil)
	graph, err := topologymcp.Binding(server, binding)
	if err != nil {
		t.Fatalf("MCPServer returned error: %v", err)
	}
	placement, ok := aggregate.ImplementedMCPPlacement(targetpkg.TargetOpenCode, targetpkg.ScopeProject)
	if !ok {
		t.Fatal("OpenCode project MCP placement is unavailable")
	}
	record, err := lock.NewMCPProjectionSubjectContract(lock.MCPProjectionSubjectInput{
		Graph:               graph,
		EntityID:            server.ID(),
		PlacementID:         placement.ID(),
		ServerID:            serverID,
		RequestedOnAbsent:   desiredmcp.OnAbsentRemoveBinding,
		LauncherCommand:     command,
		LauncherArgs:        args,
		CanonicalProjection: string(canonical),
	})
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}
	return snapshottest.File(t, record), canonical
}

func applyCodexMCPLockfile(t *testing.T, serverID string, command string, args []string) (lock.File, []byte) {
	t.Helper()
	projection := mcpcodec.MCPNoEnvServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            args,
		AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
	}
	canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalCodexProjectMCPServerEntry returned error: %v", err)
	}
	server, binding := applyMCPStdioServer(t, serverID, targetpkg.TargetCodex, command, args, nil)
	graph, err := topologymcp.Binding(server, binding)
	if err != nil {
		t.Fatalf("MCPServer returned error: %v", err)
	}
	placement, ok := aggregate.ImplementedMCPPlacement(targetpkg.TargetCodex, targetpkg.ScopeProject)
	if !ok {
		t.Fatal("Codex project MCP placement is unavailable")
	}
	record, err := lock.NewMCPProjectionSubjectContract(lock.MCPProjectionSubjectInput{
		Graph:               graph,
		EntityID:            server.ID(),
		PlacementID:         placement.ID(),
		ServerID:            serverID,
		RequestedOnAbsent:   desiredmcp.OnAbsentRemoveBinding,
		LauncherCommand:     command,
		LauncherArgs:        args,
		CanonicalProjection: string(canonical),
	})
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}
	return snapshottest.File(t, record), canonical
}

func assertApplyCodexMCPConfigEquivalent(t *testing.T, path string, serverID string, canonical []byte) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Codex MCP config: %v", err)
	}
	comparison, err := compareApplyMCPPlacementCanonicalEntry(t, aggregate.MCPPlacementCodexProject, content, serverID, canonical)
	if err != nil {
		t.Fatalf("CompareCodexProjectMCPServerCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present equivalent Codex MCP projection", comparison)
	}
}

func assertApplyCodexMCPStateSubject(t *testing.T, state durable.Snapshot, serverID string, canonical []byte) {
	t.Helper()
	assertApplyMCPAggregateStateSubject(
		t,
		state,
		serverID,
		aggregate.MCPPlacementCodexProject,
		canonical,
	)
}

func assertApplyOpenCodeMCPConfigEquivalent(t *testing.T, path string, serverID string, canonical []byte) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read OpenCode MCP config: %v", err)
	}
	comparison, err := compareApplyMCPPlacementCanonicalEntry(t, aggregate.MCPPlacementOpenCodeProject, content, serverID, canonical)
	if err != nil {
		t.Fatalf("CompareOpenCodeProjectMCPServerCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present equivalent OpenCode MCP projection", comparison)
	}
}

func assertApplyOpenCodeMCPStateSubject(t *testing.T, state durable.Snapshot, serverID string, canonical []byte) {
	t.Helper()
	assertApplyMCPAggregateStateSubject(
		t,
		state,
		serverID,
		aggregate.MCPPlacementOpenCodeProject,
		canonical,
	)
}

func assertApplyMCPConfigMissing(t *testing.T, path string, serverID string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read MCP config: %v", err)
	}
	if _, present, err := mcpcodec.ExtractClaudeProjectMCPServerProjection(content, serverID); err != nil {
		t.Fatalf("ExtractClaudeProjectMCPServerProjection returned error: %v", err)
	} else if present {
		t.Fatalf("MCP server %q is still present", serverID)
	}
}

func assertApplyMCPConfigPreservesManualFields(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read MCP config: %v", err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("decode MCP config: %v", err)
	}
	var project string
	if err := json.Unmarshal(config["project"], &project); err != nil {
		t.Fatalf("decode project field: %v", err)
	}
	if project != "keep" {
		t.Fatalf("project = %q, want keep", project)
	}
	if _, present, err := mcpcodec.ExtractClaudeProjectMCPServerProjection(content, "manual"); err != nil {
		t.Fatalf("manual MCP extraction returned error: %v", err)
	} else if !present {
		t.Fatal("manual MCP server was not preserved")
	}
}

func assertApplyMCPStateSubjectMissing(t *testing.T, snapshot durable.Snapshot, serverID string) {
	t.Helper()
	for _, stateResource := range snapshot.ManagedAggregates() {
		subject := stateResource.Subject()
		if subject.Kind() == topology.SubjectProjection &&
			subject.Namespace() == "claude-code.project.mcp-server" &&
			subject.Key() == serverID {
			t.Fatalf("MCP subject state for %q unexpectedly present: %#v", serverID, stateResource)
		}
	}
}
