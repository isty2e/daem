package carrierabsence_test

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestActionClassifiesDesiredAndObservationMatrix(t *testing.T) {
	fixture := newFixture(t, target.ScopeProject, "context7", "context7@market")
	tests := []struct {
		name        string
		desired     carrierabsence.DesiredRelationState
		observation observerelation.Correlation
		route       carrierabsence.RouteAdmission
		want        carrierabsence.Decision
	}{
		{
			name:    "retained",
			desired: carrierabsence.DesiredRetained,
			route:   carrierabsence.UnavailableRoute(),
			want:    carrierabsence.DecisionRetain,
		},
		{
			name:    "transition",
			desired: carrierabsence.DesiredTransitionConflict,
			route:   carrierabsence.UnavailableRoute(),
			want:    carrierabsence.DecisionBlockTransition,
		},
		{
			name:        "already absent",
			desired:     carrierabsence.DesiredAbsent,
			observation: fixture.observation(t, observerelation.InventorySupported, observerelation.EvidenceFresh),
			route:       carrierabsence.UnavailableRoute(),
			want:        carrierabsence.DecisionRetireAlreadyAbsent,
		},
		{
			name:        "present without route",
			desired:     carrierabsence.DesiredAbsent,
			observation: fixture.exactObservation(t),
			route:       carrierabsence.UnavailableRoute(),
			want:        carrierabsence.DecisionBlockRoute,
		},
		{
			name:        "present with route",
			desired:     carrierabsence.DesiredAbsent,
			observation: fixture.exactObservation(t),
			route:       admittedRoute(t, false),
			want:        carrierabsence.DecisionRemove,
		},
		{
			name:        "stale",
			desired:     carrierabsence.DesiredAbsent,
			observation: fixture.observation(t, observerelation.InventorySupported, observerelation.EvidenceStale),
			route:       admittedRoute(t, false),
			want:        carrierabsence.DecisionBlockStale,
		},
		{
			name:        "unsupported",
			desired:     carrierabsence.DesiredAbsent,
			observation: fixture.observation(t, observerelation.InventoryUnsupported, observerelation.EvidenceFresh),
			route:       admittedRoute(t, false),
			want:        carrierabsence.DecisionBlockUnobserved,
		},
		{
			name:        "unavailable",
			desired:     carrierabsence.DesiredAbsent,
			observation: fixture.observation(t, observerelation.InventoryUnavailable, observerelation.EvidenceFresh),
			route:       admittedRoute(t, false),
			want:        carrierabsence.DecisionBlockUnobserved,
		},
		{
			name:        "unkeyed same subject",
			desired:     carrierabsence.DesiredAbsent,
			observation: fixture.unmanagedObservation(t),
			route:       admittedRoute(t, false),
			want:        carrierabsence.DecisionBlockAmbiguous,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
				Claim:       fixture.claim,
				Desired:     test.desired,
				Observation: test.observation,
				Occupancy:   fixture.occupancy(t, fixture.claim),
				Route:       test.route,
			})
			if err != nil {
				t.Fatalf("NewAction: %v", err)
			}
			if action.Decision() != test.want {
				t.Fatalf("Decision = %q, want %q", action.Decision(), test.want)
			}
			if err := action.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if action.BlocksOrdinaryApply() != strings.HasPrefix(string(test.want), "block_") {
				t.Fatalf("BlocksOrdinaryApply = %t for %q", action.BlocksOrdinaryApply(), test.want)
			}
			wantConfirmation := test.want == carrierabsence.DecisionRemove ||
				test.want == carrierabsence.DecisionRetireAlreadyAbsent
			if action.RequiresConfirmation() != wantConfirmation {
				t.Fatalf(
					"RequiresConfirmation = %t for %q, want %t",
					action.RequiresConfirmation(),
					test.want,
					wantConfirmation,
				)
			}
		})
	}
}

