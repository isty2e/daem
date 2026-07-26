package hostroute

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/realization"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestBuildCarrierAbsenceActionsSeparatesCandidateAuthorityFromOccupancy(t *testing.T) {
	previous, relation := statusClaudePluginExtensionLockfileWithScope(
		t,
		"context7",
		"context7@market",
		target.ScopeGlobal,
	)
	current := retainedCarrierFixtureFor(t, previous)
	foreign := retainedCarrierFixtureFor(t, previous)
	observations := exactCarrierAbsenceBatch(t, previous, relation)

	actions, err := BuildCarrierAbsenceActions(CarrierAbsenceInput{
		Locked:          lock.File{Version: lock.CurrentVersion},
		SelectedTargets: statusClaudeSelection(t, "claude-code"),
		Observations:    observations,
		CurrentOwner:    current.claim.Owner(),
		AllClaims:       []durablecarrier.ManagedCarrierClaim{foreign.claim, current.claim},
		ResolveRoute: func(durablecarrier.ManagedCarrierClaim) (carrierabsence.RouteAdmission, error) {
			return admittedCarrierRemovalRoute(t, false), nil
		},
	})
	if err != nil {
		t.Fatalf("BuildCarrierAbsenceActions: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions = %#v, want only the current authority candidate", actions)
	}
	action := actions[0]
	if action.Claim().Owner().StatefileKey() != current.claim.Owner().StatefileKey() ||
		action.Decision() != carrierabsence.DecisionBlockShared ||
		len(action.RemainingDaemKnownConsumers()) != 1 {
		t.Fatalf("action = %#v, want current candidate blocked by foreign occupancy", action)
	}
}

func TestBuildCarrierAbsenceActionsClassifiesDesiredStateBeforeObservationOrRoute(t *testing.T) {
	previous, _ := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	fixture := retainedCarrierFixtureFor(t, previous)
	replacement, _ := statusClaudePluginExtensionLockfile(t, "context7", "replacement@market")
	tests := []struct {
		name   string
		locked lock.File
		want   carrierabsence.Decision
	}{
		{name: "retained", locked: previous, want: carrierabsence.DecisionRetain},
		{name: "transition", locked: replacement, want: carrierabsence.DecisionBlockTransition},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolverCalls := 0
			actions, err := BuildCarrierAbsenceActions(CarrierAbsenceInput{
				Locked:          test.locked,
				SelectedTargets: statusClaudeSelection(t, "claude-code"),
				CurrentOwner:    fixture.claim.Owner(),
				AllClaims:       []durablecarrier.ManagedCarrierClaim{fixture.claim},
				ResolveRoute: func(durablecarrier.ManagedCarrierClaim) (carrierabsence.RouteAdmission, error) {
					resolverCalls++
					return admittedCarrierRemovalRoute(t, false), nil
				},
			})
			if err != nil {
				t.Fatalf("BuildCarrierAbsenceActions: %v", err)
			}
			if len(actions) != 1 || actions[0].Decision() != test.want {
				t.Fatalf("actions = %#v, want %q", actions, test.want)
			}
			if resolverCalls != 0 {
				t.Fatalf("route resolver called %d times for %q", resolverCalls, test.want)
			}
			if _, present := actions[0].Observation(); present {
				t.Fatalf("%q action consumed absence observation", test.want)
			}
		})
	}
}

func TestBuildCarrierAbsenceActionsRetiresFreshExactAbsenceWithoutRoute(t *testing.T) {
	previous, relation := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	fixture := retainedCarrierFixtureFor(t, previous)
	inventory := mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	observations := mustStatusClaudeObservationBatch(
		t,
		previous.Locked.Subjects()[0].SubjectID(),
		relation,
		inventory,
	)
	resolverCalls := 0
	actions, err := BuildCarrierAbsenceActions(CarrierAbsenceInput{
		Locked:          lock.File{Version: lock.CurrentVersion},
		SelectedTargets: statusClaudeSelection(t, "claude-code"),
		Observations:    observations,
		CurrentOwner:    fixture.claim.Owner(),
		AllClaims:       []durablecarrier.ManagedCarrierClaim{fixture.claim},
		ResolveRoute: func(durablecarrier.ManagedCarrierClaim) (carrierabsence.RouteAdmission, error) {
			resolverCalls++
			return admittedCarrierRemovalRoute(t, false), nil
		},
	})
	if err != nil {
		t.Fatalf("BuildCarrierAbsenceActions: %v", err)
	}
	if len(actions) != 1 ||
		actions[0].Decision() != carrierabsence.DecisionRetireAlreadyAbsent ||
		!actions[0].StateOnly() ||
		!actions[0].RetiresClaim() {
		t.Fatalf("actions = %#v, want state-only retirement", actions)
	}
	if resolverCalls != 0 {
		t.Fatalf("route resolver called %d times for already-absent action", resolverCalls)
	}
}

