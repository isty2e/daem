package apply

import (
	"context"
	"errors"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/desired"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/execute/nativeoperation"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func TestProspectiveCarrierHandoffCannotExecuteBeforeAnyEffect(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	absence := alreadyAbsentWorkflowAction(t, fixture)
	environment := applyCarrierEnvironment(t, "context7", desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@next")
	locked, err := lockbuild.BuildWithOptions(t.Context(), environment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatal(err)
	}
	record := locked.Locked.Subjects()[0]
	action := applyClaudePluginCarrierRelationAction(t, record, snapshottest.DelegatedRelation(t, record), observeclaudeplugin.InventorySpec{
		Availability: relationobserve.InventorySupported, Freshness: relationobserve.EvidenceFresh,
	})
	old, _ := absence.Observation()
	key, err := relationobserve.NewCorrelationKey(action.Subject(), action.ExpectedRelation())
	if err != nil {
		t.Fatal(err)
	}
	current, _ := action.Correlation()
	batch, err := relationobserve.NewBatch(relationobserve.BatchSpec{Correlations: []relationobserve.Correlation{
		old, {Key: key, Result: current},
	}})
	if err != nil {
		t.Fatal(err)
	}
	handoffs, err := reconcile.NewCarrierHandoffs(reconcile.CarrierHandoffsInput{
		CurrentOwner: fixture.claim.Owner(), Claims: []durablecarrier.ManagedCarrierClaim{fixture.claim},
		Absences: []carrierabsence.Action{absence}, Acquisitions: []reconcile.RelationAction{action}, Observations: batch,
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, Relations: []reconcile.RelationAction{action}})
	if err != nil {
		t.Fatal(err)
	}
	expectation, err := reconcile.NewCarrierHandoffExpectation(handoffs, after)
	if err != nil {
		t.Fatal(err)
	}
	prospective := expectation.After()

	for _, native := range []*nativeoperation.NativeApplyInput{nil, {}} {
		assessment := readiness.Assessment{
			CurrentState: fixture.current, GlobalCarrierClaims: fixture.globalClaims, Reconciliation: prospective,
		}
		result, err := runWithOptions(context.Background(), daempaths.Paths{}, desired.Environment{}, lock.File{},
			targetselection.Selection{}, assessment, runOptions{nativeInput: native, markExecutionAttempted: func() {
				t.Fatal("prospective remainder crossed an effect boundary")
			}})
		if !errors.Is(err, errProspectiveCarrierHandoff) || len(result.HostRouteAttempts) != 0 || len(result.DelegateAttempts) != 0 {
			t.Fatal("direct/native execution did not reject the prospective role before effects", err)
		}
	}
	result, err := ExecuteWithOptions(t.Context(), preparedWriteForTestResult(CommandResult{Reconciliation: prospective}), ExecuteOptions{})
	if !errors.Is(err, errProspectiveCarrierHandoff) || result.ExecutionAttempted || len(result.HostRouteAttempts) != 0 || len(result.DelegateAttempts) != 0 {
		t.Fatal("prepared execution did not reject the prospective role before effects", err)
	}
}
