package apply

import (
	"context"
	"errors"
	"strings"
	"testing"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func TestNativeSupportUsesActualExtensionExecution(t *testing.T) {
	input, before := nativeAggregatePlanningInput(t, reconcile.AggregateCreate)
	decisions, err := projectionbuild.BuildAggregateDecisions(input)
	if err != nil {
		t.Fatal(err)
	}
	record, relation := applyClaudePluginCarrierContractWithDeclarationID(t, target.ScopeProject, "context7")
	for _, installed := range []bool{false, true} {
		inventory := observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
		}
		if installed {
			inventory.Rows = []observeclaudeplugin.Row{
				applyClaudePluginCarrierManagedRow(t, "context7@market", string(relation.ExpectedRelation().ManagedInstanceKey())),
			}
		}
		action := applyClaudePluginCarrierActionForSubject(t, record, relation, inventory)
		result, err := reconcile.NewResult(reconcile.ResultInput{
			Context: reconcile.ContextApply, Aggregates: decisions,
			Relations: []reconcile.RelationAction{action},
		})
		if err != nil {
			t.Fatal(err)
		}
		assessment := readiness.Assessment{CurrentState: before, Reconciliation: result}
		err = checkNativeCommandSupport(assessment)
		if installed {
			if action.Execution() != reconcile.ExecutionNoMutation || err != nil {
				t.Fatal("a qualified no-op extension blocked native work", err)
			}
		} else {
			if !action.InvokesHostRoute() || err != nil {
				t.Fatal("supported Claude execution did not reach protected native admission", err)
			}
		}
	}
}

func TestNativeSupportFailureRetainsClosedSafeFacts(t *testing.T) {
	unsupported := nativeSupportError{nativeSupportRoutes}
	for _, cause := range []error{unsupported, errors.Join(unsupported, errors.New("private-unrelated-detail"))} {
		failure := ClassifyFailure(cause, CommandResult{})
		if failure.Reason() != FailureReasonApplyRefused || failure.PrimaryDetail() != unsupported.Error() || failure.Detail() != unsupported.Error() {
			t.Fatal("human and JSON failure projections lost the common support cause")
		}
		if strings.Contains(failure.Detail(), "private-unrelated-detail") {
			t.Fatal("support diagnosis copied unclassified error text")
		}
		if failure.NativeSupportRepairInstruction() == "" || strings.Contains(failure.NativeSupportRepairInstruction(), "private-unrelated-detail") {
			t.Fatal("support guidance was missing or copied unclassified error text")
		}
	}
	if failure := ClassifyFailure(errors.Join(unsupported, context.Canceled), CommandResult{}); failure.Reason() != FailureReasonCancelled {
		t.Fatal("support reason overrode cancellation precedence")
	}
	if failure := ClassifyFailure(errors.Join(unsupported, context.Canceled), CommandResult{}); failure.NativeSupportRepairInstruction() != "" {
		t.Fatal("cancellation gained unrelated native-support repair advice")
	}
}

func TestNativeSupportPreservesAnIndependentlyBlockedRelation(t *testing.T) {
	input, before := nativeAggregatePlanningInput(t, reconcile.AggregateCreate)
	decisions, err := projectionbuild.BuildAggregateDecisions(input)
	if err != nil {
		t.Fatal(err)
	}
	record, relation := applyClaudePluginCarrierContractWithDeclarationID(t, target.ScopeProject, "context7")
	action := applyClaudePluginCarrierActionForSubject(t, record, relation, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceStale,
	})
	if action.Execution() != reconcile.ExecutionBlocked {
		t.Fatal("stale relation fixture did not establish the ordinary blocker")
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context: reconcile.ContextApply, Aggregates: decisions, Relations: []reconcile.RelationAction{action},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = checkNativeCommandSupport(readiness.Assessment{CurrentState: before, Reconciliation: result})
	if !errors.Is(err, ErrRelationActionBlock) || ClassifyFailure(err, CommandResult{}).Reason() != FailureReasonRelationActionBlocked {
		t.Fatal("managed keys masked the independently blocked relation", err)
	}
}