func TestActionAdmitsOnlyAntigravityBoundedEvidenceWithExactClaim(t *testing.T) {
	antigravity := newCarrierFixture(
		t,
		desiredextension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		desiredextension.SourceKindHostSource,
		"guidance@google",
		"antigravity-cli.plugin-carrier",
		"guidance",
		"guidance",
	)
	pi := newCarrierFixture(
		t,
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeGlobal,
		desiredextension.SourceKindHostSource,
		"npm:@acme/tools",
		"pi.package-carrier",
		"tools",
		"npm:@acme/tools",
	)
	opaqueAntigravity := newCarrierFixture(
		t,
		desiredextension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		desiredextension.SourceKindHostSource,
		"guidance",
		"antigravity-cli.plugin-carrier",
		"guidance",
		"guidance",
	)

	tests := []struct {
		name    string
		fixture fixture
		want    carrierabsence.Decision
	}{
		{
			name:    "Antigravity claim bounds name and bundle evidence",
			fixture: antigravity,
			want:    carrierabsence.DecisionRemove,
		},
		{
			name:    "Antigravity opaque source remains non-exact",
			fixture: opaqueAntigravity,
			want:    carrierabsence.DecisionBlockAmbiguous,
		},
		{
			name:    "Pi equivalent spelling remains non-exact",
			fixture: pi,
			want:    carrierabsence.DecisionBlockAmbiguous,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation := test.fixture.unmanagedObservation(t)
			if got := carrierabsence.ObservationAdmitsRouteResolution(
				test.fixture.claim.Identity(),
				observation.Result,
			); got != (test.want == carrierabsence.DecisionRemove) {
				t.Fatalf("ObservationAdmitsRouteResolution = %t, want %t", got, test.want == carrierabsence.DecisionRemove)
			}
			action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
				Claim:       test.fixture.claim,
				Desired:     carrierabsence.DesiredAbsent,
				Observation: observation,
				Occupancy:   test.fixture.occupancy(t, test.fixture.claim),
				Route:       admittedRoute(t, false),
			})
			if err != nil {
				t.Fatal(err)
			}
			if action.Decision() != test.want {
				t.Fatalf("Decision = %q, want %q", action.Decision(), test.want)
			}
		})
	}
}

func TestActionBlocksSharedCarrierUnlessRoutePreservesIt(t *testing.T) {
	fixture := newFixture(t, target.ScopeGlobal, "context7", "context7@market")
	other := newFixture(t, target.ScopeGlobal, "other", "context7@market")
	occupancy := fixture.occupancy(t, fixture.claim, other.claim)
	for _, test := range []struct {
		name      string
		preserves bool
		want      carrierabsence.Decision
	}{
		{name: "carrier deleting route", want: carrierabsence.DecisionBlockShared},
		{name: "relation only route", preserves: true, want: carrierabsence.DecisionRemove},
	} {
		t.Run(test.name, func(t *testing.T) {
			action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
				Claim:       fixture.claim,
				Desired:     carrierabsence.DesiredAbsent,
				Observation: fixture.exactObservation(t),
				Occupancy:   occupancy,
				Route:       admittedRoute(t, test.preserves),
			})
			if err != nil {
				t.Fatalf("NewAction: %v", err)
			}
			if action.Decision() != test.want {
				t.Fatalf("Decision = %q, want %q", action.Decision(), test.want)
			}
			if len(action.RemainingDaemKnownConsumers()) != 1 {
				t.Fatalf("remaining consumers = %#v", action.RemainingDaemKnownConsumers())
			}
			if !slices.Contains(action.NonClaims(), "ambient_consumers_not_observable") {
				t.Fatal("global action omitted ambient-consumer non-claim")
			}
		})
	}
}

func TestActionSeparatesDirectProjectionFromHostInvocation(t *testing.T) {
	fixture := newFixture(t, target.ScopeProject, "context7", "context7@market")
	route := admittedDirectRoute(t, false)
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:       fixture.claim,
		Desired:     carrierabsence.DesiredAbsent,
		Observation: fixture.exactObservation(t),
		Occupancy:   fixture.occupancy(t, fixture.claim),
		Route:       route,
	})
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	if action.Decision() != carrierabsence.DecisionRemove ||
		action.InvokesHostRoute() ||
		!action.MutatesDirectProjection() ||
		!action.RequiresConfirmation() {
		t.Fatalf(
			"direct action = decision %q host %t direct %t confirmation %t",
			action.Decision(),
			action.InvokesHostRoute(),
			action.MutatesDirectProjection(),
			action.RequiresConfirmation(),
		)
	}
	if request, present := action.HostRouteRequest(); present {
		t.Fatalf("host request = (%#v, %t), want absent", request, present)
	}
	request, present := action.DirectProjectionRequest()
	if !present || !request.Equal(route.Request()) {
		t.Fatalf("direct request = (%#v, %t), want %#v", request, present, route.Request())
	}
}

