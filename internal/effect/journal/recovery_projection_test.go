package journal

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/test/outputtest"
	mcptest "github.com/isty2e/daem/test/testkit/mcp"
)

func TestExtractRecoveryObservationProjectionRequiresMatchingContractAddress(t *testing.T) {
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
		Env:             map[string]string{},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	operations, ok := mcptest.OperationsForPlacementID(aggregate.MCPPlacementClaudeProject)
	if !ok {
		t.Fatal("Claude project MCP placement operations missing")
	}
	content, err := mcptest.MergeCanonicalEntry(operations, nil, "context7", canonical)
	if err != nil {
		t.Fatalf("MergeCanonicalEntry returned error: %v", err)
	}
	contract, err := operations.Placement().ProjectionContract("context7")
	if err != nil {
		t.Fatalf("ProjectionContract returned error: %v", err)
	}

	_, _, err = extractRecoveryObservationProjection(
		content,
		outputtest.Parse(t, "unknown-mcp-config.json"),
		output.ContentPath(mcpcodec.ClaudeProjectMCPContentPath("context7")),
		&contract,
		journalTestCodecs(),
	)
	if err == nil || !strings.Contains(err.Error(), "does not match observation") {
		t.Fatalf("error = %v, want mismatched contract address", err)
	}
}
