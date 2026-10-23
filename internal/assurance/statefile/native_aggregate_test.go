package statefile

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestLegacyStateAggregateRefusesNativeFamilyWithOrdinaryKey(t *testing.T) {
	state := nativeAggregateState(t, "settings")
	contribution := state.Contribution()
	contentPath, err := contribution.LegacyContentPath()
	if err != nil {
		t.Fatal("ordinary native key must be representable before testing family admission:", err)
	}
	persisted := legacyManagedAggregateDTO{
		Subject: persistedSubject(state.Subject()), PlacementID: contribution.PlacementID(),
		Target: string(contribution.Target()), Scope: string(contribution.Scope()),
		AggregateRoot: contribution.AggregateRoot().String(), ContentPath: contentPath,
		MergeUnit: string(contribution.MergeUnit()), Cardinality: string(contribution.Cardinality()),
		SiblingRetention: string(contribution.SiblingRetention()), SiblingPreservation: string(contribution.SiblingPreservation()),
		Equivalence: string(contribution.Equivalence()), CanonicalContribution: contribution.CanonicalContribution(),
		CodecContractID: string(contribution.CodecContractID()), ComparedFields: contribution.ComparedFields(),
	}
	if _, err := persisted.canonical(); err == nil {
		t.Fatal("legacy state row admitted native host-config family through an ordinary content path")
	}
}

func nativeAggregateState(t testing.TB, keys ...string) durable.ManagedAggregateState {
	t.Helper()
	document, err := configdocument.Parse(t.Context(), []byte(`{"value":"chosen"}`), configdocument.JSON, configdocument.Limits{MaximumBytes: 1024, MaximumDepth: 8})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := document.Leaves()
	if err != nil || len(entries) != 1 {
		t.Fatalf("native value fixture = %#v, %v", entries, err)
	}
	footprint := outputtest.KeyFootprint(t, keys...)
	codec, admitted := aggregate.HostConfigCodecForSyntax("json")
	if !admitted {
		t.Fatal("native JSON grammar was not admitted")
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, outputtest.Parse(t, "@pi-agent/settings.json"), footprint, codec)
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := aggregate.NewManagedContribution(contract, entries[0].Value.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "native-example")
	if err != nil {
		t.Fatal(err)
	}
	key, _ := footprint.Key()
	subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, key)
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
