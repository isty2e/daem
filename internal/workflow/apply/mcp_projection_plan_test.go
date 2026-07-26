package apply

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	outputpkg "github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	targetpkg "github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestPlanDryRunConsumesPublicMCPManifestLockSubject(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeApplyFile(t, manifestPath, `version = 1
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

	result, err := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(targetpkg.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("PlanDryRun returned error: %v", err)
	}
	if !result.ReconciliationReady {
		t.Fatal("ReconciliationReady = false, want true")
	}
	assertApplyMCPPlanSubject(t, result.Reconciliation, "context7")
	assertApplyMCPDelegateAction(t, result.Reconciliation.Delegates(), "context7")
}

func TestPlanWriteDelegatedRouteBlocksDelegateWhenProjectionLockIsStale(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeApplyFile(t, manifestPath, `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "must-not-run-daem-test"
args = ["--serve", "context7"]
`)
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	writeApplyFile(t, manifestPath, `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "must-not-run-daem-test"
args = ["--serve", "context7", "--changed"]
`)

	result, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{string(targetpkg.TargetClaudeCode)},
	})
	if err == nil || !strings.Contains(err.Error(), string(reconcile.ReasonStaleLock)) {
		t.Fatalf("PlanWrite error = %v, want stale lock error", err)
	}
	if !result.ReconciliationReady {
		t.Fatal("ReconciliationReady = false, want partial readiness result")
	}
	action := requireApplyMCPDelegateAction(t, result.Reconciliation.Delegates(), "context7")
	if action.Disposition() != reconcile.DelegateBlocked || action.SchedulesAttempt() {
		t.Fatalf("delegate action = %#v, want blocked stale projection precondition", action)
	}
	assertApplyMCPDelegateRisk(t, action, reconcile.DelegateRiskPreconditionBlocked)
}

func TestPlanDryRunSelectsStateOnlyMCPSubjectForRemoval(t *testing.T) {
	tempDir := t.TempDir()
	paths := applyTestPaths(t, tempDir)
	writeApplyFile(t, paths.ManifestPath, `version = 1
targets = ["claude-code"]
`)
	writeApplyLockfile(t, paths.LockfilePath, lock.File{Version: lock.CurrentVersion})
	canonical := canonicalApplyMCPEntry(t, "context7", "npx", nil)
	writeApplyFile(t, filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath), `{
  "mcpServers": {
    "context7": {
      "type": "stdio",
      "command": "npx",
      "args": [],
      "env": {}
    }
  }
}`)
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "claude-code.project.mcp-server", "context7")
	if err != nil {
		t.Fatalf("build MCP projection subject: %v", err)
	}
	placement, admitted := aggregate.MCPPlacementForSubject(subject)
	if !admitted {
		t.Fatalf("MCP projection subject %q has no admitted placement", subject)
	}
	contribution, err := placement.Contribution("context7", string(canonical))
	if err != nil {
		t.Fatalf("build MCP aggregate contribution: %v", err)
	}
	aggregateState, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatalf("build MCP aggregate state: %v", err)
	}
	writeApplyStatefile(t, paths.StatefilePath, applyStateSnapshot(t, durable.SnapshotInput{
		ManagedAggregates: []durable.ManagedAggregateState{aggregateState},
	}))

	result, err := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: paths.ManifestPath,
		TargetValues: []string{string(targetpkg.TargetClaudeCode)},
	})
	if err != nil {
		t.Fatalf("PlanDryRun returned error: %v", err)
	}
	decision := requireApplyMCPAggregateDecision(t, result.Reconciliation, "context7")
	if decision.Kind() != reconcile.AggregateRemove ||
		decision.Reason() != reconcile.ReasonRemovedFromManifest {
		t.Fatalf("decision = %#v, want state-only MCP subject removal", decision)
	}
}

func assertApplyMCPPlanSubject(t *testing.T, result reconcile.Result, serverID string) {
	t.Helper()
	decision := requireApplyMCPAggregateDecision(t, result, serverID)
	if decision.Target() != targetpkg.TargetClaudeCode ||
		decision.Destination() != outputpkg.Destination(aggregate.ClaudeProjectMCPConfigPath) ||
		decision.ContentPath() != outputpkg.ContentPath(mcpcodec.ClaudeProjectMCPContentPath(serverID)) {
		t.Fatalf("aggregate decision = %#v, want Claude project MCP projection subject %q", decision, serverID)
	}
}

func assertApplyMCPDelegateAction(t *testing.T, actions []reconcile.DelegateAction, serverID string) {
	t.Helper()
	action := requireApplyMCPDelegateAction(t, actions, serverID)
	actionServerID, _ := topologymcp.ServerID(action.Subject())
	if !subjectHasMCPPlacement(action.Subject(), aggregate.MCPPlacementClaudeProject) ||
		actionServerID != serverID ||
		action.Target() != targetpkg.TargetClaudeCode ||
		action.Disposition() != reconcile.DelegateSkipped ||
		action.PolicyOutcome() != reconcile.DelegatePolicySkip ||
		action.SchedulesAttempt() {
		t.Fatalf("delegate action = %#v, want skipped dry-run delegate action", action)
	}
	for _, risk := range action.Risks() {
		if risk.Code == reconcile.DelegateRiskDryRunDisclosure {
			return
		}
	}
	t.Fatalf("delegate action risks = %#v, want dry-run disclosure", action.Risks())
}

func requireApplyMCPDelegateAction(t *testing.T, actions []reconcile.DelegateAction, serverID string) reconcile.DelegateAction {
	t.Helper()
	if len(actions) != 1 {
		t.Fatalf("delegate actions = %#v, want one", actions)
	}
	action := actions[0]
	actionServerID, _ := topologymcp.ServerID(action.Subject())
	if !subjectHasMCPPlacement(action.Subject(), aggregate.MCPPlacementClaudeProject) ||
		actionServerID != serverID ||
		action.Target() != targetpkg.TargetClaudeCode {
		t.Fatalf("delegate action = %#v, want Claude project MCP delegate action for %q", action, serverID)
	}
	return action
}

func assertApplyMCPDelegateRisk(t *testing.T, action reconcile.DelegateAction, code reconcile.DelegateRiskCode) {
	t.Helper()
	for _, risk := range action.Risks() {
		if risk.Code == code {
			return
		}
	}
	t.Fatalf("delegate action risks = %#v, want %s", action.Risks(), code)
}
