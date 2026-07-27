package status

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRunConsumesPublicMCPManifestLockSubject(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusMCPPlanSubject(t, result.Reconciliation.Aggregates(), "context7")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "missing", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "unknown", string(mcpobserve.ReasonOwnershipStateUnobserved))
}

func TestRunConsumesPublicAntigravityMCPManifestLockSubject(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusAntigravityMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetAntigravityCLI)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusAntigravityMCPPlanSubject(t, result.Reconciliation.Aggregates(), "context7")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "global_projection", "missing", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "unknown", string(mcpobserve.ReasonOwnershipStateUnobserved))
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "not_observed", string(mcpobserve.ReasonLastDelegateAttemptUnobserved))
}

func TestRunConsumesPublicOpenCodeMCPManifestLockSubject(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusOpenCodeMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetOpenCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusOpenCodeMCPPlanSubject(t, result.Reconciliation.Aggregates(), "context7")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "missing", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "unknown", string(mcpobserve.ReasonOwnershipStateUnobserved))
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "not_observed", string(mcpobserve.ReasonLastDelegateAttemptUnobserved))
}

func TestRunConsumesPublicCodexMCPManifestLockSubject(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusCodexMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetCodex)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusCodexMCPPlanSubject(t, result.Reconciliation.Aggregates(), "context7")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "missing", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "unknown", string(mcpobserve.ReasonOwnershipStateUnobserved))
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "not_observed", string(mcpobserve.ReasonLastDelegateAttemptUnobserved))
}

func TestRunConsumesPublicCodexGlobalMCPManifestLockSubject(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusCodexGlobalMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetCodex)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusCodexGlobalMCPPlanSubject(t, result.Reconciliation.Aggregates(), "context7")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "global_projection", "missing", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "unknown", string(mcpobserve.ReasonOwnershipStateUnobserved))
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "not_observed", string(mcpobserve.ReasonLastDelegateAttemptUnobserved))
}

func TestRunConsumesPublicOpenCodeGlobalMCPManifestLockSubject(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusOpenCodeGlobalMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetOpenCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusOpenCodeGlobalMCPPlanSubject(t, result.Reconciliation.Aggregates(), "context7")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "global_projection", "missing", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "unknown", string(mcpobserve.ReasonOwnershipStateUnobserved))
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "not_observed", string(mcpobserve.ReasonLastDelegateAttemptUnobserved))
}

func TestRunReportsProjectedMCPStatusWhenStateOwnsEntry(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	record := lockedStatusMCPRecord(t, filepath.Join(tempDir, "daem.lock.toml"), "context7")
	delegatePlan, ok := record.DelegatePlan()
	if !ok {
		t.Fatal("locked MCP record missing delegate plan")
	}
	canonical := canonicalStatusMCPEntryWithArgs(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"})
	writeTestFile(t, tempDir, aggregate.ClaudeProjectMCPConfigPath, `{"mcpServers":{"context7":`+string(canonical)+`}}`)
	writeStatusStatefile(
		t,
		filepath.Join(tempDir, ".daem", "state.json"),
		statusMCPStateSnapshot(
			t,
			aggregate.MCPPlacementClaudeProject,
			"context7",
			canonical,
			statusLastDelegateAttempt(
				t,
				record.SubjectID(),
				target.TargetClaudeCode,
				target.ScopeProject,
				delegatePlan.IdentityKey(),
				durableattempt.DelegateStatusFailed,
				durableattempt.DelegateReasonNonZeroExit,
			),
		),
	)

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "projected", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "managed", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "failed", string(mcpobserve.ReasonDelegateNonZeroExit))
	if paths, aggregates := result.Reconciliation.PendingManagedPaths(), result.Reconciliation.PendingAggregates(); len(paths) != 0 || len(aggregates) != 0 {
		t.Fatalf("pending decisions = paths:%#v aggregates:%#v, want clean projected status", paths, aggregates)
	}
}

