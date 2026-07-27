package status

import (
	"context"
	"path/filepath"
	"testing"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestRunReportsStaleDelegateAttemptRecord(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	record := lockedStatusMCPRecord(t, filepath.Join(tempDir, "daem.lock.toml"), "context7")
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
				"delegate:old-plan",
				durableattempt.DelegateStatusSucceeded,
				durableattempt.DelegateReasonNone,
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
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "stale", string(mcpobserve.ReasonLastDelegateAttemptStale))
}

func TestRunIgnoresLastDelegateAttemptForDifferentTargetScope(t *testing.T) {
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
				target.ScopeGlobal,
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
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "not_observed", string(mcpobserve.ReasonLastDelegateAttemptUnobserved))
}

func TestRunSelectsMatchingLastDelegateAttemptAfterUnrelatedRows(t *testing.T) {
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
				target.ScopeGlobal,
				delegatePlan.IdentityKey(),
				durableattempt.DelegateStatusFailed,
				durableattempt.DelegateReasonNonZeroExit,
			),
			statusLastDelegateAttempt(
				t,
				record.SubjectID(),
				target.TargetClaudeCode,
				target.ScopeProject,
				delegatePlan.IdentityKey(),
				durableattempt.DelegateStatusSucceeded,
				durableattempt.DelegateReasonNone,
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
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "succeeded", "")
}
