package mcpcodec

import (
	"bytes"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestAbsoluteCommandPathRoundTripsEveryImplementedMCPPlacement(t *testing.T) {
	const (
		serverID     = "codegraph"
		absolutePath = "/opt/example/bin/codegraph"
	)

	command, err := desiredmcp.NewAbsolutePathCommand(absolutePath)
	if err != nil {
		t.Fatalf("NewAbsolutePathCommand returned error: %v", err)
	}
	transport, err := desiredmcp.NewStdioTransport(command, []string{"serve", "--mcp"}, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport returned error: %v", err)
	}

	for _, placement := range aggregate.ImplementedMCPPlacements() {
		t.Run(string(placement.ID()), func(t *testing.T) {
			binding, err := desiredmcp.NewBinding(
				placement.Target(),
				placement.Scope(),
				transport,
				desiredmcp.OnAbsentRemoveBinding,
			)
			if err != nil {
				t.Fatalf("NewBinding returned error: %v", err)
			}
			server, err := desiredmcp.New(desiredmcp.Spec{
				Name:     serverID,
				Bindings: []desiredmcp.Binding{binding},
			})
			if err != nil {
				t.Fatalf("New returned error: %v", err)
			}

			canonical, err := CanonicalMCPBindingContribution(server, binding, placement)
			if err != nil {
				t.Fatalf("CanonicalMCPBindingContribution returned error: %v", err)
			}
			operations, ok := ImplementedMCPPlacementOperationsForCodecContract(placement.CodecContractID())
			if !ok {
				t.Fatalf("placement %q has no operations", placement.ID())
			}
			document, err := operations.MergeCanonicalEntry(nil, serverID, canonical)
			if err != nil {
				t.Fatalf("MergeCanonicalEntry returned error: %v", err)
			}
			extracted, present, err := operations.ExtractCanonicalEntry(document, serverID)
			if err != nil {
				t.Fatalf("ExtractCanonicalEntry returned error: %v", err)
			}
			if !present || !bytes.Equal(extracted, canonical) {
				t.Fatalf("extracted = %q, present=%t, want canonical %q", extracted, present, canonical)
			}
			comparison, err := operations.CompareCanonicalEntry(document, serverID, canonical)
			if err != nil {
				t.Fatalf("CompareCanonicalEntry returned error: %v", err)
			}
			if !comparison.Present || !comparison.Equivalent {
				t.Fatalf("comparison = %#v, want present equivalent entry", comparison)
			}
		})
	}
}
