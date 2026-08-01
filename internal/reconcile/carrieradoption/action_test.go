package carrieradoption_test

import (
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	reconcilehostroute "github.com/isty2e/daem/internal/reconcile/build/hostroute"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/target"
)

func TestCarrierAdoptionDecisionOrderIsDisjointAndExhaustive(t *testing.T) {
	fixture := newAdoptionFixture(t, target.ScopeProject, "alpha@official")
	installed := fixture.claim(t, fixture.owner, durablecarrier.ClaimProvenanceInstalledObserved)
	adopted := fixture.claim(t, fixture.owner, durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved)
	foreign := fixture.claim(
		t,
		mustAuthority(t, filepath.Join(t.TempDir(), "foreign")),
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	conflict := conflictingClaim(t, fixture)
	ineligible := fixture.lifecycleWithStoreAvailability(t, false)

	tests := []struct {
		name           string
		observation    observerelation.CorrelationResult
		claims         []durablecarrier.ManagedCarrierClaim
		lifecycle      carrieradoption.Lifecycle
		manageExisting bool
		want           carrieradoption.Result
		wantProposal   bool
		wantProvenance durablecarrier.ClaimProvenance
	}{
		{name: "exact eligible absent intent", observation: fixture.exact, lifecycle: fixture.lifecycle, want: carrieradoption.ResultPresentUnclaimed},
		{name: "exact eligible explicit intent", observation: fixture.exact, lifecycle: fixture.lifecycle, manageExisting: true, want: carrieradoption.ResultEligibleExactRelation, wantProposal: true, wantProvenance: durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved},
		{name: "exact lifecycle blocker precedes absent intent", observation: fixture.exact, lifecycle: ineligible, want: carrieradoption.ResultPresentUnclaimedIneligible},
		{name: "exact lifecycle blocker precedes explicit intent", observation: fixture.exact, lifecycle: ineligible, manageExisting: true, want: carrieradoption.ResultPresentUnclaimedIneligible},
		{name: "installed claim precedes lifecycle and intent", observation: fixture.exact, claims: []durablecarrier.ManagedCarrierClaim{installed}, lifecycle: ineligible, manageExisting: true, want: carrieradoption.ResultAlreadyClaimedCurrent, wantProvenance: durablecarrier.ClaimProvenanceInstalledObserved},
		{name: "adopted claim preserves provenance", observation: fixture.exact, claims: []durablecarrier.ManagedCarrierClaim{adopted}, lifecycle: ineligible, manageExisting: true, want: carrieradoption.ResultAlreadyClaimedCurrent, wantProvenance: durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved},
		{name: "claim conflict precedes current lifecycle", observation: fixture.exact, claims: []durablecarrier.ManagedCarrierClaim{conflict}, lifecycle: fixture.lifecycle, manageExisting: true, want: carrieradoption.ResultClaimConflict},
		{name: "foreign project claim cannot be stolen", observation: fixture.exact, claims: []durablecarrier.ManagedCarrierClaim{foreign}, lifecycle: fixture.lifecycle, manageExisting: true, want: carrieradoption.ResultClaimConflict},
		{name: "missing precedes claim conflict and intent", observation: fixture.correlation(t, observationMissing), claims: []durablecarrier.ManagedCarrierClaim{conflict}, lifecycle: ineligible, manageExisting: true, want: carrieradoption.ResultMissingRelation},
		{name: "unkeyed precedes claim conflict and intent", observation: fixture.correlation(t, observationUnkeyed), claims: []durablecarrier.ManagedCarrierClaim{conflict}, lifecycle: ineligible, manageExisting: true, want: carrieradoption.ResultInexactRelation},
		{name: "shadow precedes claim conflict and intent", observation: fixture.correlation(t, observationShadow), claims: []durablecarrier.ManagedCarrierClaim{conflict}, lifecycle: ineligible, manageExisting: true, want: carrieradoption.ResultInexactRelation},
		{name: "managed key drift precedes claim conflict and intent", observation: fixture.correlation(t, observationManagedKeyDrift), claims: []durablecarrier.ManagedCarrierClaim{conflict}, lifecycle: ineligible, manageExisting: true, want: carrieradoption.ResultInexactRelation},
		{name: "ambiguous precedes claim conflict and intent", observation: fixture.correlation(t, observationAmbiguous), claims: []durablecarrier.ManagedCarrierClaim{conflict}, lifecycle: ineligible, manageExisting: true, want: carrieradoption.ResultObservationBlocked},
		{name: "stale precedes claim conflict and intent", observation: fixture.correlation(t, observationStale), claims: []durablecarrier.ManagedCarrierClaim{conflict}, lifecycle: ineligible, manageExisting: true, want: carrieradoption.ResultObservationBlocked},
		{name: "unsupported precedes claim conflict and intent", observation: fixture.correlation(t, observationUnsupported), claims: []durablecarrier.ManagedCarrierClaim{conflict}, lifecycle: ineligible, manageExisting: true, want: carrieradoption.ResultObservationBlocked},
		{name: "unavailable precedes claim conflict and intent", observation: fixture.correlation(t, observationUnavailable), claims: []durablecarrier.ManagedCarrierClaim{conflict}, lifecycle: ineligible, manageExisting: true, want: carrieradoption.ResultObservationBlocked},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			action, err := carrieradoption.NewAction(carrieradoption.ActionInput{
				Locked:         fixture.contract,
				Observation:    test.observation,
				CurrentOwner:   fixture.owner,
				Claims:         test.claims,
				Lifecycle:      test.lifecycle,
				ManageExisting: test.manageExisting,
			})
			if err != nil {
				t.Fatalf("NewAction: %v", err)
			}
			if action.Result() != test.want {
				t.Fatalf("result = %q, want %q", action.Result(), test.want)
			}
			wantBlocked := test.want != carrieradoption.ResultEligibleExactRelation &&
				test.want != carrieradoption.ResultAlreadyClaimedCurrent &&
				test.want != carrieradoption.ResultMissingRelation
			if action.BlocksOrdinaryApply() != wantBlocked {
				t.Fatalf(
					"BlocksOrdinaryApply = %t for %q, want %t",
					action.BlocksOrdinaryApply(),
					test.want,
					wantBlocked,
				)
			}
			if action.StateOnly() != (test.want == carrieradoption.ResultEligibleExactRelation) {
				t.Fatalf("StateOnly = %t for %q", action.StateOnly(), test.want)
			}
			proposed, hasProposed := action.ProposedClaim()
			if hasProposed != test.wantProposal {
				t.Fatalf("proposed claim present = %t, want %t", hasProposed, test.wantProposal)
			}
			if hasProposed && proposed.Provenance() != test.wantProvenance {
				t.Fatalf("proposed provenance = %q, want %q", proposed.Provenance(), test.wantProvenance)
			}
			if current, present := action.CurrentClaim(); present && current.Provenance() != test.wantProvenance {
				t.Fatalf("current provenance = %q, want %q", current.Provenance(), test.wantProvenance)
			}
			if action.InvokesHostRoute() {
				t.Fatal("carrier adoption action invoked a host route")
			}
			if err := action.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestCarrierAdoptionAllowsCompatibleGlobalSharing(t *testing.T) {
	fixture := newAdoptionFixture(t, target.ScopeGlobal, "alpha@official")
	foreignOwner := mustAuthority(t, filepath.Join(t.TempDir(), "foreign"))
	foreign := fixture.claim(t, foreignOwner, durablecarrier.ClaimProvenanceInstalledObserved)

	action, err := carrieradoption.NewAction(carrieradoption.ActionInput{
		Locked:         fixture.contract,
		Observation:    fixture.exact,
		CurrentOwner:   fixture.owner,
		Claims:         []durablecarrier.ManagedCarrierClaim{foreign},
		Lifecycle:      fixture.lifecycle,
		ManageExisting: true,
	})
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	if action.Result() != carrieradoption.ResultEligibleExactRelation {
		t.Fatalf("result = %q, want eligible exact relation", action.Result())
	}
	if action.Occupancy().DaemKnownConsumerCount() != 1 {
		t.Fatalf("occupancy = %d, want one compatible foreign consumer", action.Occupancy().DaemKnownConsumerCount())
	}
	if conflicts := action.ConflictingClaims(); len(conflicts) != 0 {
		t.Fatalf("compatible global claim became conflicts: %#v", conflicts)
	}
}

func TestCarrierAdoptionPlanIdentityCoversIntentLifecycleAndClaimBasis(t *testing.T) {
	fixture := newAdoptionFixture(t, target.ScopeProject, "alpha@official")
	base := fixture.action(t, nil, fixture.lifecycle, false)
	repeated := fixture.action(t, nil, fixture.lifecycle, false)
	explicit := fixture.action(t, nil, fixture.lifecycle, true)
	ineligible := fixture.action(t, nil, fixture.lifecycleWithStoreAvailability(t, false), false)
	installed := fixture.action(
		t,
		[]durablecarrier.ManagedCarrierClaim{
			fixture.claim(t, fixture.owner, durablecarrier.ClaimProvenanceInstalledObserved),
		},
		fixture.lifecycle,
		false,
	)
	adopted := fixture.action(
		t,
		[]durablecarrier.ManagedCarrierClaim{
			fixture.claim(t, fixture.owner, durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved),
		},
		fixture.lifecycle,
		false,
	)
	route := fixture.lifecycle.RemovalRoute()
	changedRoute, err := carrierabsence.NewRouteAdmission(carrierabsence.RouteAdmissionInput{
		Operation:              route.Operation(),
		Request:                route.Request(),
		PreservesSharedCarrier: route.PreservesSharedCarrier(),
		RemovedEffects:         route.RemovedEffects(),
		RetainedEffects:        append(route.RetainedEffects(), "additional_retained_effect"),
		NonClaims:              route.NonClaims(),
	})
	if err != nil {
		t.Fatalf("NewRouteAdmission: %v", err)
	}
	changedLifecycle, err := carrieradoption.NewLifecycle(carrieradoption.LifecycleInput{
		Locked:         fixture.contract,
		InstallRoute:   carrieradoption.InstallRouteAdmitted,
		RemovalRoute:   changedRoute,
		ClaimStore:     carrieradoption.ClaimStoreProjectStatefile,
		StoreAvailable: true,
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	routeDisclosure := fixture.action(t, nil, changedLifecycle, false)
	refusedLifecycle, err := carrieradoption.NewLifecycle(carrieradoption.LifecycleInput{
		Locked:         fixture.contract,
		InstallRoute:   carrieradoption.InstallRouteRefused,
		RemovalRoute:   fixture.lifecycle.RemovalRoute(),
		ClaimStore:     carrieradoption.ClaimStoreProjectStatefile,
		StoreAvailable: true,
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	routeRefusal := fixture.action(t, nil, refusedLifecycle, true)
	if routeRefusal.Result() != carrieradoption.ResultPresentUnclaimedIneligible ||
		routeRefusal.Lifecycle().Blocker() != carrieradoption.BlockInstallRouteNotAdmitted {
		t.Fatalf(
			"route refusal = %q/%q",
			routeRefusal.Result(),
			routeRefusal.Lifecycle().Blocker(),
		)
	}

	if base.PlanIdentity() != repeated.PlanIdentity() {
		t.Fatal("identical decision basis produced unstable plan identity")
	}
	for name, candidate := range map[string]carrieradoption.Action{
		"intent":             explicit,
		"lifecycle":          ineligible,
		"installed claim":    installed,
		"adopted provenance": adopted,
		"route disclosure":   routeDisclosure,
		"route refusal":      routeRefusal,
	} {
		if candidate.PlanIdentity() == base.PlanIdentity() {
			t.Fatalf("%s change did not change plan identity", name)
		}
	}
	if installed.PlanIdentity() == adopted.PlanIdentity() {
		t.Fatal("claim provenance did not change plan identity")
	}

	unrelated := unrelatedClaim(t, fixture.owner)
	withUnrelated := fixture.action(
		t,
		[]durablecarrier.ManagedCarrierClaim{unrelated},
		fixture.lifecycle,
		false,
	)
	if withUnrelated.PlanIdentity() != base.PlanIdentity() {
		t.Fatal("unrelated claim changed carrier adoption plan identity")
	}
}

func TestCarrierAdoptionPlanIdentityIgnoresInventoryEnumerationOrder(t *testing.T) {
	fixture := newAdoptionFixture(t, target.ScopeProject, "alpha@official")
	identity, _, _ := durablecarrier.ManagedCarrierIdentityFromLockedRecord(fixture.contract)
	expected := identity.ExpectedRelation()
	row := func(managedKey string) observerelation.Row {
		value, err := observerelation.NewRow(observerelation.RowSpec{
			SubjectKey:            string(expected.SubjectKey()),
			HasManagedInstanceKey: true,
			ManagedInstanceKey:    managedKey,
		})
		if err != nil {
			t.Fatalf("NewRow: %v", err)
		}
		return value
	}
	exact := row(string(expected.ManagedInstanceKey()))
	shadow := row("sha256:shadow")
	actionForRows := func(rows []observerelation.Row) carrieradoption.Action {
		inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows:         rows,
		})
		if err != nil {
			t.Fatalf("NewInventory: %v", err)
		}
		action, err := carrieradoption.NewAction(carrieradoption.ActionInput{
			Locked:         fixture.contract,
			Observation:    observerelation.Correlate(expected, inventory),
			CurrentOwner:   fixture.owner,
			Lifecycle:      fixture.lifecycle,
			ManageExisting: true,
		})
		if err != nil {
			t.Fatalf("NewAction: %v", err)
		}
		return action
	}

	forward := actionForRows([]observerelation.Row{exact, shadow})
	reversed := actionForRows([]observerelation.Row{shadow, exact})
	if forward.Result() != carrieradoption.ResultInexactRelation ||
		reversed.Result() != carrieradoption.ResultInexactRelation {
		t.Fatalf("results = %q/%q, want inexact", forward.Result(), reversed.Result())
	}
	if forward.PlanIdentity() != reversed.PlanIdentity() {
		t.Fatal("inventory enumeration order changed semantic adoption plan identity")
	}
}

func TestCarrierAdoptionPlanIdentityIncludesStatefileSemanticsWitness(t *testing.T) {
	fixture := newAdoptionFixture(t, target.ScopeProject, "alpha@official")
	base := fixture.action(t, nil, fixture.lifecycle, true)

	changed := fixture
	owner, err := stateauthority.New(
		pathtest.DarwinCaseSensitive(fixture.owner.StatefileKey()),
		fixture.owner.ManifestPath(),
	)
	if err != nil {
		t.Fatal(err)
	}
	changed.owner = owner
	changedAction := changed.action(t, nil, changed.lifecycle, true)
	if base.PlanIdentity() == changedAction.PlanIdentity() {
		t.Fatal("statefile semantics witness did not change adoption plan identity")
	}
}

func TestCarrierAdoptionRejectsMixedCorrelationAndDuplicateClaimBasis(t *testing.T) {
	fixture := newAdoptionFixture(t, target.ScopeProject, "alpha@official")
	other := newAdoptionFixture(t, target.ScopeProject, "beta@official")
	if _, err := carrieradoption.NewAction(carrieradoption.ActionInput{
		Locked:       fixture.contract,
		Observation:  other.exact,
		CurrentOwner: fixture.owner,
		Lifecycle:    fixture.lifecycle,
	}); err == nil {
		t.Fatal("NewAction accepted observation for another locked relation")
	}

	claim := fixture.claim(t, fixture.owner, durablecarrier.ClaimProvenanceInstalledObserved)
	if _, err := carrieradoption.NewAction(carrieradoption.ActionInput{
		Locked:       fixture.contract,
		Observation:  fixture.exact,
		CurrentOwner: fixture.owner,
		Claims:       []durablecarrier.ManagedCarrierClaim{claim, claim},
		Lifecycle:    fixture.lifecycle,
	}); err == nil {
		t.Fatal("NewAction accepted duplicate owner-relation claims")
	}
}

type observationCase string

const (
	observationMissing         observationCase = "missing"
	observationUnkeyed         observationCase = "unkeyed"
	observationShadow          observationCase = "shadow"
	observationManagedKeyDrift observationCase = "managed_key_drift"
	observationAmbiguous       observationCase = "ambiguous"
	observationStale           observationCase = "stale"
	observationUnsupported     observationCase = "unsupported"
	observationUnavailable     observationCase = "unavailable"
)

type adoptionFixture struct {
	contract  lock.LockedSubjectContract
	owner     stateauthority.Authority
	exact     observerelation.CorrelationResult
	lifecycle carrieradoption.Lifecycle
}

func newAdoptionFixture(t *testing.T, scope target.Scope, source string) adoptionFixture {
	t.Helper()
	desired := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "alpha",
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   scope,
		Source:  desiredtest.ExtensionSource(t, desiredextension.SourceKindMarketplace, source),
	})
	file, relation := snapshottest.ExtensionCarrierFile(t, desired)
	contract := file.Locked.Subjects()[0]
	owner := mustAuthority(t, filepath.Join(t.TempDir(), "selected"))
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatalf("DelegatedOperationRequest: %v", err)
	}
	candidate, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		request,
		durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
	)
	if err != nil {
		t.Fatalf("NewManagedCarrierClaim: %v", err)
	}
	removal, err := reconcilehostroute.ResolveCurrentCarrierRemovalRoute(candidate)
	if err != nil {
		t.Fatalf("ResolveCurrentCarrierRemovalRoute: %v", err)
	}
	store := carrieradoption.ClaimStoreProjectStatefile
	if scope == target.ScopeGlobal {
		store = carrieradoption.ClaimStoreGlobalRegistry
	}
	lifecycle, err := carrieradoption.NewLifecycle(carrieradoption.LifecycleInput{
		Locked:         contract,
		InstallRoute:   carrieradoption.InstallRouteAdmitted,
		RemovalRoute:   removal,
		ClaimStore:     store,
		StoreAvailable: true,
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	expected := relation.ExpectedRelation()
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey:            string(expected.SubjectKey()),
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(expected.ManagedInstanceKey()),
	})
	if err != nil {
		t.Fatalf("NewRow: %v", err)
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         []observerelation.Row{row},
	})
	if err != nil {
		t.Fatalf("NewInventory: %v", err)
	}
	return adoptionFixture{
		contract:  contract,
		owner:     owner,
		exact:     observerelation.Correlate(expected, inventory),
		lifecycle: lifecycle,
	}
}

