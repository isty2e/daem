package lockfile

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestLegacyRealizationEncodingRefusesUnrepresentableNativeScope(t *testing.T) {
	key, err := output.NewKeyPath("providers", "a/b")
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
	contribution, err := aggregate.NewManagedContribution(contract, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := realization.NewManagedAggregateContribution(contribution)
	if err != nil {
		t.Fatal(err)
	}
	dto, err := realizationToDTO(spec, true)
	if !errors.Is(err, output.ErrNotLegacyFootprint) || dto != nil {
		t.Fatalf("legacy encoder returned a lossy or partial realization: dto=%#v err=%v", dto, err)
	}
}
