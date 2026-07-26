package carrier_test

import (
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestStateAuthorityUsesStatefileIdentityAndRetainsExactProvenance(t *testing.T) {
	root := t.TempDir()
	first := mustStateAuthority(t, root, "first.toml")
	second, err := durablecarrier.NewStateAuthority(first.StatefileKey(), filepath.Join(root, "second.toml"))
	if err != nil {
		t.Fatalf("NewStateAuthority returned error: %v", err)
	}
	if !first.Equal(second) {
		t.Fatal("same statefile key did not identify one authority")
	}
	if first.ExactEqual(second) {
		t.Fatal("diagnostic manifest provenance was ignored by exact equality")
	}

	for _, test := range []struct {
		name         string
		statefileKey string
		manifestPath string
		want         string
	}{
		{name: "relative state", statefileKey: "state.json", manifestPath: filepath.Join(root, "daem.toml"), want: "must be absolute"},
		{name: "unclean manifest", statefileKey: filepath.Join(root, "state.json"), manifestPath: filepath.Join(root, "nested") + "/../daem.toml", want: "must be clean"},
		{name: "nul", statefileKey: filepath.Join(root, "state.json") + "\x00", manifestPath: filepath.Join(root, "daem.toml"), want: "NUL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := durablecarrier.NewStateAuthority(test.statefileKey, test.manifestPath); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestManagedCarrierIdentityRejectsCrossFamilyAndManagedKeyDrift(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)

	wrongSubject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"codex.plugin-carrier",
		"context7",
	)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	if _, err := durablecarrier.NewManagedCarrierIdentity(
		fixture.carrier,
		wrongSubject,
		fixture.relation,
	); err == nil || !strings.Contains(err.Error(), "outside carrier family") {
		t.Fatalf("cross-family error = %v", err)
	}

	other := carrierFixtureFor(t, "other", "context7@official", target.ScopeGlobal)
	if _, err := durablecarrier.NewManagedCarrierIdentity(
		fixture.carrier,
		fixture.subject,
		other.relation,
	); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("managed-key drift error = %v", err)
	}
}

func TestManagedCarrierIdentityReconstructsCompleteLockedIdentity(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)

	reconstructed, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(fixture.contract)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", reconstructed, admitted, err)
	}
	if !reconstructed.ExactEqual(fixture.identity) {
		t.Fatalf("reconstructed identity = %#v, want %#v", reconstructed, fixture.identity)
	}
	if !reconstructed.MatchesLockedRecord(fixture.contract, fixture.installRequest) {
		t.Fatal("reconstructed identity did not match its exact locked acquisition record")
	}
}

func TestPendingInstallPromotionPreservesExactAcquisitionIdentity(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	pending, err := durablecarrier.NewPendingCarrierInstall(
		owner,
		fixture.identity,
		fixture.installRequest,
	)
	if err != nil {
		t.Fatalf("NewPendingCarrierInstall returned error: %v", err)
	}
	claim, err := durablecarrier.ClaimAfterObservedInstall(
		pending,
		exactCorrelation(t, fixture),
		nil,
	)
	if err != nil {
		t.Fatalf("ClaimAfterObservedInstall returned error: %v", err)
	}
	if !claim.Owner().ExactEqual(owner) ||
		!claim.Identity().ExactEqual(fixture.identity) ||
		!claim.InstallRequest().Equal(fixture.installRequest) ||
		claim.Provenance() != durablecarrier.ClaimProvenanceInstalledObserved {
		t.Fatalf("promoted claim lost acquisition identity: %#v", claim)
	}
	if !claim.MatchesLockedRecord(fixture.contract) {
		t.Fatal("claim did not match the exact locked acquisition record")
	}

	changed := carrierFixtureFor(t, "context7", "context7-next@official", target.ScopeGlobal)
	if claim.MatchesLockedRecord(changed.contract) {
		t.Fatal("claim matched a changed carrier source")
	}
}