func TestActionDistinguishesFreshAbsenceFromPendingRemovalSettlement(t *testing.T) {
	fixture := newFixture(t, target.ScopeProject, "context7", "context7@market")
	route := admittedRoute(t, false)
	pending := pendingRemoval(t, fixture.claim, route)
	missing := fixture.observation(
		t,
		observerelation.InventorySupported,
		observerelation.EvidenceFresh,
	)

	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:       fixture.claim,
		Desired:     carrierabsence.DesiredAbsent,
		Observation: missing,
		Occupancy:   fixture.occupancy(t, fixture.claim),
		Route:       carrierabsence.UnavailableRoute(),
		Pending:     &pending,
	})
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	if action.Decision() != carrierabsence.DecisionVerifyPendingRemoval ||
		!action.VerifiesPendingRemoval() ||
		action.StateOnly() ||
		action.InvokesHostRoute() ||
		!action.RetiresClaim() ||
		!action.RequiresConfirmation() {
		t.Fatalf(
			"pending absence = decision %q verify %t state-only %t invokes %t retires %t confirmation %t",
			action.Decision(),
			action.VerifiesPendingRemoval(),
			action.StateOnly(),
			action.InvokesHostRoute(),
			action.RetiresClaim(),
			action.RequiresConfirmation(),
		)
	}
	if request, present := action.HostRouteRequest(); present {
		t.Fatalf("host route = (%#v, %t), want no new invocation", request, present)
	}
	stored, present := action.PendingRemoval()
	if !present || !stored.ExactEqual(pending) {
		t.Fatal("action did not preserve the exact pending removal")
	}
}

func TestActionBlocksDesiredRelationWhileRemovalIsPending(t *testing.T) {
	fixture := newFixture(t, target.ScopeProject, "context7", "context7@market")
	pending := pendingRemoval(t, fixture.claim, admittedRoute(t, false))
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:     fixture.claim,
		Desired:   carrierabsence.DesiredRetained,
		Occupancy: fixture.occupancy(t, fixture.claim),
		Route:     carrierabsence.UnavailableRoute(),
		Pending:   &pending,
	})
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	if action.Decision() != carrierabsence.DecisionBlockPending ||
		!action.BlocksOrdinaryApply() {
		t.Fatalf("action = decision %q blocked %t", action.Decision(), action.BlocksOrdinaryApply())
	}
}

func TestActionReusesOnlyTheExactPendingRemovalRoute(t *testing.T) {
	fixture := newFixture(t, target.ScopeProject, "context7", "context7@market")
	route := admittedRoute(t, false)
	matching := pendingRemoval(t, fixture.claim, route)
	otherRequest, err := realizationdelegate.NewRequest(
		"other.remove",
		"test-removal-v1",
		exactHash("other-remove-request"),
	)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	baselines, err := durablecarrier.NewEffectBaselineSet(nil)
	if err != nil {
		t.Fatalf("NewEffectBaselineSet: %v", err)
	}
	drifted, err := durablecarrier.NewPendingCarrierRemoval(
		fixture.claim,
		otherRequest,
		route.Operation().EffectPostconditions(),
		baselines,
	)
	if err != nil {
		t.Fatalf("NewPendingCarrierRemoval: %v", err)
	}
	for _, test := range []struct {
		name    string
		pending durablecarrier.PendingCarrierRemoval
		want    carrierabsence.Decision
	}{
		{name: "exact route", pending: matching, want: carrierabsence.DecisionRemove},
		{name: "route drift", pending: drifted, want: carrierabsence.DecisionBlockRoute},
	} {
		t.Run(test.name, func(t *testing.T) {
			action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
				Claim:       fixture.claim,
				Desired:     carrierabsence.DesiredAbsent,
				Observation: fixture.exactObservation(t),
				Occupancy:   fixture.occupancy(t, fixture.claim),
				Route:       route,
				Pending:     &test.pending,
			})
			if err != nil {
				t.Fatalf("NewAction: %v", err)
			}
			if action.Decision() != test.want {
				t.Fatalf("Decision = %q, want %q", action.Decision(), test.want)
			}
		})
	}
}

