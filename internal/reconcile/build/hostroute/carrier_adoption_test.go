package hostroute

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
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildCarrierAdoptionActionsCoversEveryCurrentCarrierScopeRow(t *testing.T) {
	tests := []struct {
		name       string
		carrier    desiredextension.Carrier
		target     target.Target
		scope      target.Scope
		sourceKind desiredextension.SourceKind
		sourceRef  string
		exact      bool
		want       carrieradoption.Result
	}{
		{name: "claude project", carrier: desiredextension.CarrierClaudeCodePlugin, target: target.TargetClaudeCode, scope: target.ScopeProject, sourceKind: desiredextension.SourceKindMarketplace, sourceRef: "alpha@official", exact: true, want: carrieradoption.ResultEligibleExactRelation},
		{name: "claude global", carrier: desiredextension.CarrierClaudeCodePlugin, target: target.TargetClaudeCode, scope: target.ScopeGlobal, sourceKind: desiredextension.SourceKindMarketplace, sourceRef: "alpha@official", exact: true, want: carrieradoption.ResultEligibleExactRelation},
		{name: "codex global", carrier: desiredextension.CarrierCodexPlugin, target: target.TargetCodex, scope: target.ScopeGlobal, sourceKind: desiredextension.SourceKindMarketplace, sourceRef: "alpha@official", exact: true, want: carrieradoption.ResultEligibleExactRelation},
		{name: "opencode project", carrier: desiredextension.CarrierOpenCodePlugin, target: target.TargetOpenCode, scope: target.ScopeProject, sourceKind: desiredextension.SourceKindHostSource, sourceRef: "github:acme/opencode-alpha", exact: true, want: carrieradoption.ResultEligibleExactRelation},
		{name: "opencode global", carrier: desiredextension.CarrierOpenCodePlugin, target: target.TargetOpenCode, scope: target.ScopeGlobal, sourceKind: desiredextension.SourceKindHostSource, sourceRef: "github:acme/opencode-alpha", exact: true, want: carrieradoption.ResultEligibleExactRelation},
		{name: "pi project", carrier: desiredextension.CarrierPiPackage, target: target.TargetPi, scope: target.ScopeProject, sourceKind: desiredextension.SourceKindHostSource, sourceRef: "github:acme/pi-alpha", exact: true, want: carrieradoption.ResultEligibleExactRelation},
		{name: "pi global", carrier: desiredextension.CarrierPiPackage, target: target.TargetPi, scope: target.ScopeGlobal, sourceKind: desiredextension.SourceKindHostSource, sourceRef: "github:acme/pi-alpha", exact: true, want: carrieradoption.ResultEligibleExactRelation},
		{name: "antigravity global remains source inexact", carrier: desiredextension.CarrierAntigravityCLIPlugin, target: target.TargetAntigravityCLI, scope: target.ScopeGlobal, sourceKind: desiredextension.SourceKindHostSource, sourceRef: "alpha@official", exact: false, want: carrieradoption.ResultInexactRelation},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := desiredtest.Extension(t, desiredextension.Spec{
				Name:    "alpha",
				Carrier: test.carrier,
				Target:  test.target,
				Scope:   test.scope,
				Source:  desiredtest.ExtensionSource(t, test.sourceKind, test.sourceRef),
			})
			locked, relation := snapshottest.ExtensionCarrierFile(t, value)
			contract := locked.Locked.Subjects()[0]
			observations := adoptionObservationBatch(t, contract, relation.ExpectedRelation(), test.exact)
			owner := adoptionOwner(t)
			selected, err := reconcile.NewSelectedTargets([]target.Target{test.target})
			if err != nil {
				t.Fatalf("NewSelectedTargets: %v", err)
			}

			actions, err := BuildCarrierAdoptionActions(CarrierAdoptionInput{
				Locked:          locked,
				SelectedTargets: selected,
				Observations:    observations,
				CurrentOwner:    owner,
				ManageExisting:  true,
				StoreAvailable:  true,
			})
			if err != nil {
				t.Fatalf("BuildCarrierAdoptionActions: %v", err)
			}
			if len(actions) != 1 || actions[0].Result() != test.want {
				t.Fatalf("actions = %#v, want one %q", actions, test.want)
			}
			if test.exact {
				if !actions[0].Lifecycle().Eligible() {
					t.Fatalf("lifecycle blocker = %q, want eligible", actions[0].Lifecycle().Blocker())
				}
				proposed, present := actions[0].ProposedClaim()
				if !present ||
					proposed.Provenance() != durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved ||
					!proposed.MatchesLockedRecord(contract) {
					t.Fatalf("proposed claim = (%#v, %t), want exact adopted claim", proposed, present)
				}
			} else if _, present := actions[0].ProposedClaim(); present {
				t.Fatal("source-inexact carrier produced a claim")
			}
		})
	}
}

