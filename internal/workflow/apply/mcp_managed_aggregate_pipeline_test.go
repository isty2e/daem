package apply

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestManagedAggregateMCPPipelineCreatesThroughCommand(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	writeApplyFile(t, manifestPath, `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)
	if _, err := workflowlock.RunLock(
		context.Background(),
		workflowlock.LockInput{ManifestPath: manifestPath},
	); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}

	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	if len(planned.Reconciliation.Aggregates()) != 1 {
		t.Fatalf("aggregate decisions = %#v, want one document decision", planned.Reconciliation.Aggregates())
	}
	if planned.Reconciliation.Aggregates()[0].CodecContractID() != aggregate.ClaudeProjectMCPStdioAdapterV1 {
		t.Fatalf(
			"aggregate codec = %q, want %q",
			planned.Reconciliation.Aggregates()[0].CodecContractID(),
			aggregate.ClaudeProjectMCPStdioAdapterV1,
		)
	}

	executed, err := ExecuteWithOptions(context.Background(), planned, successfulMCPDelegateExecuteOptions())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if executed.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want one semantic MCP subject", executed.ActionCount)
	}

	configPath := filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read projected MCP config: %v", err)
	}
	canonical := canonicalApplyMCPEntry(
		t,
		"context7",
		"npx",
		[]string{"-y", "@upstash/context7-mcp"},
	)
	comparison, err := compareApplyMCPPlacementCanonicalEntry(
		t,
		aggregate.MCPPlacementClaudeProject,
		content,
		"context7",
		canonical,
	)
	if err != nil {
		t.Fatalf("compare projected MCP entry: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("projected MCP comparison = %#v, want present and equivalent", comparison)
	}

	paths := applyTestPaths(t, root)
	applied := loadApplyStatefile(t, paths.StatefilePath)
	aggregates := applied.ManagedAggregates()
	if len(aggregates) != 1 {
		t.Fatalf("state aggregates = %#v, want one managed aggregate contribution", aggregates)
	}
	subject := aggregates[0].Subject()
	serverID, isMCP := topologymcp.ServerID(subject)
	if !isMCP || serverID != "context7" {
		t.Fatalf("state subject = %q, want MCP server context7", subject)
	}
}

func TestManagedAggregateMCPPipelineBlocksAlternateConfigThroughCommand(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	writeApplyFile(t, manifestPath, `version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)
	if _, err := workflowlock.RunLock(
		context.Background(),
		workflowlock.LockInput{ManifestPath: manifestPath},
	); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	alternatePath := filepath.Join(root, aggregate.OpenCodeProjectMCPConfigPath+"c")
	writeApplyFile(t, alternatePath, `{"mcp":{"context7":{"type":"local","command":["node"]}}}`)

	_, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetOpenCode)},
	})
	if err == nil || !strings.Contains(err.Error(), "document_absent") {
		t.Fatalf("PlanWrite error = %v, want generic alternate-document precondition rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, aggregate.OpenCodeProjectMCPConfigPath)); !os.IsNotExist(statErr) {
		t.Fatalf("primary config stat error = %v, want no mutation", statErr)
	}
}

