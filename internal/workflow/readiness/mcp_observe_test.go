package readiness

import (
	"os"
	"path/filepath"
	"testing"

	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
)

func TestUnsupportedAlternateMCPProjectionConfigUsesCanonicalPlacementPath(t *testing.T) {
	root := t.TempDir()
	operations, ok := mcpcodec.ImplementedMCPPlacementOperationsForCodecContract(
		aggregate.MCPCodecOpenCodeProjectLocal,
	)
	if !ok {
		t.Fatal("OpenCode project MCP placement operations are missing")
	}
	conflictingConfigPath, hasConflictingConfigPath := operations.Placement().ConflictingConfigPath()
	if !hasConflictingConfigPath {
		t.Fatal("OpenCode project placement is missing its alternate config path")
	}
	hostPath := filepath.Join(root, filepath.FromSlash(conflictingConfigPath.RelativePath()))
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o700); err != nil {
		t.Fatalf("create conflicting config parent: %v", err)
	}
	if err := os.WriteFile(hostPath, []byte(`{"mcp":{}}`), 0o600); err != nil {
		t.Fatalf("write conflicting config: %v", err)
	}

	preconditions, admitted, err := aggregate.OperationPreconditionsForCodec(
		operations.Placement().CodecContractID(),
	)
	if err != nil {
		t.Fatalf("OperationPreconditionsForCodec: %v", err)
	}
	if !admitted || len(preconditions) != 1 {
		t.Fatalf("operation preconditions = %#v, %t, want one admitted row", preconditions, admitted)
	}
	precondition := preconditions[0]
	if precondition.DocumentAddress().AggregateRoot() != conflictingConfigPath {
		t.Fatalf(
			"precondition path = %q, want canonical placement conflict path %q",
			precondition.DocumentAddress().AggregateRoot(),
			conflictingConfigPath,
		)
	}
	present, err := observeAggregatePrecondition(
		liveobserve.DestinationResolver(hostpath.NewResolver(root).Resolve),
		precondition,
	)
	if err != nil {
		t.Fatalf("observeAggregatePrecondition: %v", err)
	}
	if present {
		t.Fatalf("conflicting config %q satisfied an absent-document precondition", conflictingConfigPath)
	}
}
