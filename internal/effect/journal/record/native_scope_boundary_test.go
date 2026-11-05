package record

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestLegacyJournalBoundariesRefuseNativeScopeWithoutLosslessEncoding(t *testing.T) {
	key, err := output.NewKeyPath("")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewProjectionContract(aggregate.ProjectionContractInput{
		PlacementID: "pi.host-config", Target: target.TargetPi, Scope: target.ScopeProject,
		AggregateRoot: outputtest.Parse(t, ".pi/settings.json"), Footprint: scope,
		MergeUnit: "native-key", Cardinality: aggregate.ContributionExclusive,
		SiblingRetention: aggregate.PreserveUnmanagedSiblings, SiblingPreservation: aggregate.PreserveSiblingsSemantic,
		Equivalence: aggregate.EquivalenceCanonicalSemantic, CodecContractID: "native-test-v1", ComparedFields: []string{"value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := persistedAggregateContract(contract)
	if !errors.Is(err, output.ErrNotLegacyFootprint) || persisted != nil {
		t.Fatalf("legacy journal returned a lossy contract: %#v, %v", persisted, err)
	}
	mutation, err := pathMutationFromAggregate(ManagedAggregateMutation{contract: contract})
	if !errors.Is(err, output.ErrNotLegacyFootprint) || mutation.AggregateContract != nil {
		t.Fatalf("legacy capture returned a usable partial mutation: %#v, %v", mutation, err)
	}
}
