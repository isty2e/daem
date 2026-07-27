package execute

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/test/outputtest"
)

type mcpProjectionApplyFixture struct {
	root           string
	hostConfigPath string
	paths          Paths
	destination    output.Destination
	contentPath    func(string) string
}

func newMCPProjectionApplyFixture(t *testing.T) mcpProjectionApplyFixture {
	t.Helper()
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	return mcpProjectionApplyFixture{
		root:           root,
		hostConfigPath: filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath),
		destination:    outputtest.Parse(t, aggregate.ClaudeProjectMCPConfigPath),
		contentPath:    mcpcodec.ClaudeProjectMCPContentPath,
		paths: Paths{
			RecoveryDir:   filepath.Join(stateDir, "recovery"),
			StateDir:      stateDir,
			StatefilePath: filepath.Join(stateDir, "state.json"),
			ManifestRoot:  root,
		},
	}
}

func newClaudeGlobalMCPProjectionApplyFixture(t *testing.T) mcpProjectionApplyFixture {
	t.Helper()
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	return mcpProjectionApplyFixture{
		root:           root,
		hostConfigPath: filepath.Join(home, ".claude.json"),
		destination:    outputtest.Parse(t, aggregate.ClaudeGlobalMCPConfigPath),
		contentPath:    mcpcodec.ClaudeGlobalMCPContentPath,
		paths: Paths{
			RecoveryDir:   filepath.Join(stateDir, "recovery"),
			StateDir:      stateDir,
			StatefilePath: filepath.Join(stateDir, "state.json"),
			ManifestRoot:  root,
		},
	}
}

func (fixture mcpProjectionApplyFixture) canonicalEntry(
	t *testing.T,
	serverID string,
	command string,
) []byte {
	t.Helper()
	content, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(
		mcpcodec.ClaudeProjectMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            []string{"-y", "@upstream/" + serverID},
			Env:             map[string]string{"API_KEY": "${CONTEXT7_API_KEY}"},
			AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
		},
	)
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry: %v", err)
	}
	return content
}

func (fixture mcpProjectionApplyFixture) claudeGlobalCanonicalEntry(
	t *testing.T,
	serverID string,
	command string,
) []byte {
	t.Helper()
	content, err := mcpcodec.CanonicalClaudeGlobalMCPServerEntry(
		mcpcodec.ClaudeGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            []string{"-y", "@upstream/" + serverID},
			AdapterContract: aggregate.ClaudeGlobalMCPStdioAdapterV1,
		},
	)
	if err != nil {
		t.Fatalf("CanonicalClaudeGlobalMCPServerEntry: %v", err)
	}
	return content
}

func mergeMCPPlacementCanonicalEntry(
	t *testing.T,
	id aggregate.MCPPlacementID,
	existing []byte,
	serverID string,
	canonical []byte,
) ([]byte, error) {
	t.Helper()
	operations, ok := mcpcodec.ImplementedMCPPlacementOperationsForID(id)
	if !ok {
		t.Fatalf("MCP placement operations %q missing", id)
	}
	return operations.MergeCanonicalEntry(existing, serverID, canonical)
}

func (fixture mcpProjectionApplyFixture) writeMCPConfig(t *testing.T, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(fixture.hostConfigPath), 0o700); err != nil {
		t.Fatalf("create MCP config dir: %v", err)
	}
	if err := os.WriteFile(fixture.hostConfigPath, content, 0o600); err != nil {
		t.Fatalf("write MCP config: %v", err)
	}
}

func (fixture mcpProjectionApplyFixture) writeStatefile(t *testing.T, state durable.Snapshot) {
	t.Helper()
	content, err := statefile.Marshal(state)
	if err != nil {
		t.Fatalf("marshal statefile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.paths.StatefilePath), 0o700); err != nil {
		t.Fatalf("create statefile dir: %v", err)
	}
	if err := os.WriteFile(fixture.paths.StatefilePath, content, 0o600); err != nil {
		t.Fatalf("write statefile: %v", err)
	}
}
