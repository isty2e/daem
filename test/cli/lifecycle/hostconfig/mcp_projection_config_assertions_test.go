package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	mcptest "github.com/isty2e/daem/test/testkit/mcp"
)

func canonicalMCPEntryForSpec(t *testing.T, serverID string, spec mcpManifestSpec) []byte {
	t.Helper()
	env := make(map[string]string, len(spec.Env))
	for key, fromEnv := range spec.Env {
		env[key] = "${" + fromEnv + "}"
	}
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID:        serverID,
		Command:         spec.Command,
		Args:            append([]string(nil), spec.Args...),
		Env:             env,
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	return canonical
}

func compareCLIMCPPlacementCanonicalEntry(
	t *testing.T,
	id aggregate.MCPPlacementID,
	content []byte,
	serverID string,
	canonical []byte,
) (mcpcodec.MCPProjectionCanonicalComparison, error) {
	t.Helper()
	operations, ok := mcptest.OperationsForPlacementID(id)
	if !ok {
		t.Fatalf("MCP placement operations %q missing", id)
	}
	return operations.CompareCanonicalEntry(content, serverID, canonical)
}

func assertMCPConfigEquivalent(t *testing.T, root string, serverID string, spec mcpManifestSpec) {
	t.Helper()
	content := readMCPConfig(t, root)
	canonical := canonicalMCPEntryForSpec(t, serverID, spec)
	comparison, err := compareCLIMCPPlacementCanonicalEntry(t, aggregate.MCPPlacementClaudeProject, content, serverID, canonical)
	if err != nil {
		t.Fatalf("CompareClaudeProjectMCPServerCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present equivalent projection", comparison)
	}
}

func assertMCPConfigMissing(t *testing.T, root string, serverID string) {
	t.Helper()
	content := readMCPConfig(t, root)
	if _, present, err := mcpcodec.ExtractClaudeProjectMCPServerProjection(content, serverID); err != nil {
		t.Fatalf("ExtractClaudeProjectMCPServerProjection returned error: %v", err)
	} else if present {
		t.Fatalf("MCP server %q is still present in %s", serverID, content)
	}
}

func assertMCPConfigPreservesHostFields(t *testing.T, root string) {
	t.Helper()
	content := readMCPConfig(t, root)
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
	var residue map[string]string
	if err := json.Unmarshal(config["hostTrustResidue"], &residue); err != nil {
		t.Fatalf("decode hostTrustResidue: %v", err)
	}
	if residue["context7"] != "leave-alone" {
		t.Fatalf("hostTrustResidue = %#v, want preserved", residue)
	}
	if _, present, err := mcpcodec.ExtractClaudeProjectMCPServerProjection(content, "manual"); err != nil {
		t.Fatalf("manual MCP extraction returned error: %v", err)
	} else if !present {
		t.Fatal("manual MCP server was not preserved")
	}
}

func assertMCPConfigBytesEqual(t *testing.T, root string, want []byte, label string) {
	t.Helper()
	got := readMCPConfig(t, root)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s config changed:\ngot  %s\nwant %s", label, got, want)
	}
}

func readMCPConfig(t *testing.T, root string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath))
	if err != nil {
		t.Fatalf("read MCP config: %v", err)
	}
	return content
}