func TestObservedInstallPreservesExactRetainedClaimProvenance(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	pending, err := durablecarrier.NewPendingCarrierInstall(
		owner,
		fixture.identity,
		fixture.installRequest,
	)
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		fixture.identity,
		fixture.installRequest,
		durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
	)
	if err != nil {
		t.Fatal(err)
	}

	retained, err := durablecarrier.ClaimAfterObservedInstall(
		pending,
		exactCorrelation(t, fixture),
		[]durablecarrier.ManagedCarrierClaim{adopted},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !retained.ExactEqual(adopted) {
		t.Fatalf("retained claim = %#v, want adopted provenance", retained)
	}
	if _, err := durablecarrier.ClaimAfterObservedInstall(
		pending,
		exactCorrelation(t, fixture),
		[]durablecarrier.ManagedCarrierClaim{adopted, adopted},
	); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate retained claim error = %v", err)
	}

	created, err := durablecarrier.ClaimAfterObservedInstall(
		pending,
		exactCorrelation(t, fixture),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if created.Provenance() != durablecarrier.ClaimProvenanceInstalledObserved {
		t.Fatalf("new claim provenance = %q", created.Provenance())
	}

	changed := carrierFixtureFor(t, "context7", "other@official", target.ScopeGlobal)
	conflict := claimForFixture(t, changed, owner)
	if _, err := durablecarrier.ClaimAfterObservedInstall(
		pending,
		exactCorrelation(t, fixture),
		[]durablecarrier.ManagedCarrierClaim{conflict},
	); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("retained conflict error = %v", err)
	}
}

func TestManagedCarrierClaimRejectsUnsupportedProvenance(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	_, err := durablecarrier.NewManagedCarrierClaim(
		mustStateAuthority(t, t.TempDir(), "daem.toml"),
		fixture.identity,
		fixture.installRequest,
		durablecarrier.ClaimProvenance("attempt_succeeded"),
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error = %v, want unsupported provenance", err)
	}
}

func TestManagedCarrierClaimSameAcquisitionIgnoresOnlyProvenance(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	installed := claimForFixture(t, fixture, owner)
	adopted, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		fixture.identity,
		fixture.installRequest,
		durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	if installed.ExactEqual(adopted) || !installed.SameAcquisition(adopted) {
		t.Fatalf("provenance variants = installed %#v adopted %#v", installed, adopted)
	}

	otherSource := claimForFixture(
		t,
		carrierFixtureFor(t, "context7", "other@official", target.ScopeGlobal),
		owner,
	)
	otherOwner := claimForFixture(
		t,
		fixture,
		mustStateAuthority(t, t.TempDir(), "daem.toml"),
	)
	if installed.SameAcquisition(otherSource) || installed.SameAcquisition(otherOwner) {
		t.Fatal("SameAcquisition ignored source or owner drift")
	}
}

func TestObservedAdoptionCreatesExactClaimWithoutHostTransition(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")

	claim, err := durablecarrier.ClaimFromObservedAdoption(
		owner,
		fixture.contract,
		exactCorrelation(t, fixture),
	)
	if err != nil {
		t.Fatalf("ClaimFromObservedAdoption returned error: %v", err)
	}
	if !claim.Owner().ExactEqual(owner) ||
		!claim.Identity().ExactEqual(fixture.identity) ||
		!claim.InstallRequest().Equal(fixture.installRequest) ||
		claim.Provenance() != durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved {
		t.Fatalf("adopted claim lost exact locked identity: %#v", claim)
	}
	if !claim.MatchesLockedRecord(fixture.contract) {
		t.Fatal("adopted claim did not match the exact locked acquisition record")
	}
}

func TestObservedAdoptionRejectsInexactStaleAndMixedEvidence(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	other := carrierFixtureFor(t, "other", "other@official", target.ScopeGlobal)
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	staleInventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceStale,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name        string
		observation observerelation.CorrelationResult
	}{
		{name: "unkeyed", observation: unkeyedCorrelation(t, fixture)},
		{
			name: "unsupported",
			observation: observerelation.Correlate(
				fixture.relation,
				observerelation.UnsupportedInventory(),
			),
		},
		{
			name:        "stale",
			observation: observerelation.Correlate(fixture.relation, staleInventory),
		},
		{name: "mixed identity", observation: exactCorrelation(t, other)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := durablecarrier.ClaimFromObservedAdoption(
				owner,
				fixture.contract,
				test.observation,
			); err == nil {
				t.Fatal("ClaimFromObservedAdoption accepted ineligible evidence")
			}
		})
	}
}

