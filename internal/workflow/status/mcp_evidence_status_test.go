package status

import (
	"context"
	"path/filepath"
	"testing"

	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestRunReportsMalformedMCPStatusWithoutWorkflowFailure(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	writeTestFile(t, tempDir, aggregate.ClaudeProjectMCPConfigPath, `{"mcpServers":`)

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error for malformed status: %v", err)
	}
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "malformed", string(mcpobserve.ReasonConfigMalformed))
	if pending := result.Reconciliation.PendingAggregates(); len(pending) != 1 || !result.Reconciliation.HasErrors() {
		t.Fatalf("pending aggregates = %#v, errors = %t, want malformed projection block", pending, result.Reconciliation.HasErrors())
	}
}

func TestRunReportsUnmanagedSameNameMCPStatus(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	canonical := canonicalStatusMCPEntryWithArgs(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"})
	writeTestFile(t, tempDir, aggregate.ClaudeProjectMCPConfigPath, `{"mcpServers":{"context7":`+string(canonical)+`}}`)

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "unmanaged_same_name", string(mcpobserve.ReasonRoutePreexistingUnowned))
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "same_scope_ownership", "unmanaged_same_name", string(mcpobserve.ReasonRoutePreexistingUnowned))
}

func TestRunReportsUnsupportedMCPStatusWithoutWorkflowFailure(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	writeTestFile(t, tempDir, aggregate.ClaudeProjectMCPConfigPath, `{"mcpServers":{"context7":{"type":"http","command":"npx"}}}`)

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error for unsupported status: %v", err)
	}
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "unsupported", string(mcpobserve.ReasonUnsupportedTransport))
	if pending := result.Reconciliation.PendingAggregates(); len(pending) != 1 || !result.Reconciliation.HasErrors() {
		t.Fatalf("pending aggregates = %#v, errors = %t, want unsupported projection block", pending, result.Reconciliation.HasErrors())
	}
}

func TestRunReportsUnsupportedOpenCodeJSONCAlternateConfigWithoutWorkflowFailure(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeStatusOpenCodeMCPManifest(t, manifestPath)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	writeTestFile(t, tempDir, aggregate.OpenCodeProjectMCPConfigPath+"c", `{"mcp":{"context7":{"type":"local","command":["npx"]}}}`)

	result, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(target.TargetOpenCode)},
	})
	if err != nil {
		t.Fatalf("Run returned error for alternate config status: %v", err)
	}
	assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "unsupported", string(mcpobserve.ReasonUnsupportedAlternateConfig))
	if pending := result.Reconciliation.PendingAggregates(); len(pending) != 1 || !result.Reconciliation.HasErrors() {
		t.Fatalf("pending aggregates = %#v, errors = %t, want alternate-config projection block", pending, result.Reconciliation.HasErrors())
	}
}
