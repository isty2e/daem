package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit"
)

func loadMCPStatefile(t *testing.T, root string) durable.Snapshot {
	t.Helper()
	state, err := statefile.Load(t.Context(), filepath.Join(root, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("load statefile: %v", err)
	}
	return state
}

func assertMCPStateSubject(t *testing.T, file durable.Snapshot, serverID string) {
	t.Helper()
	for _, state := range file.ManagedAggregates() {
		subject := state.Subject()
		if subject.Kind() != topology.SubjectProjection ||
			subject.Namespace() != "claude-code.project.mcp-server" ||
			subject.Key() != serverID {
			continue
		}
		contribution := state.Contribution()
		if contribution.Target() != target.TargetClaudeCode ||
			contribution.Scope() != target.ScopeProject ||
			contribution.AggregateRoot().String() != aggregate.ClaudeProjectMCPConfigPath ||
			contribution.ContentPath() != mcpcodec.ClaudeProjectMCPContentPath(serverID) ||
			contribution.CanonicalContribution() == "" {
			t.Fatalf("MCP aggregate state = %#v, want managed Claude project MCP subject", state)
		}
		return
	}
	t.Fatalf("MCP subject state for %q not found in %#v", serverID, file.ManagedAggregates())
}

func assertMCPStateSubjectHash(t *testing.T, file durable.Snapshot, serverID string, wantHash string) {
	t.Helper()
	for _, state := range file.ManagedAggregates() {
		subject := state.Subject()
		if subject.Namespace() == "claude-code.project.mcp-server" && subject.Key() == serverID {
			gotHash := string(artifact.HashFileContent([]byte(state.Contribution().CanonicalContribution())))
			if gotHash != wantHash {
				t.Fatalf("MCP state contribution hash = %q, want %q", gotHash, wantHash)
			}
			return
		}
	}
	t.Fatalf("MCP subject state for %q not found in %#v", serverID, file.ManagedAggregates())
}

func assertMCPStateSubjectMissing(t *testing.T, file durable.Snapshot, serverID string) {
	t.Helper()
	for _, state := range file.ManagedAggregates() {
		subject := state.Subject()
		if subject.Kind() == topology.SubjectProjection &&
			subject.Namespace() == "claude-code.project.mcp-server" &&
			subject.Key() == serverID {
			t.Fatalf("MCP subject state for %q unexpectedly present: %#v", serverID, state)
		}
	}
}

func assertMCPStatefileMissing(t *testing.T, root string, label string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(root, ".daem", "state.json"))
	if err == nil {
		t.Fatalf("%s unexpectedly created statefile", label)
	}
	if !os.IsNotExist(err) {
		t.Fatalf("%s stat statefile: %v", label, err)
	}
}

func assertLockedMCPSubject(t *testing.T, file lock.File, serverID string) {
	assertLockedMCPSubjectForPlacement(t, file, serverID, target.TargetClaudeCode, target.ScopeProject,
		"claude-code.project.mcp-server", aggregate.ClaudeProjectMCPConfigPath,
		mcpcodec.ClaudeProjectMCPContentPath(serverID), aggregate.ClaudeProjectMCPStdioAdapterV1)
}

func assertLockedAntigravityMCPSubject(t *testing.T, file lock.File, serverID string) {
	assertLockedMCPSubjectForPlacement(t, file, serverID, target.TargetAntigravityCLI, target.ScopeGlobal,
		"antigravity-cli.global.mcp-server", aggregate.AntigravityGlobalMCPConfigPath,
		mcpcodec.AntigravityGlobalMCPContentPath(serverID), aggregate.AntigravityGlobalMCPCommandAdapterV1)
}

func assertLockedCodexMCPSubject(t *testing.T, file lock.File, serverID string) {
	assertLockedMCPSubjectForPlacement(t, file, serverID, target.TargetCodex, target.ScopeProject,
		"codex.project.mcp-server", aggregate.CodexProjectMCPConfigPath,
		mcpcodec.CodexProjectMCPContentPath(serverID), aggregate.CodexProjectMCPStdioCommandV1)
}

func assertLockedCodexGlobalMCPSubject(t *testing.T, file lock.File, serverID string) {
	assertLockedMCPSubjectForPlacement(t, file, serverID, target.TargetCodex, target.ScopeGlobal,
		"codex.global.mcp-server", aggregate.CodexGlobalMCPConfigPath,
		mcpcodec.CodexGlobalMCPContentPath(serverID), aggregate.CodexGlobalMCPStdioEnvVarsV1)
}

func assertLockedClaudeGlobalMCPSubject(t *testing.T, file lock.File, serverID string) {
	assertLockedMCPSubjectForPlacement(t, file, serverID, target.TargetClaudeCode, target.ScopeGlobal,
		"claude-code.global.mcp-server", aggregate.ClaudeGlobalMCPConfigPath,
		mcpcodec.ClaudeGlobalMCPContentPath(serverID), aggregate.ClaudeGlobalMCPStdioEnvAdapterV1)
}

func assertLockedMCPSubjectForPlacement(
	t *testing.T,
	file lock.File,
	serverID string,
	selectedTarget target.Target,
	scope target.Scope,
	namespace string,
	configPath string,
	contentPath string,
	adapterContract string,
) {
	t.Helper()
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one", file.Locked.Subjects())
	}
	contract := file.Locked.Subjects()[0]
	subject := contract.SubjectID()
	if subject.Kind() != topology.SubjectProjection || subject.Namespace() != namespace || subject.Key() != serverID {
		t.Fatalf("locked subject = %#v, want projection %q/%q", subject, namespace, serverID)
	}
	contribution := testkit.LockedManagedAggregateContribution(t, contract)
	if contribution.Target() != selectedTarget || contribution.Scope() != scope ||
		contribution.AggregateRoot().String() != configPath || contribution.ContentPath() != contentPath ||
		string(contribution.CodecContractID()) != adapterContract {
		t.Fatalf("managed aggregate contribution = %#v", contribution)
	}
}
