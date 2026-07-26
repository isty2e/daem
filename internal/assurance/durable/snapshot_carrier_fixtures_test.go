package durable_test

import (
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type carrierFixture struct {
	relation       hostrelation.ExpectedRelation
	identity       durablecarrier.ManagedCarrierIdentity
	installRequest realizationdelegate.Request
}

func carrierFixtureFor(
	t *testing.T,
	name string,
	sourceValue string,
	scope target.Scope,
) carrierFixture {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		sourceValue,
	)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{
		Name:    name,
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   scope,
		Source:  source,
	})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := extensiontopology.Relation(value)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(sourceValue)
	if err != nil {
		t.Fatal(err)
	}
	relation, err := hostrelation.Derive(value.CarrierKey(), subject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := extensiontopology.NewCarrier(value.CarrierKey())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, relation)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := lock.NewDelegatedRelationCarrierContract(
		value.ID(),
		value.CarrierKey(),
		subject,
		subjectKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	installRequest, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	return carrierFixture{
		relation:       relation,
		identity:       identity,
		installRequest: installRequest,
	}
}

func mustStateAuthority(
	t *testing.T,
	root string,
	manifestName string,
) durablecarrier.StateAuthority {
	t.Helper()
	authority, err := durablecarrier.NewStateAuthority(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, manifestName),
	)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func claimForFixture(
	t *testing.T,
	fixture carrierFixture,
	owner durablecarrier.StateAuthority,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	pending, err := durablecarrier.NewPendingCarrierInstall(
		owner,
		fixture.identity,
		fixture.installRequest,
	)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.ClaimAfterObservedInstall(
		pending,
		exactCorrelation(t, fixture),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func exactCorrelation(
	t *testing.T,
	fixture carrierFixture,
) observerelation.CorrelationResult {
	t.Helper()
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey:            string(fixture.relation.SubjectKey()),
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(fixture.relation.ManagedInstanceKey()),
	})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         []observerelation.Row{row},
	})
	if err != nil {
		t.Fatal(err)
	}
	return observerelation.Correlate(fixture.relation, inventory)
}

func relationOnlyRemovalPostconditions() effectpostcondition.Set {
	return effectpostcondition.Set{}
}

func removalRequestForTest(
	t *testing.T,
	key string,
) realizationdelegate.Request {
	t.Helper()
	request, err := realizationdelegate.NewRequest(
		"claude-code.plugin-carrier.remove."+key,
		"v1",
		"sha256:2222222222222222222222222222222222222222222222222222222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