func TestRunReportsProjectedAntigravityMCPStatusWhenStateOwnsEntry(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusAntigravityMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	record := lockedStatusAntigravityMCPRecord(t, filepath.Join(tempDir, "daem.lock.toml"), "context7")
	if _, ok := record.DelegatePlan(); ok {
		t.Fatal("locked Antigravity MCP record unexpectedly has delegate plan")
	}
	canonical := canonicalStatusAntigravityMCPEntryWithArgs(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"})
	writeHomeRelativeTestFile(t, homeDir, ".gemini/config/mcp_config.json", `{"mcpServers":{"context7":`+string(canonical)+`}}`)
	writeStatusStatefile(
		t,
		filepath.Join(tempDir, ".daem", "state.json"),
		statusMCPStateSnapshot(t, aggregate.MCPPlacementAntigravityGlobal, "context7", canonical),
	)
	writeStatusOwnershipClaim(t, manifestPath, outputtest.Parse(t, aggregate.AntigravityGlobalMCPConfigPath), output.ContentPath(mcpcodec.AntigravityGlobalMCPContentPath("context7")))

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetAntigravityCLI)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "global_projection", "projected", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "managed", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "not_observed", string(mcpobserve.ReasonLastDelegateAttemptUnobserved))
	if paths, aggregates := result.Reconciliation.PendingManagedPaths(), result.Reconciliation.PendingAggregates(); len(paths) != 0 || len(aggregates) != 0 {
		t.Fatalf("pending decisions = paths:%#v aggregates:%#v, want clean projected status", paths, aggregates)
	}
}

func TestRunReportsProjectedOpenCodeMCPStatusWhenStateOwnsEntry(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusOpenCodeMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	canonical := canonicalStatusOpenCodeMCPEntryWithArgs(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"})
	writeTestFile(t, tempDir, aggregate.OpenCodeProjectMCPConfigPath, `{"mcp":{"context7":`+string(canonical)+`}}`)
	writeStatusStatefile(
		t,
		filepath.Join(tempDir, ".daem", "state.json"),
		statusMCPStateSnapshot(t, aggregate.MCPPlacementOpenCodeProject, "context7", canonical),
	)

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetOpenCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "projected", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "managed", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "not_observed", string(mcpobserve.ReasonLastDelegateAttemptUnobserved))
	if paths, aggregates := result.Reconciliation.PendingManagedPaths(), result.Reconciliation.PendingAggregates(); len(paths) != 0 || len(aggregates) != 0 {
		t.Fatalf("pending decisions = paths:%#v aggregates:%#v, want clean projected status", paths, aggregates)
	}
}

func TestRunReportsProjectedCodexMCPStatusWhenStateOwnsEntry(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusCodexMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	canonical := canonicalStatusCodexMCPEntryWithArgs(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"})
	writeTestFile(t, tempDir, aggregate.CodexProjectMCPConfigPath, `[mcp_servers.context7]
`+string(canonical))
	writeStatusStatefile(
		t,
		filepath.Join(tempDir, ".daem", "state.json"),
		statusMCPStateSnapshot(t, aggregate.MCPPlacementCodexProject, "context7", canonical),
	)

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetCodex)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "projected", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "managed", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "not_observed", string(mcpobserve.ReasonLastDelegateAttemptUnobserved))
	if paths, aggregates := result.Reconciliation.PendingManagedPaths(), result.Reconciliation.PendingAggregates(); len(paths) != 0 || len(aggregates) != 0 {
		t.Fatalf("pending decisions = paths:%#v aggregates:%#v, want clean projected status", paths, aggregates)
	}
}

func TestRunReportsProjectedCodexGlobalMCPStatusWhenStateOwnsEntry(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusCodexGlobalMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	canonical := canonicalStatusCodexGlobalMCPEntryWithArgs(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"})
	writeHomeRelativeTestFile(t, homeDir, ".codex/config.toml", `[mcp_servers.context7]
`+string(canonical))
	writeStatusStatefile(
		t,
		filepath.Join(tempDir, ".daem", "state.json"),
		statusMCPStateSnapshot(t, aggregate.MCPPlacementCodexGlobal, "context7", canonical),
	)
	writeStatusOwnershipClaim(t, manifestPath, outputtest.Parse(t, aggregate.CodexGlobalMCPConfigPath), output.ContentPath(mcpcodec.CodexGlobalMCPContentPath("context7")))

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetCodex)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "global_projection", "projected", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "managed", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "not_observed", string(mcpobserve.ReasonLastDelegateAttemptUnobserved))
	if paths, aggregates := result.Reconciliation.PendingManagedPaths(), result.Reconciliation.PendingAggregates(); len(paths) != 0 || len(aggregates) != 0 {
		t.Fatalf("pending decisions = paths:%#v aggregates:%#v, want clean projected status", paths, aggregates)
	}
}

