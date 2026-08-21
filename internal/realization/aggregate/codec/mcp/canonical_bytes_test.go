package mcpcodec

import (
	"bytes"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestMCPPlacementsRejectSemanticallyEquivalentNoncanonicalEntryBytes(t *testing.T) {
	for _, test := range mcpProjectionMutationCases() {
		t.Run(test.name, func(t *testing.T) {
			operations, ok := ImplementedMCPPlacementOperationsForPlacement(test.placement)
			if !ok {
				t.Fatalf("placement operations %q missing", test.placement)
			}
			canonical := mustMutationCanonical(t, test, "context7", "npx")
			noncanonical := bytes.TrimSuffix(canonical, []byte("\n"))
			if bytes.Equal(noncanonical, canonical) {
				t.Fatal("canonical fixture has no final newline to remove")
			}

			_, err := operations.mergeCanonicalEntry(nil, "context7", noncanonical)
			assertMCPProjectionReason(t, err, MCPProjectionReasonCanonicalInvalid)
		})
	}
}

func TestMCPCodecRejectsNoncanonicalContributionBeforeOccupancy(t *testing.T) {
	for _, placementID := range []aggregate.MCPPlacementID{
		aggregate.MCPPlacementClaudeProject,
		aggregate.MCPPlacementCodexProject,
	} {
		t.Run(string(placementID), func(t *testing.T) {
			operations, ok := ImplementedMCPPlacementOperationsForPlacement(placementID)
			if !ok {
				t.Fatal("placement operations missing")
			}
			placement := operations.Placement()
			codec, ok := For(placement.CodecContractID())
			if !ok {
				t.Fatal("codec missing")
			}
			var canonical []byte
			for _, test := range mcpProjectionMutationCases() {
				if test.placement == placementID {
					canonical = mustMutationCanonical(t, test, "context7", "npx")
					break
				}
			}
			noncanonical := bytes.TrimSuffix(canonical, []byte("\n"))
			contribution := mcpCodecContribution(t, placement, "context7", noncanonical)
			err := codec.ValidateContributions(mcpCodecExclusiveSet(t, contribution))
			var failure *aggregate.CodecFailure
			if !errors.As(err, &failure) || failure.Reason() != aggregate.CodecFailureCanonicalInvalid {
				t.Fatalf("ValidateContributions(noncanonical) = %v, want canonical invalid", err)
			}
		})
	}
}