func TestNonAbsenceActionsRejectIrrelevantObservationAndRouteFacts(t *testing.T) {
	fixture := newFixture(t, target.ScopeProject, "context7", "context7@market")
	for _, test := range []struct {
		name        string
		desired     carrierabsence.DesiredRelationState
		observation observerelation.Correlation
		route       carrierabsence.RouteAdmission
	}{
		{
			name:        "retained with observation",
			desired:     carrierabsence.DesiredRetained,
			observation: fixture.exactObservation(t),
			route:       carrierabsence.UnavailableRoute(),
		},
		{
			name:    "transition with route",
			desired: carrierabsence.DesiredTransitionConflict,
			route:   admittedRoute(t, false),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := carrierabsence.NewAction(carrierabsence.ActionInput{
				Claim:       fixture.claim,
				Desired:     test.desired,
				Observation: test.observation,
				Occupancy:   fixture.occupancy(t, fixture.claim),
				Route:       test.route,
			})
			if err == nil || !strings.Contains(err.Error(), "must not consume") {
				t.Fatalf("error = %v, want irrelevant-fact rejection", err)
			}
		})
	}
}

func TestActionRejectsOccupancyWithoutExactCandidate(t *testing.T) {
	fixture := newFixture(t, target.ScopeProject, "context7", "context7@market")
	other := newFixture(t, target.ScopeProject, "other", "context7@market")
	_, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:       fixture.claim,
		Desired:     carrierabsence.DesiredAbsent,
		Observation: fixture.exactObservation(t),
		Occupancy:   fixture.occupancy(t, other.claim),
		Route:       admittedRoute(t, false),
	})
	if err == nil || !strings.Contains(err.Error(), "exact candidate claim") {
		t.Fatalf("error = %v, want exact candidate rejection", err)
	}
}

func TestActionRejectsForgedExactCorrelation(t *testing.T) {
	fixture := newFixture(t, target.ScopeProject, "context7", "context7@market")
	other := newFixture(t, target.ScopeProject, "other", "other@market")
	for _, observation := range []observerelation.Correlation{
		other.exactObservation(t),
		other.observation(t, observerelation.InventorySupported, observerelation.EvidenceFresh),
	} {
		_, err := carrierabsence.NewAction(carrierabsence.ActionInput{
			Claim:       fixture.claim,
			Desired:     carrierabsence.DesiredAbsent,
			Observation: observation,
			Occupancy:   fixture.occupancy(t, fixture.claim),
			Route:       admittedRoute(t, false),
		})
		if err == nil || !strings.Contains(err.Error(), "does not match claim") {
			t.Fatalf("error = %v, want claim mismatch", err)
		}
	}
}

func TestActionDisclosureCollectionsAreImmutable(t *testing.T) {
	fixture := newFixture(t, target.ScopeGlobal, "context7", "context7@market")
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:       fixture.claim,
		Desired:     carrierabsence.DesiredAbsent,
		Observation: fixture.exactObservation(t),
		Occupancy:   fixture.occupancy(t, fixture.claim),
		Route:       admittedRoute(t, false),
	})
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	nonClaims := action.NonClaims()
	nonClaims[0] = "forged"
	if action.NonClaims()[0] == "forged" {
		t.Fatal("NonClaims exposed mutable action state")
	}
	removed := action.RouteAdmission().RemovedEffects()
	removed[0] = "forged"
	if action.RouteAdmission().RemovedEffects()[0] == "forged" {
		t.Fatal("RemovedEffects exposed mutable route state")
	}
}

