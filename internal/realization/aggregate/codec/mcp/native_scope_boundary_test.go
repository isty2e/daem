package mcpcodec

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestMCPCodecRefusesNativeScopeOutsideItsLegacyContract(t *testing.T) {
	placement := mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeProject).Placement()
	key, err := output.NewKeyPath("mcpServers", "a/b")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewProjectionContract(aggregate.ProjectionContractInput{
		PlacementID: string(placement.ID()), Target: placement.Target(), Scope: placement.Scope(),
		AggregateRoot: placement.ConfigPath(), Footprint: scope,
		MergeUnit: aggregate.MergeUnit(placement.MergeUnit()), Cardinality: aggregate.ContributionExclusive,
		SiblingRetention: aggregate.PreserveUnmanagedSiblings, SiblingPreservation: aggregate.PreserveSiblingsSemantic,
		Equivalence: aggregate.EquivalenceCanonicalSemantic, CodecContractID: placement.CodecContractID(), ComparedFields: placement.ComparedFields(),
	})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := For(contract.CodecContractID())
	if !ok {
		t.Fatal("fixture has no MCP codec")
	}
	_, failure := codec.Read(aggregate.ExistingDocument([]byte(`{"mcpServers":{}}`)), selection)
	if failure == nil || failure.Reason() != aggregate.CodecFailureSelectedShapeUnsupported || failure.ContentPath() != "" {
		t.Fatalf("legacy MCP codec did not refuse the native scope at the document boundary: %v", failure)
	}
}