func TestObservedAdoptionRejectsSourceInexactCarrierAndZeroOwner(t *testing.T) {
	antigravity := carrierFixtureForSpec(
		t,
		"guidance",
		desiredextension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		desiredextension.SourceKindHostSource,
		"guidance@google",
		"guidance",
	)
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	if _, err := durablecarrier.ClaimFromObservedAdoption(
		owner,
		antigravity.contract,
		unkeyedCorrelation(t, antigravity),
	); err == nil || !strings.Contains(err.Error(), "source-exact") {
		t.Fatalf("Antigravity adoption error = %v, want source-exact refusal", err)
	}

	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	if _, err := durablecarrier.ClaimFromObservedAdoption(
		durablecarrier.StateAuthority{},
		fixture.contract,
		exactCorrelation(t, fixture),
	); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("zero-owner adoption error = %v, want owner refusal", err)
	}
}

func TestCarrierClaimEqualityKeepsProvenanceOutOfOccupancyIdentity(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	owner := mustStateAuthority(t, t.TempDir(), "daem.toml")
	installed := claimForFixture(t, fixture, owner)
	adopted, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		fixture.identity,
		fixture.installRequest,
		durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	if installed.ExactEqual(adopted) {
		t.Fatal("claim exact equality ignored provenance")
	}
	if _, err := durablecarrier.NewCarrierOccupancy(
		fixture.carrier,
		[]durablecarrier.ManagedCarrierClaim{installed, adopted},
	); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("occupancy error = %v, want duplicate owner/relation refusal", err)
	}
}

func TestPendingInstallCannotPromoteWithoutFreshExactPostObservation(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	pending, err := durablecarrier.NewPendingCarrierInstall(
		mustStateAuthority(t, t.TempDir(), "daem.toml"),
		fixture.identity,
		fixture.installRequest,
	)
	if err != nil {
		t.Fatalf("NewPendingCarrierInstall returned error: %v", err)
	}
	tests := []struct {
		name        string
		observation observerelation.CorrelationResult
	}{
		{
			name: "unsupported",
			observation: observerelation.Correlate(
				fixture.relation,
				observerelation.UnsupportedInventory(),
			),
		},
		{
			name: "fresh missing",
			observation: func() observerelation.CorrelationResult {
				inventory, inventoryErr := observerelation.NewInventory(observerelation.InventorySpec{
					Availability: observerelation.InventorySupported,
					Freshness:    observerelation.EvidenceFresh,
				})
				if inventoryErr != nil {
					t.Fatalf("NewInventory returned error: %v", inventoryErr)
				}
				return observerelation.Correlate(fixture.relation, inventory)
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := durablecarrier.ClaimAfterObservedInstall(
				pending,
				test.observation,
				nil,
			); err == nil || !strings.Contains(err.Error(), "fresh exact") {
				t.Fatalf("error = %v, want exact post-observation rejection", err)
			}
		})
	}
}