func TestRouteAdmissionRejectsIncompleteRemovalContracts(t *testing.T) {
	operation := removalOperation(t)
	request := removalRequest(t)
	tests := []struct {
		name   string
		mutate func(*carrierabsence.RouteAdmissionInput)
	}{
		{
			name: "wrong request route",
			mutate: func(input *carrierabsence.RouteAdmissionInput) {
				input.Request, _ = realizationdelegate.NewRequest(
					"other.remove",
					"test-removal-v1",
					exactHash("remove-request"),
				)
			},
		},
		{
			name: "missing removed effects",
			mutate: func(input *carrierabsence.RouteAdmissionInput) {
				input.RemovedEffects = nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := carrierabsence.RouteAdmissionInput{
				Operation:      operation,
				Request:        request,
				RemovedEffects: []string{"managed_relation"},
			}
			test.mutate(&input)
			if _, err := carrierabsence.NewRouteAdmission(input); err == nil {
				t.Fatal("NewRouteAdmission accepted incomplete contract")
			}
		})
	}
}

type fixture struct {
	claim    durablecarrier.ManagedCarrierClaim
	carrier  extensiontopology.Carrier
	expected hostrelation.ExpectedRelation
}

func newFixture(
	t *testing.T,
	scope target.Scope,
	declarationID string,
	sourceRef string,
) fixture {
	return newCarrierFixture(
		t,
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		scope,
		desiredextension.SourceKindMarketplace,
		sourceRef,
		"claude-code.plugin-carrier",
		declarationID,
		sourceRef,
	)
}

func newCarrierFixture(
	t *testing.T,
	carrierFamily desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceKind desiredextension.SourceKind,
	sourceRef string,
	namespace string,
	declarationID string,
	subjectRef string,
) fixture {
	t.Helper()
	source, err := desiredextension.NewSourceRef(sourceKind, sourceRef)
	if err != nil {
		t.Fatalf("NewSourceRef: %v", err)
	}
	key, err := desiredextension.NewCarrierKey(
		carrierFamily,
		selectedTarget,
		scope,
		source,
	)
	if err != nil {
		t.Fatalf("NewCarrierKey: %v", err)
	}
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatalf("NewCarrier: %v", err)
	}
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		namespace,
		declarationID,
	)
	if err != nil {
		t.Fatalf("NewSubjectID: %v", err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(subjectRef)
	if err != nil {
		t.Fatalf("NewSubjectKey: %v", err)
	}
	expected, err := hostrelation.Derive(key, subject, subjectKey)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, expected)
	if err != nil {
		t.Fatalf("NewManagedCarrierIdentity: %v", err)
	}
	root := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(
		filepath.Join(root, ".daem", "state.json"),
	),

		filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatalf("stateauthority.New: %v", err)
	}
	install, err := realizationdelegate.NewRequest(
		"test.install",
		"test-install-v1",
		exactHash("install-"+declarationID),
	)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		install,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatalf("NewManagedCarrierClaim: %v", err)
	}
	return fixture{claim: claim, carrier: carrier, expected: expected}
}

func (fixture fixture) occupancy(
	t *testing.T,
	claims ...durablecarrier.ManagedCarrierClaim,
) durablecarrier.CarrierOccupancy {
	t.Helper()
	occupancy, err := durablecarrier.NewCarrierOccupancy(fixture.carrier, claims)
	if err != nil {
		t.Fatalf("NewCarrierOccupancy: %v", err)
	}
	return occupancy
}

func (fixture fixture) exactObservation(t *testing.T) observerelation.Correlation {
	t.Helper()
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey:            string(fixture.expected.SubjectKey()),
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(fixture.expected.ManagedInstanceKey()),
	})
	if err != nil {
		t.Fatalf("NewRow: %v", err)
	}
	return fixture.observationWithRows(t, observerelation.EvidenceFresh, row)
}

func (fixture fixture) unmanagedObservation(t *testing.T) observerelation.Correlation {
	t.Helper()
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey: string(fixture.expected.SubjectKey()),
	})
	if err != nil {
		t.Fatalf("NewRow: %v", err)
	}
	return fixture.observationWithRows(t, observerelation.EvidenceFresh, row)
}

func (fixture fixture) observation(
	t *testing.T,
	availability observerelation.InventoryAvailability,
	freshness observerelation.EvidenceFreshness,
) observerelation.Correlation {
	t.Helper()
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: availability,
		Freshness:    freshness,
	})
	if err != nil {
		t.Fatalf("NewInventory: %v", err)
	}
	return fixture.keyedObservation(t, observerelation.Correlate(fixture.expected, inventory))
}

