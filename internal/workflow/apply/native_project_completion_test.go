package apply

import (
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func nativeProjectCompletionFixture(t *testing.T, kind reconcile.AggregateDecisionKind, fresh bool) (readiness.Assessment, durablecarrier.PendingCarrierInstall) {
	t.Helper()
	input, baseline := nativeAggregatePlanningInput(t, kind)
	decisions, err := projectionbuild.BuildAggregateDecisions(input)
	if err != nil {
		t.Fatal(err)
	}
	record, relation := applyClaudePluginCarrierContractWithDeclarationID(t, target.ScopeProject, "context7")
	freshness := observerelation.EvidenceFresh
	if !fresh {
		freshness = observerelation.EvidenceStale
	}
	inventory, err := observeclaudeplugin.NewInventory(observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported, Freshness: freshness,
		Rows: []observeclaudeplugin.Row{applyClaudePluginCarrierManagedRow(t, "context7@market", string(relation.ExpectedRelation().ManagedInstanceKey()))},
	})
	if err != nil {
		t.Fatal(err)
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
	if err != nil || !admitted {
		t.Fatal("fixture lacks admitted project carrier identity", err)
	}
	admission, err := applyClaudePluginCarrierAdmission()
	if err != nil {
		t.Fatal(err)
	}
	action, err := reconcile.NewRelationAction(reconcile.RelationActionInput{
		CarrierIdentity: identity, RouteRequest: relation.RouteRequest(),
		Correlation: observeclaudeplugin.Correlate(relation, inventory), RouteAdmission: admission, PendingInstallPresent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	owner, err := stateauthority.New(mustObservedPathAuthority(t, filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, relation.RouteRequest())
	if err != nil {
		t.Fatal(err)
	}
	before, err := baseline.WithPendingCarrierInstalls([]durablecarrier.PendingCarrierInstall{pending})
	if err != nil {
		t.Fatal(err)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, Aggregates: decisions, Relations: []reconcile.RelationAction{action}})
	if err != nil {
		t.Fatal(err)
	}
	return readiness.Assessment{CurrentState: before, Reconciliation: result}, pending
}

func TestNativePreparationCompletesExactPendingProjectInstall(t *testing.T) {
	for _, kind := range []reconcile.AggregateDecisionKind{reconcile.AggregateCreate, reconcile.AggregateNoOp} {
		t.Run(string(kind), func(t *testing.T) {
			assessment, pending := nativeProjectCompletionFixture(t, kind, true)
			planned := commandPlan{assessment: assessment}
			if err := admitNativeCommandPlan(t.Context(), &planned); err != nil {
				t.Fatal(err)
			}
			after := planned.nativePlan.state.AfterState()
			if len(after.ManagedAggregates()) != 1 || len(after.PendingCarrierInstalls()) != 0 || len(after.ManagedCarrierClaims()) != 1 {
				t.Fatalf("native successor lost exact completion: keys=%d pending=%d claims=%d", len(after.ManagedAggregates()), len(after.PendingCarrierInstalls()), len(after.ManagedCarrierClaims()))
			}
			claim := after.ManagedCarrierClaims()[0]
			if !claim.Owner().ExactEqual(pending.Owner()) || !claim.Identity().ExactEqual(pending.Identity()) || !claim.InstallRequest().Equal(pending.InstallRequest()) || claim.Provenance() != durablecarrier.ClaimProvenanceInstalledObserved {
				t.Fatal("native completion changed exact acquisition provenance")
			}
			completions := planned.nativePlan.state.ProjectCarrierCompletions()
			if len(completions) != 1 || !completions[0].ExactEqual(claim) {
				t.Fatal("native preparation lost its declared journal completion")
			}
			completions[0] = durablecarrier.ManagedCarrierClaim{}
			if !planned.nativePlan.state.ProjectCarrierCompletions()[0].ExactEqual(claim) {
				t.Fatal("caller mutated the prepared completion selection")
			}
			content, err := planned.nativePlan.stateCodec.Encode(after)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := planned.nativePlan.stateCodec.Decode(content)
			if err != nil || !decoded.Equal(after) {
				t.Fatal("native codec did not preserve the complete successor", err)
			}
		})
	}
}

func TestNativePreparationCannotCompleteFromPendingHistoryAlone(t *testing.T) {
	for _, scenario := range []string{"no relation", "no pending", "different request", "different identity", "stale evidence"} {
		t.Run(scenario, func(t *testing.T) {
			assessment, pending := nativeProjectCompletionFixture(t, reconcile.AggregateNoOp, scenario != "stale evidence")
			var err error
			switch scenario {
			case "no relation":
				assessment.Reconciliation, err = reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, Aggregates: assessment.Reconciliation.Aggregates()})
			case "no pending":
				assessment.CurrentState, err = assessment.CurrentState.WithPendingCarrierInstalls(nil)
			case "different request":
				var request realizationdelegate.Request
				request, err = realizationdelegate.NewRequest(pending.InstallRequest().RouteID(), pending.InstallRequest().ContractVersion(), "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
				if err == nil {
					pending, err = durablecarrier.NewPendingCarrierInstall(pending.Owner(), pending.Identity(), request)
				}
			case "different identity":
				record, relation := applyClaudePluginCarrierContractWithDeclarationID(t, target.ScopeProject, "other")
				identity, _, identityErr := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
				if identityErr != nil {
					t.Fatal(identityErr)
				}
				pending, err = durablecarrier.NewPendingCarrierInstall(pending.Owner(), identity, relation.RouteRequest())
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "different request" || scenario == "different identity" {
				assessment.CurrentState, err = assessment.CurrentState.WithPendingCarrierInstalls([]durablecarrier.PendingCarrierInstall{pending})
				if err != nil {
					t.Fatal(err)
				}
			}
			planned := commandPlan{assessment: assessment}
			err = admitNativeCommandPlan(t.Context(), &planned)
			if scenario == "stale evidence" {
				if err == nil || planned.nativePlan != nil {
					t.Fatal("stale evidence admitted a usable native operation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !planned.nativePlan.state.AfterState().Equal(assessment.CurrentState) {
				t.Fatal("unmatched pending history changed project claims or state")
			}
		})
	}
}