func TestRunReportsProjectedOpenCodeGlobalMCPStatusWhenStateOwnsEntry(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusOpenCodeGlobalMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	canonical := canonicalStatusOpenCodeGlobalMCPEntryWithArgs(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"})
	writeHomeRelativeTestFile(t, homeDir, ".config/opencode/opencode.json", `{"mcp":{"context7":`+string(canonical)+`}}`)
	writeStatusStatefile(
		t,
		filepath.Join(tempDir, ".daem", "state.json"),
		statusMCPStateSnapshot(t, aggregate.MCPPlacementOpenCodeGlobal, "context7", canonical),
	)
	writeStatusOwnershipClaim(t, manifestPath, outputtest.Parse(t, aggregate.OpenCodeGlobalMCPConfigPath), output.ContentPath(mcpcodec.OpenCodeGlobalMCPContentPath("context7")))

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetOpenCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "global_projection", "projected", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "managed", "")
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "not_observed", string(mcpobserve.ReasonLastDelegateAttemptUnobserved))
	if paths, aggregates := result.Reconciliation.PendingManagedPaths(), result.Reconciliation.PendingAggregates(); len(paths) != 0 || len(aggregates) != 0 {
		t.Fatalf("pending decisions = paths:%#v aggregates:%#v, want clean projected status", paths, aggregates)
	}
}

func TestRunDoesNotTreatClaudeMCPStateAsOpenCodeOwnershipForSameName(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusOpenCodeMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	openCodeCanonical := canonicalStatusOpenCodeMCPEntryWithArgs(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"})
	writeTestFile(t, tempDir, aggregate.OpenCodeProjectMCPConfigPath, `{"mcp":{"context7":`+string(openCodeCanonical)+`}}`)
	claudeCanonical := canonicalStatusMCPEntryWithArgs(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"})
	writeStatusStatefile(
		t,
		filepath.Join(tempDir, ".daem", "state.json"),
		statusMCPStateSnapshot(t, aggregate.MCPPlacementClaudeProject, "context7", claudeCanonical),
	)

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetOpenCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "unmanaged_same_name", string(mcpobserve.ReasonRoutePreexistingUnowned))
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "unmanaged_same_name", string(mcpobserve.ReasonRoutePreexistingUnowned))
	assertStatusMCPAggregatePlanSubject(
		t,
		result.Reconciliation.Aggregates(),
		aggregate.MCPPlacementOpenCodeProject,
		"context7",
		reconcile.AggregateBlocked,
		reconcile.ReasonUnmanagedOutputExists,
	)
}