func (fixture fixture) observationWithRows(
	t *testing.T,
	freshness observerelation.EvidenceFreshness,
	rows ...observerelation.Row,
) observerelation.Correlation {
	t.Helper()
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    freshness,
		Rows:         rows,
	})
	if err != nil {
		t.Fatalf("NewInventory: %v", err)
	}
	return fixture.keyedObservation(t, observerelation.Correlate(fixture.expected, inventory))
}

func (fixture fixture) keyedObservation(
	t *testing.T,
	result observerelation.CorrelationResult,
) observerelation.Correlation {
	t.Helper()
	key, err := observerelation.NewCorrelationKey(
		fixture.claim.Identity().RelationSubject(),
		fixture.expected,
	)
	if err != nil {
		t.Fatalf("NewCorrelationKey: %v", err)
	}
	return observerelation.Correlation{Key: key, Result: result}
}

func admittedRoute(t *testing.T, preservesSharedCarrier bool) carrierabsence.RouteAdmission {
	t.Helper()
	return admittedRouteForActuation(
		t,
		lock.ActuationDelegatedHostRoute,
		preservesSharedCarrier,
	)
}

func admittedDirectRoute(t *testing.T, preservesSharedCarrier bool) carrierabsence.RouteAdmission {
	t.Helper()
	return admittedRouteForActuation(
		t,
		lock.ActuationDirectProjection,
		preservesSharedCarrier,
	)
}

func admittedRouteForActuation(
	t *testing.T,
	actuation lock.ActuationKind,
	preservesSharedCarrier bool,
) carrierabsence.RouteAdmission {
	t.Helper()
	route, err := carrierabsence.NewRouteAdmission(carrierabsence.RouteAdmissionInput{
		Operation:              removalOperationForActuation(t, actuation),
		Request:                removalRequest(t),
		PreservesSharedCarrier: preservesSharedCarrier,
		RemovedEffects:         []string{"managed_relation", "selected_carrier_artifacts"},
		RetainedEffects:        []string{"external_stores"},
		NonClaims:              []string{"credential_cleanup", "prune"},
	})
	if err != nil {
		t.Fatalf("NewRouteAdmission: %v", err)
	}
	return route
}

func removalOperation(t *testing.T) lock.OperationContract {
	t.Helper()
	return removalOperationForActuation(t, lock.ActuationDelegatedHostRoute)
}

func removalOperationForActuation(
	t *testing.T,
	actuation lock.ActuationKind,
) lock.OperationContract {
	t.Helper()
	operation, err := lock.NewOperationContract(lock.OperationContractInput{
		Operation: lock.OperationRemove,
		Actuation: actuation,
		Authority: lock.AuthorityRemove,
		Route: lock.RouteContractRef{
			RouteID:                "test.remove",
			AdapterContractVersion: "test-removal-v1",
		},
		EffectEnvelope:  lock.EffectEnvelopeComplete,
		Idempotency:     lock.ConditionallyIdempotent,
		Verification:    lock.VerificationHostRelation,
		TrustActivation: lock.TrustActivationNotRequired,
		Recovery:        lock.OperationRecoverySafeRetry,
	})
	if err != nil {
		t.Fatalf("NewOperationContract: %v", err)
	}
	return operation
}

func removalRequest(t *testing.T) realizationdelegate.Request {
	t.Helper()
	request, err := realizationdelegate.NewRequest(
		"test.remove",
		"test-removal-v1",
		exactHash("remove-request"),
	)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return request
}

func pendingRemoval(
	t *testing.T,
	claim durablecarrier.ManagedCarrierClaim,
	route carrierabsence.RouteAdmission,
) durablecarrier.PendingCarrierRemoval {
	t.Helper()
	baselines, err := durablecarrier.NewEffectBaselineSet(nil)
	if err != nil {
		t.Fatalf("NewEffectBaselineSet: %v", err)
	}
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		route.Request(),
		route.Operation().EffectPostconditions(),
		baselines,
	)
	if err != nil {
		t.Fatalf("NewPendingCarrierRemoval: %v", err)
	}
	return pending
}

func exactHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}
