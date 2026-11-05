package publication

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type documentClaimFixture struct {
	owner       stateauthority.Authority
	exact       pathauthority.Exact
	provisional pathauthority.Provisional
	alias       aggregate.DocumentAddress
}

func documentClaimsForTest(t *testing.T) documentClaimFixture {
	t.Helper()
	root := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	exact := pathtest.DarwinCaseSensitive(filepath.Join(root, "host", "Caf\u00e9.json"))
	namespace := pathtest.DarwinCaseSensitive(root)
	provisional, err := pathauthority.NewProvisional(exact.Key(), exact.Witness(), namespace.Key(), namespace.Witness())
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("@pi-agent/custom.json")
	if err != nil {
		t.Fatal(err)
	}
	id, found := aggregate.HostConfigCodecForSyntax("json")
	if !found {
		t.Fatal("native JSON codec is unavailable")
	}
	key, err := output.NewKeyPath("selected")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, destination, footprint, id)
	if err != nil {
		t.Fatal(err)
	}
	return documentClaimFixture{owner: owner, exact: exact, provisional: provisional, alias: contract.Address().Document()}
}

func documentKeyFootprint(t *testing.T, parts ...string) output.Footprint {
	t.Helper()
	key, err := output.NewKeyPath(parts...)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	return footprint
}

func documentActiveClaim(t *testing.T, fixture documentClaimFixture, footprint output.Footprint) ownership.Claim {
	t.Helper()
	address, err := ownership.NewManagedAddress(fixture.exact, footprint)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, fixture.owner)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func piSelectionContract(t *testing.T, destination string, parts ...string) aggregate.ProjectionContract {
	t.Helper()
	root, err := output.Parse(destination)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath(parts...)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, root, footprint, "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func selectionNativeRow(t *testing.T, contract aggregate.ProjectionContract, value string) durable.ManagedAggregateState {
	t.Helper()
	id, err := entity.New(entity.KindHostConfig, "native")
	if err != nil {
		t.Fatal(err)
	}
	address := contract.Address()
	subject, err := topologyprojection.HostConfigKeySubject(id, address.Document().Target(), address.Document().Scope(), address.KeyPath())
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

func stateGroupContract(t *testing.T, alias aggregate.DocumentAddress, parts ...string) aggregate.ProjectionContract {
	t.Helper()
	contract, err := aggregate.NewHostConfigProjectionContract(alias.Target(), alias.Scope(), alias.AggregateRoot(), documentKeyFootprint(t, parts...), "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func membershipNativeRow(t *testing.T, alias aggregate.DocumentAddress, codec aggregate.CodecContractID, parts ...string) durable.ManagedAggregateState {
	t.Helper()
	contract, err := aggregate.NewHostConfigProjectionContract(alias.Target(), alias.Scope(), alias.AggregateRoot(), documentKeyFootprint(t, parts...), codec)
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