func TestRunSelectsStateOnlyMCPSubjectForRemoval(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["claude-code"]
`)
	writeStatusLockfile(t, filepath.Join(tempDir, "daem.lock.toml"), lock.File{Version: lock.CurrentVersion})
	canonical := canonicalStatusMCPEntry(t, "context7", "npx")
	writeTestFile(t, tempDir, aggregate.ClaudeProjectMCPConfigPath, `{
  "mcpServers": {
    "context7": {
      "type": "stdio",
      "command": "npx",
      "args": [],
      "env": {}
    }
  }
}`)
	writeStatusStatefile(
		t,
		filepath.Join(tempDir, ".daem", "state.json"),
		statusMCPStateSnapshot(t, aggregate.MCPPlacementClaudeProject, "context7", canonical),
	)

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusMCPAggregatePlanSubject(
		t,
		result.Reconciliation.Aggregates(),
		aggregate.MCPPlacementClaudeProject,
		"context7",
		reconcile.AggregateRemove,
		reconcile.ReasonRemovedFromManifest,
	)
}

func assertStatusMCPPlanSubject(t *testing.T, decisions []reconcile.AggregateDecision, serverID string) {
	t.Helper()
	assertStatusMCPAggregatePlanSubject(
		t, decisions, aggregate.MCPPlacementClaudeProject, serverID,
		reconcile.AggregateCreate, reconcile.ReasonMissingOutput,
	)
}

func assertStatusAntigravityMCPPlanSubject(t *testing.T, decisions []reconcile.AggregateDecision, serverID string) {
	t.Helper()
	assertStatusMCPAggregatePlanSubject(
		t, decisions, aggregate.MCPPlacementAntigravityGlobal, serverID,
		reconcile.AggregateCreate, reconcile.ReasonMissingOutput,
	)
}

func assertStatusOpenCodeMCPPlanSubject(t *testing.T, decisions []reconcile.AggregateDecision, serverID string) {
	t.Helper()
	assertStatusMCPAggregatePlanSubject(
		t, decisions, aggregate.MCPPlacementOpenCodeProject, serverID,
		reconcile.AggregateCreate, reconcile.ReasonMissingOutput,
	)
}

func assertStatusCodexMCPPlanSubject(t *testing.T, decisions []reconcile.AggregateDecision, serverID string) {
	t.Helper()
	assertStatusMCPAggregatePlanSubject(
		t, decisions, aggregate.MCPPlacementCodexProject, serverID,
		reconcile.AggregateCreate, reconcile.ReasonMissingOutput,
	)
}

func assertStatusCodexGlobalMCPPlanSubject(t *testing.T, decisions []reconcile.AggregateDecision, serverID string) {
	t.Helper()
	assertStatusMCPAggregatePlanSubject(
		t, decisions, aggregate.MCPPlacementCodexGlobal, serverID,
		reconcile.AggregateCreate, reconcile.ReasonMissingOutput,
	)
}

func assertStatusOpenCodeGlobalMCPPlanSubject(t *testing.T, decisions []reconcile.AggregateDecision, serverID string) {
	t.Helper()
	assertStatusMCPAggregatePlanSubject(
		t, decisions, aggregate.MCPPlacementOpenCodeGlobal, serverID,
		reconcile.AggregateCreate, reconcile.ReasonMissingOutput,
	)
}

func assertStatusMCPAggregatePlanSubject(
	t *testing.T,
	decisions []reconcile.AggregateDecision,
	placementID aggregate.MCPPlacementID,
	serverID string,
	wantKind reconcile.AggregateDecisionKind,
	wantReason reconcile.ActionReason,
) {
	t.Helper()
	for _, decision := range decisions {
		for _, projection := range decision.Projections() {
			for _, subject := range projection.Subjects() {
				name, recognized := topologymcp.ServerID(subject)
				placement, admitted := aggregate.MCPPlacementForSubject(subject)
				if !recognized || !admitted || name != serverID || placement.ID() != placementID {
					continue
				}
				contentPath, err := placement.ContentPath(serverID)
				if err != nil {
					t.Fatalf("ContentPath returned error: %v", err)
				}
				contract := projection.Contract()
				if contract.Address().Document().Target() != placement.Target() ||
					contract.Address().Document().Scope() != placement.Scope() ||
					contract.Address().Document().AggregateRoot() != placement.ConfigPath() ||
					contract.Address().ContentPath() != contentPath ||
					projection.Kind() != wantKind ||
					projection.Reason() != wantReason {
					t.Fatalf(
						"aggregate projection = kind %q reason %q contract %#v, want kind %q reason %q placement %q",
						projection.Kind(),
						projection.Reason(),
						contract,
						wantKind,
						wantReason,
						placementID,
					)
				}
				return
			}
		}
	}
	t.Fatalf(
		"aggregate decisions = %#v, want MCP projection %q for placement %q",
		decisions,
		serverID,
		placementID,
	)
}

func lockedStatusAntigravityMCPRecord(t *testing.T, lockfilePath string, serverID string) lock.LockedSubjectContract {
	t.Helper()
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("Load lockfile returned error: %v", err)
	}
	for _, record := range locked.Locked.Subjects() {
		name, _ := topologymcp.ServerID(record.SubjectID())
		if subjectHasMCPPlacement(record.SubjectID(), aggregate.MCPPlacementAntigravityGlobal) && name == serverID {
			return record
		}
	}
	t.Fatalf("locked subjects = %#v, want Antigravity global MCP record %q", locked.Locked.Subjects(), serverID)
	return lock.LockedSubjectContract{}
}

func writeStatusAntigravityMCPManifest(t *testing.T, manifestPath string) {
	t.Helper()
	writeTestFile(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), `version = 1
targets = ["antigravity-cli"]

[[mcp_server]]
name = "context7"
targets = ["antigravity-cli"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)
}

func writeStatusOpenCodeGlobalMCPManifest(t *testing.T, manifestPath string) {
	t.Helper()
	writeTestFile(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), `version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)
}

func writeStatusCodexMCPManifest(t *testing.T, manifestPath string) {
	t.Helper()
	writeTestFile(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), `version = 1
targets = ["codex"]

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)
}