func TestBuildCarrierAbsenceActionsSettlesPendingRemovalWithoutRouteRetry(t *testing.T) {
	previous, relation := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	fixture := retainedCarrierFixtureFor(t, previous)
	pending := pendingCarrierRemovalFor(t, fixture.claim)
	inventory := mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	observations := mustStatusClaudeObservationBatch(
		t,
		previous.Locked.Subjects()[0].SubjectID(),
		relation,
		inventory,
	)
	resolverCalls := 0
	actions, err := BuildCarrierAbsenceActions(CarrierAbsenceInput{
		Locked:          lock.File{Version: lock.CurrentVersion},
		SelectedTargets: statusClaudeSelection(t, "claude-code"),
		Observations:    observations,
		CurrentOwner:    fixture.claim.Owner(),
		AllClaims:       []durablecarrier.ManagedCarrierClaim{fixture.claim},
		PendingRemovals: []durablecarrier.PendingCarrierRemoval{pending},
		ResolveRoute: func(durablecarrier.ManagedCarrierClaim) (carrierabsence.RouteAdmission, error) {
			resolverCalls++
			return admittedCarrierRemovalRoute(t, false), nil
		},
	})
	if err != nil {
		t.Fatalf("BuildCarrierAbsenceActions: %v", err)
	}
	if len(actions) != 1 ||
		actions[0].Decision() != carrierabsence.DecisionVerifyPendingRemoval ||
		!actions[0].VerifiesPendingRemoval() ||
		actions[0].StateOnly() {
		t.Fatalf("actions = %#v, want pending-removal verification", actions)
	}
	if resolverCalls != 0 {
		t.Fatalf("route resolver called %d times for observation-only settlement", resolverCalls)
	}
}

func TestBuildCarrierAbsenceActionsBlocksRetainedPendingRemoval(t *testing.T) {
	previous, _ := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	fixture := retainedCarrierFixtureFor(t, previous)
	actions, err := BuildCarrierAbsenceActions(CarrierAbsenceInput{
		Locked:          previous,
		SelectedTargets: statusClaudeSelection(t, "claude-code"),
		CurrentOwner:    fixture.claim.Owner(),
		AllClaims:       []durablecarrier.ManagedCarrierClaim{fixture.claim},
		PendingRemovals: []durablecarrier.PendingCarrierRemoval{
			pendingCarrierRemovalFor(t, fixture.claim),
		},
	})
	if err != nil {
		t.Fatalf("BuildCarrierAbsenceActions: %v", err)
	}
	if len(actions) != 1 ||
		actions[0].Decision() != carrierabsence.DecisionBlockPending ||
		!actions[0].BlocksOrdinaryApply() {
		t.Fatalf("actions = %#v, want retained-pending block", actions)
	}
}