func TestPendingInstallPromotionBoundsAntigravityNameEvidence(t *testing.T) {
	antigravity := carrierFixtureForSpec(
		t,
		"guidance",
		desiredextension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		desiredextension.SourceKindHostSource,
		"guidance@google",
		"guidance",
	)
	pi := carrierFixtureForSpec(
		t,
		"tools",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeGlobal,
		desiredextension.SourceKindHostSource,
		"npm:@acme/tools",
		"npm:@acme/tools",
	)
	opaqueAntigravity := carrierFixtureForSpec(
		t,
		"opaque-guidance",
		desiredextension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		desiredextension.SourceKindHostSource,
		"opaque-guidance",
		"opaque-guidance",
	)

	for _, test := range []struct {
		name      string
		fixture   carrierFixture
		wantError bool
	}{
		{name: "Antigravity exact pending bounds unkeyed name evidence", fixture: antigravity},
		{name: "Antigravity opaque source remains ineligible", fixture: opaqueAntigravity, wantError: true},
		{name: "Pi exact pending cannot upgrade unkeyed source spelling", fixture: pi, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			pending, err := durablecarrier.NewPendingCarrierInstall(
				mustStateAuthority(t, t.TempDir(), "daem.toml"),
				test.fixture.identity,
				test.fixture.installRequest,
			)
			if err != nil {
				t.Fatal(err)
			}
			observation := unkeyedCorrelation(t, test.fixture)
			claim, err := durablecarrier.ClaimAfterObservedInstall(pending, observation, nil)
			if test.wantError {
				if err == nil {
					t.Fatal("ClaimAfterObservedInstall upgraded source-inexact evidence")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !claim.Identity().ExactEqual(test.fixture.identity) {
				t.Fatalf("claim identity = %#v, want pending identity", claim.Identity())
			}
		})
	}
}

type carrierFixture struct {
	carrier        extensiontopology.Carrier
	subject        topology.SubjectID
	relation       hostrelation.ExpectedRelation
	identity       durablecarrier.ManagedCarrierIdentity
	contract       lock.LockedSubjectContract
	installRequest realizationdelegate.Request
}

func carrierFixtureFor(
	t *testing.T,
	name string,
	sourceValue string,
	scope target.Scope,
) carrierFixture {
	return carrierFixtureForSpec(
		t,
		name,
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		scope,
		desiredextension.SourceKindMarketplace,
		sourceValue,
		sourceValue,
	)
}

func carrierFixtureForSpec(
	t *testing.T,
	name string,
	carrierFamily desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceKind desiredextension.SourceKind,
	sourceValue string,
	subjectValue string,
) carrierFixture {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		sourceKind,
		sourceValue,
	)
	if err != nil {
		t.Fatalf("NewSourceRef returned error: %v", err)
	}
	value, err := desiredextension.New(desiredextension.Spec{
		Name:    name,
		Carrier: carrierFamily,
		Target:  selectedTarget,
		Scope:   scope,
		Source:  source,
	})
	if err != nil {
		t.Fatalf("extension.New returned error: %v", err)
	}
	subject, err := extensiontopology.Relation(value)
	if err != nil {
		t.Fatalf("extension topology relation returned error: %v", err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(subjectValue)
	if err != nil {
		t.Fatalf("NewSubjectKey returned error: %v", err)
	}
	relation, err := hostrelation.Derive(value.CarrierKey(), subject, subjectKey)
	if err != nil {
		t.Fatalf("Derive returned error: %v", err)
	}
	carrier, err := extensiontopology.NewCarrier(value.CarrierKey())
	if err != nil {
		t.Fatalf("NewCarrier returned error: %v", err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, relation)
	if err != nil {
		t.Fatalf("NewManagedCarrierIdentity returned error: %v", err)
	}
	contract, err := lock.NewDelegatedRelationCarrierContract(
		value.ID(),
		value.CarrierKey(),
		subject,
		subjectKey,
	)
	if err != nil {
		t.Fatalf("NewDelegatedRelationCarrierContract returned error: %v", err)
	}
	installRequest, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatalf("DelegatedOperationRequest returned error: %v", err)
	}
	return carrierFixture{
		carrier:        carrier,
		subject:        subject,
		relation:       relation,
		identity:       identity,
		contract:       contract,
		installRequest: installRequest,
	}
}

func unkeyedCorrelation(t *testing.T, fixture carrierFixture) observerelation.CorrelationResult {
	t.Helper()
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey: string(fixture.relation.SubjectKey()),
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

func mustStateAuthority(t *testing.T, root string, manifestName string) durablecarrier.StateAuthority {
	t.Helper()
	authority, err := durablecarrier.NewStateAuthority(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, manifestName),
	)
	if err != nil {
		t.Fatalf("NewStateAuthority returned error: %v", err)
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
		t.Fatalf("NewPendingCarrierInstall returned error: %v", err)
	}
	claim, err := durablecarrier.ClaimAfterObservedInstall(
		pending,
		exactCorrelation(t, fixture),
		nil,
	)
	if err != nil {
		t.Fatalf("ClaimAfterObservedInstall returned error: %v", err)
	}
	return claim
}

func exactCorrelation(t *testing.T, fixture carrierFixture) observerelation.CorrelationResult {
	t.Helper()
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey:            string(fixture.relation.SubjectKey()),
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(fixture.relation.ManagedInstanceKey()),
	})
	if err != nil {
		t.Fatalf("NewRow returned error: %v", err)
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         []observerelation.Row{row},
	})
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	return observerelation.Correlate(fixture.relation, inventory)
}
