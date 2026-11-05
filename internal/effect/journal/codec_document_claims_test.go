package journal

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
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
