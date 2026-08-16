package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestMCPPublicCLIStatusReportsDelegateFailureWithoutCheckFailure(t *testing.T) {
	t.Setenv("CONTEXT7_API_TOKEN", "test-token")
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "must-not-run-daem-test",
		Args:    []string{"--serve", "context7"},
		Env:     map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	})
	writeMCPConfigWithSibling(t, project.root, "")
	runMCPLock(t, project)

	runMCPCLIWithSuccessfulDelegate(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes")

	record := loadMCPDelegateStatusRecord(t, project.lockfilePath, "context7")
	delegatePlan, ok := record.DelegatePlan()
	if !ok {
		t.Fatal("locked MCP record missing delegate plan")
	}
	state := loadMCPStatefile(t, project.root)
	state = snapshotWithMCPDelegateStatusAttempt(
		t,
		state,
		record.SubjectID(),
		delegatePlan.IdentityKey(),
		durableattempt.DelegateStatusFailed,
		durableattempt.DelegateReasonNonZeroExit,
	)
	testkit.WriteStatefile(t, filepath.Join(project.root, ".daem", "state.json"), state)

	exitCode, stdout, stderr := runMCPCLI(t, "status", "--manifest", project.manifestPath, "--target", "claude-code", "--check")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("human status check exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	for _, want := range []string{
		"project_projection: projected",
		"delegate_last_attempt: failed reason=DELEGATE_NONZERO_EXIT",
		"status: 1 actions",
		`noop subject="projection/claude-code.project.mcp-server/context7"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("human stdout = %q, want %q", stdout, want)
		}
	}
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--target", "claude-code", "--check", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("json status check exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	payload := clijson.DecodePlan(t, []byte(stdout))
	if payload.HasErrors || payload.ActionCount != 1 || payload.Actions[0].Kind != "noop" {
		t.Fatalf("status payload = %#v, want clean noop projection", payload)
	}
	assertMCPJSONDimension(t, payload, "project_projection", "projected", "")
	assertMCPJSONDimension(t, payload, "delegate_last_attempt", "failed", "DELEGATE_NONZERO_EXIT")
	assertMCPJSONDimensionInGroup(t, payload, "projection", "project_projection", "projected", "")
	assertMCPJSONDimensionInGroup(t, payload, "delegate", "delegate_last_attempt", "failed", "DELEGATE_NONZERO_EXIT")
	assertNoPublicMCPOutputLeaks(t, stdout)

	state = snapshotWithMCPDelegateStatusAttempt(
		t,
		state,
		record.SubjectID(),
		delegatePlan.IdentityKey(),
		durableattempt.DelegateStatusSucceeded,
		durableattempt.DelegateReasonNone,
	)
	testkit.WriteStatefile(t, filepath.Join(project.root, ".daem", "state.json"), state)

	exitCode, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--target", "claude-code", "--check", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("json status check with succeeded attempt exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	payload = clijson.DecodePlan(t, []byte(stdout))
	if payload.HasErrors || payload.ActionCount != 1 || payload.Actions[0].Kind != "noop" {
		t.Fatalf("status payload = %#v, want clean noop projection with succeeded last attempt", payload)
	}
	assertMCPJSONDimension(t, payload, "project_projection", "projected", "")
	assertMCPJSONDimension(t, payload, "delegate_last_attempt", "succeeded", "")
	assertNoPublicMCPOutputLeaks(t, stdout)
}

func TestMCPPublicCLIStatusReportsStaleLastDelegateAttemptAfterLockDrift(t *testing.T) {
	t.Setenv("CONTEXT7_API_TOKEN", "test-token")
	project := newMCPCLIProject(t)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
		Env:     map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	})
	writeMCPConfigWithSibling(t, project.root, "")
	runMCPLock(t, project)
	runMCPCLIWithSuccessfulDelegate(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")

	oldRecord := loadMCPDelegateStatusRecord(t, project.lockfilePath, "context7")
	oldDelegatePlan, ok := oldRecord.DelegatePlan()
	if !ok {
		t.Fatal("old locked MCP record missing delegate plan")
	}
	state := loadMCPStatefile(t, project.root)
	state = snapshotWithMCPDelegateStatusAttempt(
		t,
		state,
		oldRecord.SubjectID(),
		oldDelegatePlan.IdentityKey(),
		durableattempt.DelegateStatusSucceeded,
		durableattempt.DelegateReasonNone,
	)
	testkit.WriteStatefile(t, filepath.Join(project.root, ".daem", "state.json"), state)

	writeMCPManifest(t, project.root, mcpManifestSpec{
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp@1.2.4"},
		Env:     map[string]string{"API_TOKEN": "CONTEXT7_API_TOKEN"},
	})
	runMCPLock(t, project)
	runMCPCLIWithSuccessfulDelegate(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	testkit.WriteStatefile(t, filepath.Join(project.root, ".daem", "state.json"), state)

	exitCode, stdout, stderr := runMCPCLI(t, "status", "--manifest", project.manifestPath, "--target", "claude-code", "--check", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("status after lock drift exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	payload := clijson.DecodePlan(t, []byte(stdout))
	if payload.HasErrors || payload.ActionCount != 1 || payload.Actions[0].Kind != "record" || payload.Actions[0].Reason != "state_stale" {
		t.Fatalf("status payload = %#v, want state refresh plus stale delegate attempt record", payload)
	}
	assertMCPJSONDimension(t, payload, "project_projection", "projected", "")
	assertMCPJSONDimension(t, payload, "delegate_last_attempt", "stale", "LAST_DELEGATE_ATTEMPT_STALE")
	assertMCPJSONDimensionInGroup(t, payload, "projection", "project_projection", "projected", "")
	assertMCPJSONDimensionInGroup(t, payload, "delegate", "delegate_last_attempt", "stale", "LAST_DELEGATE_ATTEMPT_STALE")
	assertNoPublicMCPOutputLeaks(t, stdout)
}

func loadMCPDelegateStatusRecord(t *testing.T, lockfilePath string, serverID string) lock.LockedSubjectContract {
	t.Helper()
	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("Load lockfile returned error: %v", err)
	}
	for _, record := range locked.Locked.Subjects() {
		if cliSubjectHasMCPPlacement(record.SubjectID(), aggregate.MCPPlacementClaudeProject) && record.SubjectID().Key() == serverID {
			return record
		}
	}
	t.Fatalf("locked subjects = %#v, want Claude project MCP record %q", locked.Locked.Subjects(), serverID)
	return lock.LockedSubjectContract{}
}

func snapshotWithMCPDelegateStatusAttempt(
	t *testing.T,
	snapshot durable.Snapshot,
	subject topology.SubjectID,
	planIdentityKey string,
	status durableattempt.DelegateAttemptStatus,
	reason durableattempt.DelegateAttemptReason,
) durable.Snapshot {
	t.Helper()
	var exitCode *int
	if reason == durableattempt.DelegateReasonNonZeroExit {
		value := 17
		exitCode = &value
	}
	attemptObserved := status != durableattempt.DelegateStatusBlocked
	processReason := durableattempt.DelegateProcessReasonNone
	if reason != durableattempt.DelegateReasonNone &&
		reason != durableattempt.DelegateReasonPolicyBlocked &&
		reason != durableattempt.DelegateReasonWorkDirAuthority {
		processReason = durableattempt.DelegateProcessReason(reason)
	}
	attempt, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
		Subject:         subject,
		Target:          target.TargetClaudeCode,
		Scope:           target.ScopeProject,
		PlanIdentityKey: planIdentityKey,
		ObservedAt:      time.Date(2026, time.June, 30, 10, 0, 0, 0, time.UTC),
		Status:          status,
		Reason:          reason,
		AttemptObserved: attemptObserved,
		ProcessReason:   processReason,
		ExitCode:        exitCode,
		TimedOut:        reason == durableattempt.DelegateReasonTimeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := snapshot.WithDelegateAttempts([]durableattempt.DelegateAttempt{attempt})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func cliSubjectHasMCPPlacement(subject topology.SubjectID, placementID aggregate.MCPPlacementID) bool {
	placement, ok := aggregate.MCPPlacementForSubject(subject)
	return ok && placement.ID() == placementID
}