func TestBuildCarrierAbsenceActionsRejectsInvalidPendingRemovalSet(t *testing.T) {
	previous, _ := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	fixture := retainedCarrierFixtureFor(t, previous)
	other := retainedCarrierFixtureFor(t, previous)
	pending := pendingCarrierRemovalFor(t, fixture.claim)
	for _, test := range []struct {
		name         string
		currentOwner durablecarrier.StateAuthority
		claims       []durablecarrier.ManagedCarrierClaim
		pending      []durablecarrier.PendingCarrierRemoval
		want         string
	}{
		{
			name:         "orphan",
			currentOwner: fixture.claim.Owner(),
			pending:      []durablecarrier.PendingCarrierRemoval{pending},
			want:         "no exact active claim",
		},
		{
			name:         "foreign owner",
			currentOwner: other.claim.Owner(),
			claims:       []durablecarrier.ManagedCarrierClaim{fixture.claim, other.claim},
			pending:      []durablecarrier.PendingCarrierRemoval{pending},
			want:         "foreign state authority",
		},
		{
			name:         "duplicate",
			currentOwner: fixture.claim.Owner(),
			claims:       []durablecarrier.ManagedCarrierClaim{fixture.claim},
			pending:      []durablecarrier.PendingCarrierRemoval{pending, pending},
			want:         "duplicates one owner relation",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildCarrierAbsenceActions(CarrierAbsenceInput{
				Locked:          lock.File{Version: lock.CurrentVersion},
				SelectedTargets: statusClaudeSelection(t, "claude-code"),
				CurrentOwner:    test.currentOwner,
				AllClaims:       test.claims,
				PendingRemovals: test.pending,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBuildCarrierAbsenceActionsDefaultsToHonestRouteBlock(t *testing.T) {
	previous, relation := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	fixture := retainedCarrierFixtureFor(t, previous)
	actions, err := BuildCarrierAbsenceActions(CarrierAbsenceInput{
		Locked:          lock.File{Version: lock.CurrentVersion},
		SelectedTargets: statusClaudeSelection(t, "claude-code"),
		Observations:    exactCarrierAbsenceBatch(t, previous, relation),
		CurrentOwner:    fixture.claim.Owner(),
		AllClaims:       []durablecarrier.ManagedCarrierClaim{fixture.claim},
	})
	if err != nil {
		t.Fatalf("BuildCarrierAbsenceActions: %v", err)
	}
	if len(actions) != 1 || actions[0].Decision() != carrierabsence.DecisionBlockRoute {
		t.Fatalf("actions = %#v, want route block", actions)
	}
}

func TestBuildCarrierAbsenceActionsValidatesAllClaimsBeforeFiltering(t *testing.T) {
	previous, _ := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	fixture := retainedCarrierFixtureFor(t, previous)
	_, err := BuildCarrierAbsenceActions(CarrierAbsenceInput{
		Locked:          lock.File{Version: lock.CurrentVersion},
		SelectedTargets: statusClaudeSelection(t, "codex"),
		CurrentOwner:    fixture.claim.Owner(),
		AllClaims:       []durablecarrier.ManagedCarrierClaim{{}},
	})
	if err == nil || !strings.Contains(err.Error(), "carrier absence claim[0]") {
		t.Fatalf("error = %v, want invalid claim rejection", err)
	}
}

type retainedCarrierFixture struct {
	claim durablecarrier.ManagedCarrierClaim
}

func retainedCarrierFixtureFor(t *testing.T, locked lock.File) retainedCarrierFixture {
	t.Helper()
	contract := locked.Locked.Subjects()[0]
	realizationSpec, ok := contract.Realization()
	if !ok {
		t.Fatal("carrier fixture has no realization")
	}
	relation, ok := realizationSpec.DelegatedRelation()
	if !ok {
		t.Fatal("carrier fixture has no delegated relation")
	}
	carrierKey, admitted, err := lock.DelegatedRelationCarrierKey(contract)
	if err != nil || !admitted {
		t.Fatalf("DelegatedRelationCarrierKey = (%#v, %t, %v)", carrierKey, admitted, err)
	}
	carrier, err := extensiontopology.NewCarrier(carrierKey)
	if err != nil {
		t.Fatalf("NewCarrier: %v", err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(
		carrier,
		contract.SubjectID(),
		relation.ExpectedRelation(),
	)
	if err != nil {
		t.Fatalf("NewManagedCarrierIdentity: %v", err)
	}
	root := t.TempDir()
	owner, err := durablecarrier.NewStateAuthority(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatalf("NewStateAuthority: %v", err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatalf("DelegatedOperationRequest: %v", err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		request,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatalf("NewManagedCarrierClaim: %v", err)
	}
	return retainedCarrierFixture{claim: claim}
}

func exactCarrierAbsenceBatch(
	t *testing.T,
	previous lock.File,
	relation realization.DelegatedRelation,
) observerelation.Batch {
	t.Helper()
	expected := relation.ExpectedRelation()
	hostScope := observeclaudeplugin.HostScopeProject
	if relation.Scope() == target.ScopeGlobal {
		hostScope = observeclaudeplugin.HostScopeUser
	}
	row := mustStatusManagedRowWithHostScope(
		t,
		string(expected.SubjectKey()),
		string(expected.ManagedInstanceKey()),
		hostScope,
	)
	inventory := mustStatusClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         []observeclaudeplugin.Row{row},
	})
	return mustStatusClaudeObservationBatch(
		t,
		previous.Locked.Subjects()[0].SubjectID(),
		relation,
		inventory,
	)
}

func admittedCarrierRemovalRoute(
	t *testing.T,
	preservesSharedCarrier bool,
) carrierabsence.RouteAdmission {
	t.Helper()
	operation, err := lock.NewOperationContract(lock.OperationContractInput{
		Operation: lock.OperationRemove,
		Actuation: lock.ActuationDelegatedHostRoute,
		Authority: lock.AuthorityRemove,
		Route: lock.RouteContractRef{
			RouteID:                "test.remove",
			AdapterContractVersion: "test-remove-v1",
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
	digest := sha256.Sum256([]byte("test remove"))
	request, err := realizationdelegate.NewRequest(
		"test.remove",
		"test-remove-v1",
		"sha256:"+hex.EncodeToString(digest[:]),
	)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	admission, err := carrierabsence.NewRouteAdmission(carrierabsence.RouteAdmissionInput{
		Operation:              operation,
		Request:                request,
		PreservesSharedCarrier: preservesSharedCarrier,
		RemovedEffects:         []string{"managed_relation"},
		RetainedEffects:        []string{"external_stores"},
		NonClaims:              []string{"prune"},
	})
	if err != nil {
		t.Fatalf("NewRouteAdmission: %v", err)
	}
	return admission
}

func pendingCarrierRemovalFor(
	t *testing.T,
	claim durablecarrier.ManagedCarrierClaim,
) durablecarrier.PendingCarrierRemoval {
	t.Helper()
	route := admittedCarrierRemovalRoute(t, false)
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