func TestBuildCarrierAdoptionActionsSeparatesIntentAndStoreAdmission(t *testing.T) {
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "alpha",
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source: desiredtest.ExtensionSource(
			t,
			desiredextension.SourceKindMarketplace,
			"alpha@official",
		),
	})
	locked, relation := snapshottest.ExtensionCarrierFile(t, value)
	contract := locked.Locked.Subjects()[0]
	observations := adoptionObservationBatch(t, contract, relation.ExpectedRelation(), true)
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetClaudeCode})
	if err != nil {
		t.Fatal(err)
	}
	base := CarrierAdoptionInput{
		Locked:          locked,
		SelectedTargets: selected,
		Observations:    observations,
		CurrentOwner:    adoptionOwner(t),
		StoreAvailable:  true,
	}
	plain, err := BuildCarrierAdoptionActions(base)
	if err != nil {
		t.Fatal(err)
	}
	if plain[0].Result() != carrieradoption.ResultPresentUnclaimed {
		t.Fatalf("plain result = %q", plain[0].Result())
	}
	plainResult, err := reconcile.NewResult(reconcile.ResultInput{
		Context:          reconcile.ContextDryRun,
		CarrierAdoptions: plain,
	})
	if err != nil {
		t.Fatalf("NewResult(plain): %v", err)
	}
	if !plainResult.HasBlockedCarrierAdoptions() || !plainResult.HasErrors() {
		t.Fatal("unclaimed exact presence did not block ordinary reconciliation")
	}
	base.ManageExisting = true
	base.StoreAvailable = false
	blocked, err := BuildCarrierAdoptionActions(base)
	if err != nil {
		t.Fatal(err)
	}
	if blocked[0].Result() != carrieradoption.ResultPresentUnclaimedIneligible ||
		blocked[0].Lifecycle().Blocker() != carrieradoption.BlockClaimStoreUnavailable {
		t.Fatalf(
			"blocked result = %q blocker = %q",
			blocked[0].Result(),
			blocked[0].Lifecycle().Blocker(),
		)
	}
}

func TestCarrierAdoptionDoesNotUpgradeBoundedEvidenceFromManagedKeyPresence(t *testing.T) {
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "alpha",
		Carrier: desiredextension.CarrierAntigravityCLIPlugin,
		Target:  target.TargetAntigravityCLI,
		Scope:   target.ScopeGlobal,
		Source: desiredtest.ExtensionSource(
			t,
			desiredextension.SourceKindHostSource,
			"alpha@official",
		),
	})
	locked, relation := snapshottest.ExtensionCarrierFile(t, value)
	contract := locked.Locked.Subjects()[0]
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetAntigravityCLI})
	if err != nil {
		t.Fatal(err)
	}
	actions, err := BuildCarrierAdoptionActions(CarrierAdoptionInput{
		Locked:          locked,
		SelectedTargets: selected,
		Observations:    adoptionObservationBatch(t, contract, relation.ExpectedRelation(), true),
		CurrentOwner:    adoptionOwner(t),
		ManageExisting:  true,
		StoreAvailable:  true,
	})
	if err != nil {
		t.Fatalf("BuildCarrierAdoptionActions: %v", err)
	}
	if len(actions) != 1 || actions[0].Result() != carrieradoption.ResultInexactRelation {
		t.Fatalf("actions = %#v, want one inexact relation", actions)
	}
	if _, present := actions[0].ProposedClaim(); present {
		t.Fatal("bounded evidence acquired a managed carrier claim")
	}
}

func TestCarrierAdoptionActionsJoinCanonicalResultWithoutAliasing(t *testing.T) {
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "alpha",
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source:  desiredtest.ExtensionSource(t, desiredextension.SourceKindMarketplace, "alpha@official"),
	})
	locked, relation := snapshottest.ExtensionCarrierFile(t, value)
	contract := locked.Locked.Subjects()[0]
	selected, _ := reconcile.NewSelectedTargets([]target.Target{target.TargetClaudeCode})
	actions, err := BuildCarrierAdoptionActions(CarrierAdoptionInput{
		Locked:          locked,
		SelectedTargets: selected,
		Observations:    adoptionObservationBatch(t, contract, relation.ExpectedRelation(), true),
		CurrentOwner:    adoptionOwner(t),
		ManageExisting:  true,
		StoreAvailable:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:          reconcile.ContextDryRun,
		CarrierAdoptions: actions,
	})
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	actions[0] = carrieradoption.Action{}
	returned := result.CarrierAdoptions()
	if len(returned) != 1 ||
		returned[0].Result() != carrieradoption.ResultEligibleExactRelation ||
		result.DecisionCount() != 1 {
		t.Fatalf("canonical carrier adoptions = %#v count=%d", returned, result.DecisionCount())
	}
	if result.HasBlockedCarrierAdoptions() || result.HasErrors() {
		t.Fatal("eligible state-only adoption blocked reconciliation")
	}
	returned[0] = carrieradoption.Action{}
	if result.CarrierAdoptions()[0].Result() != carrieradoption.ResultEligibleExactRelation {
		t.Fatal("CarrierAdoptions accessor leaked mutable outer storage")
	}
}

func adoptionObservationBatch(
	t *testing.T,
	contract lock.LockedSubjectContract,
	expected hostrelation.ExpectedRelation,
	exact bool,
) observerelation.Batch {
	t.Helper()
	rowSpec := observerelation.RowSpec{SubjectKey: string(expected.SubjectKey())}
	if exact {
		rowSpec.HasManagedInstanceKey = true
		rowSpec.ManagedInstanceKey = string(expected.ManagedInstanceKey())
	}
	row, err := observerelation.NewRow(rowSpec)
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
	key, err := observerelation.NewCorrelationKey(contract.SubjectID(), expected)
	if err != nil {
		t.Fatalf("NewCorrelationKey: %v", err)
	}
	batch, err := observerelation.NewBatch(observerelation.BatchSpec{
		Correlations: []observerelation.Correlation{{
			Key:    key,
			Result: observerelation.Correlate(expected, inventory),
		}},
	})
	if err != nil {
		t.Fatalf("NewBatch: %v", err)
	}
	return batch
}

func adoptionOwner(t *testing.T) stateauthority.Authority {
	t.Helper()
	root := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(
		filepath.Join(root, ".daem", "state.json"),
	),

		filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatalf("stateauthority.New: %v", err)
	}
	return owner
}