func (fixture adoptionFixture) lifecycleWithStoreAvailability(
	t *testing.T,
	available bool,
) carrieradoption.Lifecycle {
	t.Helper()
	store := carrieradoption.ClaimStoreProjectStatefile
	identity, _, _ := durablecarrier.ManagedCarrierIdentityFromLockedRecord(fixture.contract)
	if identity.Scope() == target.ScopeGlobal {
		store = carrieradoption.ClaimStoreGlobalRegistry
	}
	lifecycle, err := carrieradoption.NewLifecycle(carrieradoption.LifecycleInput{
		Locked:         fixture.contract,
		InstallRoute:   carrieradoption.InstallRouteAdmitted,
		RemovalRoute:   fixture.lifecycle.RemovalRoute(),
		ClaimStore:     store,
		StoreAvailable: available,
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	return lifecycle
}

func (fixture adoptionFixture) claim(
	t *testing.T,
	owner stateauthority.Authority,
	provenance durablecarrier.ClaimProvenance,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(fixture.contract)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(fixture.contract, lock.OperationInstall)
	if err != nil {
		t.Fatalf("DelegatedOperationRequest: %v", err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(owner, identity, request, provenance)
	if err != nil {
		t.Fatalf("NewManagedCarrierClaim: %v", err)
	}
	return claim
}

func (fixture adoptionFixture) action(
	t *testing.T,
	claims []durablecarrier.ManagedCarrierClaim,
	lifecycle carrieradoption.Lifecycle,
	manageExisting bool,
) carrieradoption.Action {
	t.Helper()
	action, err := carrieradoption.NewAction(carrieradoption.ActionInput{
		Locked:         fixture.contract,
		Observation:    fixture.exact,
		CurrentOwner:   fixture.owner,
		Claims:         claims,
		Lifecycle:      lifecycle,
		ManageExisting: manageExisting,
	})
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	return action
}

func (fixture adoptionFixture) correlation(
	t *testing.T,
	testCase observationCase,
) observerelation.CorrelationResult {
	t.Helper()
	identity, _, _ := durablecarrier.ManagedCarrierIdentityFromLockedRecord(fixture.contract)
	expected := identity.ExpectedRelation()
	spec := observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}
	row := func(subject string, managed string, hasManaged bool) observerelation.Row {
		value, err := observerelation.NewRow(observerelation.RowSpec{
			SubjectKey:            subject,
			HasManagedInstanceKey: hasManaged,
			ManagedInstanceKey:    managed,
		})
		if err != nil {
			t.Fatalf("NewRow: %v", err)
		}
		return value
	}
	switch testCase {
	case observationMissing:
	case observationUnkeyed:
		spec.Rows = []observerelation.Row{
			row(string(expected.SubjectKey()), "", false),
		}
	case observationShadow:
		spec.Rows = []observerelation.Row{
			row(string(expected.SubjectKey()), "sha256:wrong", true),
		}
	case observationManagedKeyDrift:
		spec.Rows = []observerelation.Row{
			row("different@official", string(expected.ManagedInstanceKey()), true),
		}
	case observationAmbiguous:
		exact := row(
			string(expected.SubjectKey()),
			string(expected.ManagedInstanceKey()),
			true,
		)
		spec.Rows = []observerelation.Row{exact, exact}
	case observationStale:
		spec.Freshness = observerelation.EvidenceStale
	case observationUnsupported:
		spec.Availability = observerelation.InventoryUnsupported
	case observationUnavailable:
		spec.Availability = observerelation.InventoryUnavailable
	default:
		t.Fatalf("unsupported observation case %q", testCase)
	}
	inventory, err := observerelation.NewInventory(spec)
	if err != nil {
		t.Fatalf("NewInventory: %v", err)
	}
	return observerelation.Correlate(expected, inventory)
}

func conflictingClaim(
	t *testing.T,
	fixture adoptionFixture,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	desired := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "alpha",
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source: desiredtest.ExtensionSource(
			t,
			desiredextension.SourceKindMarketplace,
			"alpha@different",
		),
	})
	file, _ := snapshottest.ExtensionCarrierFile(t, desired)
	other := adoptionFixture{contract: file.Locked.Subjects()[0]}
	return other.claim(t, fixture.owner, durablecarrier.ClaimProvenanceInstalledObserved)
}

func unrelatedClaim(
	t *testing.T,
	owner stateauthority.Authority,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	desired := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "unrelated",
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source: desiredtest.ExtensionSource(
			t,
			desiredextension.SourceKindMarketplace,
			"unrelated@official",
		),
	})
	file, _ := snapshottest.ExtensionCarrierFile(t, desired)
	fixture := adoptionFixture{contract: file.Locked.Subjects()[0]}
	return fixture.claim(t, owner, durablecarrier.ClaimProvenanceInstalledObserved)
}

func mustAuthority(t *testing.T, root string) stateauthority.Authority {
	t.Helper()
	owner, err := stateauthority.New(pathtest.Exact(
		filepath.Join(root, ".daem", "state.json"),
	),

		filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatalf("stateauthority.New: %v", err)
	}
	return owner
}
