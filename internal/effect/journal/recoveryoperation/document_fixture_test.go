package recoveryoperation

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func selectionNativeRow(t *testing.T, contract aggregate.ProjectionContract, value string) durable.ManagedAggregateState {
	t.Helper()
	id, err := entity.New(entity.KindAgentConfig, "native")
	if err != nil {
		t.Fatal(err)
	}
	address := contract.Address()
	subject, err := topologyprojection.AgentConfigKeySubject(id, address.Document().Target(), address.Document().Scope(), address.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := aggregate.NewManagedContribution(contract, value)
	if err != nil {
		t.Fatal(err)
	}
	row, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func selectionState(t *testing.T, rows ...durable.ManagedAggregateState) durable.Snapshot {
	t.Helper()
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: rows})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func membershipNativeRow(t *testing.T, alias aggregate.DocumentAddress, codec aggregate.CodecContractID, parts ...string) durable.ManagedAggregateState {
	t.Helper()
	contract, err := aggregate.NewAgentConfigProjectionContract(alias.Target(), alias.Scope(), alias.AggregateRoot(), documentKeyFootprint(t, parts...), codec)
	if err != nil {
		t.Fatal(err)
	}
	return selectionNativeRow(t, contract, "1")
}

func membershipBindingValue(t *testing.T, fixture documentClaimFixture) ownership.DocumentBindingValue {
	t.Helper()
	binding, err := ownership.NewDocumentBinding(fixture.owner, fixture.alias.AggregateRoot(), fixture.exact)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func membershipIntent(t *testing.T, fixture documentClaimFixture, before, after ownership.DocumentBindingValue) ownership.DocumentBindingIntent {
	t.Helper()
	intent, err := ownership.NewExactDocumentBindingIntent(fixture.owner, fixture.alias.AggregateRoot(), before, after)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}