func TestManagedAggregateMCPPipelineCommitsMixedBatchAndThenNoOps(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	configPath := filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)
	writeApplyFile(t, configPath, `{
  "mcpServers": {
    "unmanaged": {
      "type": "stdio",
      "command": "keep-me"
    }
  },
  "unknownTopLevel": {
    "keep": true
  }
}
`)
	writeApplyFile(t, manifestPath, `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "alpha"
transport = "stdio"
command = "alpha-v1"

[[mcp_server]]
name = "beta"
transport = "stdio"
command = "beta-v1"
`)
	lockApplyManifest(t, manifestPath)
	created := planAndExecuteManagedAggregateMCP(t, manifestPath, target.TargetClaudeCode)
	if created.ActionCount != 2 {
		t.Fatalf("create ActionCount = %d, want two semantic subjects", created.ActionCount)
	}
	assertMCPEntryCommand(t, configPath, aggregate.MCPPlacementClaudeProject, "alpha", "alpha-v1")
	assertMCPEntryCommand(t, configPath, aggregate.MCPPlacementClaudeProject, "beta", "beta-v1")
	assertMCPEntryCommand(t, configPath, aggregate.MCPPlacementClaudeProject, "unmanaged", "keep-me")

	writeApplyFile(t, manifestPath, `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "alpha"
transport = "stdio"
command = "alpha-v2"

[[mcp_server]]
name = "gamma"
transport = "stdio"
command = "gamma-v1"
`)
	lockApplyManifest(t, manifestPath)
	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("PlanWrite mixed batch returned error: %v", err)
	}
	if len(planned.Reconciliation.Aggregates()) != 1 {
		t.Fatalf("mixed aggregate decisions = %d, want one document", len(planned.Reconciliation.Aggregates()))
	}
	if len(planned.Reconciliation.Aggregates()[0].Projections()) != 3 {
		t.Fatalf(
			"mixed projection decisions = %d, want update/remove/create",
			len(planned.Reconciliation.Aggregates()[0].Projections()),
		)
	}
	executed, err := ExecuteWithOptions(context.Background(), planned, successfulMCPDelegateExecuteOptions())
	if err != nil {
		t.Fatalf("Execute mixed batch returned error: %v", err)
	}
	if executed.ActionCount != 3 {
		t.Fatalf("mixed ActionCount = %d, want three semantic subjects", executed.ActionCount)
	}
	assertMCPEntryCommand(t, configPath, aggregate.MCPPlacementClaudeProject, "alpha", "alpha-v2")
	assertMCPEntryAbsent(t, configPath, aggregate.MCPPlacementClaudeProject, "beta")
	assertMCPEntryCommand(t, configPath, aggregate.MCPPlacementClaudeProject, "gamma", "gamma-v1")
	assertMCPEntryCommand(t, configPath, aggregate.MCPPlacementClaudeProject, "unmanaged", "keep-me")

	state := loadApplyStatefile(t, applyTestPaths(t, root).StatefilePath)
	aggregates := state.ManagedAggregates()
	if len(aggregates) != 2 {
		t.Fatalf("state aggregates = %d, want alpha and gamma", len(aggregates))
	}
	stateNames := make(map[string]struct{}, len(aggregates))
	for _, resource := range aggregates {
		subject := resource.Subject()
		serverID, isMCP := topologymcp.ServerID(subject)
		if !isMCP {
			t.Fatalf("state subject %q is not an MCP projection", subject)
		}
		stateNames[serverID] = struct{}{}
	}
	for _, name := range []string{"alpha", "gamma"} {
		if _, present := stateNames[name]; !present {
			t.Fatalf("state subjects = %#v, missing %q", stateNames, name)
		}
	}

	beforeNoOp, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config before no-op: %v", err)
	}
	noOp := planAndExecuteManagedAggregateMCP(t, manifestPath, target.TargetClaudeCode)
	if noOp.ActionCount != 0 {
		t.Fatalf("no-op ActionCount = %d, want no host or state mutation", noOp.ActionCount)
	}
	afterNoOp, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config after no-op: %v", err)
	}
	if !os.SameFile(beforeNoOp, afterNoOp) {
		t.Fatal("no-op aggregate apply replaced the physical config document")
	}
}

func lockApplyManifest(t *testing.T, manifestPath string) {
	t.Helper()
	if _, err := workflowlock.RunLock(
		context.Background(),
		workflowlock.LockInput{ManifestPath: manifestPath},
	); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
}

func planAndExecuteManagedAggregateMCP(
	t *testing.T,
	manifestPath string,
	selectedTarget target.Target,
) CommandResult {
	t.Helper()
	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(selectedTarget)},
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	if len(planned.Reconciliation.Aggregates()) != 1 {
		t.Fatalf("aggregate decisions = %d, want one document", len(planned.Reconciliation.Aggregates()))
	}
	executed, err := ExecuteWithOptions(context.Background(), planned, successfulMCPDelegateExecuteOptions())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	return executed
}

func successfulMCPDelegateExecuteOptions() ExecuteOptions {
	return ExecuteOptions{
		DelegateExecutor: delegate.NewExecutor(delegate.Options{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	}
}

func assertMCPEntryCommand(
	t *testing.T,
	configPath string,
	placementID aggregate.MCPPlacementID,
	serverID string,
	wantCommand string,
) {
	t.Helper()
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read MCP config: %v", err)
	}
	operations, present := mcpcodec.ImplementedMCPPlacementOperationsForID(placementID)
	if !present {
		t.Fatalf("placement operations %q are missing", placementID)
	}
	entry, present, err := operations.ExtractCanonicalEntry(content, serverID)
	if err != nil {
		t.Fatalf("extract MCP entry %q: %v", serverID, err)
	}
	if !present {
		t.Fatalf("MCP entry %q is absent", serverID)
	}
	if !strings.Contains(string(entry), wantCommand) {
		t.Fatalf("MCP entry %q = %s, want command %q", serverID, entry, wantCommand)
	}
}

func assertMCPEntryAbsent(
	t *testing.T,
	configPath string,
	placementID aggregate.MCPPlacementID,
	serverID string,
) {
	t.Helper()
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read MCP config: %v", err)
	}
	operations, present := mcpcodec.ImplementedMCPPlacementOperationsForID(placementID)
	if !present {
		t.Fatalf("placement operations %q are missing", placementID)
	}
	_, present, err = operations.ExtractCanonicalEntry(content, serverID)
	if err != nil {
		t.Fatalf("extract MCP entry %q: %v", serverID, err)
	}
	if present {
		t.Fatalf("MCP entry %q is still present", serverID)
	}
}