func writeStatusCodexGlobalMCPManifest(t *testing.T, manifestPath string) {
	t.Helper()
	writeTestFile(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), `version = 1
targets = ["codex"]

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)
}

func writeHomeRelativeTestFile(t *testing.T, homeDir string, relativePath string, content string) {
	t.Helper()
	path := filepath.Join(homeDir, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create home-relative test file dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write home-relative test file: %v", err)
	}
}

func writeStatusLockfile(t *testing.T, path string, file lock.File) {
	t.Helper()
	content, err := lockfile.Marshal(file)
	if err != nil {
		t.Fatalf("marshal lockfile: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}
}

func writeStatusOwnershipClaim(t *testing.T, manifestPath string, destination output.Destination, contentPath output.ContentPath) {
	t.Helper()
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("resolve ownership paths: %v", err)
	}
	physical, err := destinationResolver(paths).Resolve(destination)
	if err != nil {
		t.Fatalf("resolve ownership destination: %v", err)
	}
	canonicalPhysical, err := mutation.CanonicalDirectoryEntryKey(physical)
	if err != nil {
		t.Fatalf("canonicalize ownership destination: %v", err)
	}
	address, err := ownership.NewManagedAddress(canonicalPhysical, string(contentPath))
	if err != nil {
		t.Fatalf("NewManagedAddress: %v", err)
	}
	statefileKey, err := mutation.CanonicalDirectoryEntryKey(paths.StatefilePath)
	if err != nil {
		t.Fatalf("canonicalize statefile authority: %v", err)
	}
	owner, err := ownership.NewOwnerAuthority(statefileKey, paths.ManifestPath)
	if err != nil {
		t.Fatalf("NewOwnerAuthority: %v", err)
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatalf("NewActiveClaim: %v", err)
	}
	claimValue, _ := ownership.PresentClaim(claim)
	registryStore, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatalf("open ownership registry: %v", err)
	}
	if _, err := registryStore.Apply(context.Background(), address, ownership.NoClaim(), claimValue); err != nil {
		t.Fatalf("write ownership claim: %v", err)
	}
}

func canonicalStatusMCPEntry(t *testing.T, serverID string, command string) []byte {
	return canonicalStatusMCPEntryWithArgs(t, serverID, command, nil)
}

func canonicalStatusAntigravityMCPEntryWithArgs(t *testing.T, serverID string, command string, args []string) []byte {
	t.Helper()
	canonical, err := mcpcodec.CanonicalAntigravityGlobalMCPServerEntry(mcpcodec.AntigravityGlobalMCPServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            append([]string(nil), args...),
		AdapterContract: aggregate.AntigravityGlobalMCPCommandAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalAntigravityGlobalMCPServerEntry returned error: %v", err)
	}
	return canonical
}

func canonicalStatusOpenCodeMCPEntryWithArgs(t *testing.T, serverID string, command string, args []string) []byte {
	t.Helper()
	canonical, err := mcpcodec.CanonicalOpenCodeProjectMCPServerEntry(mcpcodec.OpenCodeProjectMCPServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            append([]string(nil), args...),
		AdapterContract: aggregate.OpenCodeProjectMCPLocalCommandV1,
	})
	if err != nil {
		t.Fatalf("CanonicalOpenCodeProjectMCPServerEntry returned error: %v", err)
	}
	return canonical
}

func canonicalStatusCodexMCPEntryWithArgs(t *testing.T, serverID string, command string, args []string) []byte {
	t.Helper()
	canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.CodexProjectMCPServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            append([]string(nil), args...),
		AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
	})
	if err != nil {
		t.Fatalf("CanonicalCodexProjectMCPServerEntry returned error: %v", err)
	}
	return canonical
}

func canonicalStatusCodexGlobalMCPEntryWithArgs(t *testing.T, serverID string, command string, args []string) []byte {
	t.Helper()
	canonical, err := mcpcodec.CanonicalCodexGlobalMCPServerEntry(mcpcodec.CodexGlobalMCPServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            append([]string(nil), args...),
		AdapterContract: aggregate.CodexGlobalMCPStdioCommandV1,
	})
	if err != nil {
		t.Fatalf("CanonicalCodexGlobalMCPServerEntry returned error: %v", err)
	}
	return canonical
}

func canonicalStatusOpenCodeGlobalMCPEntryWithArgs(t *testing.T, serverID string, command string, args []string) []byte {
	t.Helper()
	canonical, err := mcpcodec.CanonicalOpenCodeGlobalMCPServerEntry(mcpcodec.OpenCodeGlobalMCPServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            append([]string(nil), args...),
		AdapterContract: aggregate.OpenCodeGlobalMCPLocalCommandV1,
	})
	if err != nil {
		t.Fatalf("CanonicalOpenCodeGlobalMCPServerEntry returned error: %v", err)
	}
	return canonical
}
