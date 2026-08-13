package status

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	mcptest "github.com/isty2e/daem/test/testkit/mcp"
)

func assertStatusMCPDimension(
	t *testing.T,
	observations []mcpobserve.LockedProjectionObservation,
	serverID string,
	dimension string,
	state string,
	reason string,
) {
	t.Helper()
	for _, observation := range observations {
		name, _ := topologymcp.ServerID(observation.Subject())
		if name != serverID {
			continue
		}
		gotState, gotReason, found := canonicalMCPDimension(observation, dimension)
		if !found {
			t.Fatalf("canonical MCP observation has no dimension %q", dimension)
		}
		if gotState != state || gotReason != reason {
			t.Fatalf("%s dimension = state=%q reason=%q, want state=%q reason=%q", dimension, gotState, gotReason, state, reason)
		}
		return
	}
	t.Fatalf("observations = %#v, want %s for %s", observations, dimension, serverID)
}

func canonicalMCPDimension(
	observation mcpobserve.LockedProjectionObservation,
	dimension string,
) (string, string, bool) {
	current := observation.Current()
	switch dimension {
	case "project_projection":
		if observation.Scope() != target.ScopeProject {
			return "", "", false
		}
		return string(current.Projection.State), string(current.Projection.Reason), true
	case "global_projection":
		if observation.Scope() != target.ScopeGlobal {
			return "", "", false
		}
		return string(current.Projection.State), string(current.Projection.Reason), true
	case "same_scope_ownership":
		return string(current.Ownership.State), string(current.Ownership.Reason), true
	case "effective_shadowing":
		return string(current.Shadowing.State), string(current.Shadowing.Reason), true
	case "delegate_last_attempt":
		attempt := observation.LastDelegateAttempt()
		return string(attempt.State), string(attempt.Reason), true
	default:
		return "", "", false
	}
}

func lockedStatusMCPRecord(t *testing.T, lockfilePath string, serverID string) lock.LockedSubjectContract {
	t.Helper()
	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("Load lockfile returned error: %v", err)
	}
	for _, record := range locked.Locked.Subjects() {
		name, _ := topologymcp.ServerID(record.SubjectID())
		if subjectHasMCPPlacement(record.SubjectID(), aggregate.MCPPlacementClaudeProject) && name == serverID {
			return record
		}
	}
	t.Fatalf("locked subjects = %#v, want Claude project MCP record %q", locked.Locked.Subjects(), serverID)
	return lock.LockedSubjectContract{}
}

func subjectHasMCPPlacement(subject topology.SubjectID, placementID aggregate.MCPPlacementID) bool {
	placement, ok := aggregate.MCPPlacementForSubject(subject)
	return ok && placement.ID() == placementID
}

func statusLastDelegateAttempt(
	t *testing.T,
	subject topology.SubjectID,
	selectedTarget target.Target,
	selectedScope target.Scope,
	identityKey string,
	status durableattempt.DelegateAttemptStatus,
	reason durableattempt.DelegateAttemptReason,
) durableattempt.DelegateAttempt {
	t.Helper()
	var exitCode *int
	if reason == durableattempt.DelegateReasonNonZeroExit {
		value := 17
		exitCode = &value
	}
	attempt, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
		Subject:         subject,
		Target:          selectedTarget,
		Scope:           selectedScope,
		PlanIdentityKey: identityKey,
		ObservedAt:      time.Date(2026, time.June, 30, 10, 0, 0, 0, time.UTC),
		Status:          status,
		Reason:          reason,
		ExitCode:        exitCode,
	})
	if err != nil {
		t.Fatalf("NewDelegateAttempt returned error: %v", err)
	}
	return attempt
}

func writeStatusMCPManifest(t *testing.T, manifestPath string) {
	t.Helper()
	writeTestFile(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)
}

func writeStatusOpenCodeMCPManifest(t *testing.T, manifestPath string) {
	t.Helper()
	writeTestFile(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), `version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
		`)
}

func writeStatusStatefile(t *testing.T, path string, snapshot durable.Snapshot) {
	t.Helper()
	content, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal statefile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create statefile directory: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write statefile: %v", err)
	}
}

func statusMCPStateSnapshot(
	t *testing.T,
	placementID aggregate.MCPPlacementID,
	serverID string,
	canonical []byte,
	attempts ...durableattempt.DelegateAttempt,
) durable.Snapshot {
	t.Helper()
	operations, ok := mcptest.OperationsForPlacementID(placementID)
	if !ok {
		t.Fatalf("MCP placement operations %q are unavailable", placementID)
	}
	placement := operations.Placement()
	contribution, err := placement.Contribution(serverID, string(canonical))
	if err != nil {
		t.Fatalf("construct MCP contribution: %v", err)
	}
	subject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), serverID)
	if err != nil {
		t.Fatalf("construct MCP projection subject: %v", err)
	}
	state, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatalf("construct MCP aggregate state: %v", err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedAggregates: []durable.ManagedAggregateState{state},
		DelegateAttempts:  attempts,
	})
	if err != nil {
		t.Fatalf("construct status snapshot: %v", err)
	}
	return snapshot
}

func canonicalStatusMCPEntryWithArgs(t *testing.T, serverID string, command string, args []string) []byte {
	t.Helper()
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            append([]string(nil), args...),
		Env:             map[string]string{},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	return canonical
}
