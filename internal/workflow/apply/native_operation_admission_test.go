package apply

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func nativeAggregatePlanningInput(t *testing.T, kind reconcile.AggregateDecisionKind) (projectionbuild.AggregateInput, durable.Snapshot) {
	t.Helper()
	current := `{"theme":1}`
	if kind == reconcile.AggregateNoOp || kind == reconcile.AggregateRecord {
		current = `{"theme":2}`
	}
	observed := nativeActionPreparationInputForScope(t, configdocument.JSON, current, current, `{"theme":2}`, hostconfig.OwnershipKeys, target.ScopeProject).Source
	projections := observed.Projections()
	if len(projections) != 1 {
		t.Fatal("fixture does not have one actual source-produced key")
	}
	item, present, err := projections[0].ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatal(err)
	}
	contract := item.Contribution().Contract()
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{selection.CodecContractID()}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := catalog.Lookup(selection.CodecContractID())
	document := aggregate.ExistingDocument([]byte(current))
	mode := aggregate.DocumentFileMode
	if kind == reconcile.AggregateCreate {
		document = aggregate.AbsentDocument()
		mode = 0
	}
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	evidence, err := observe.NewAggregateEvidence(document, snapshot, mode)
	if err != nil {
		t.Fatal(err)
	}
	var states []durable.ManagedAggregateState
	if kind != reconcile.AggregateCreate && kind != reconcile.AggregateRecord {
		previous, err := aggregate.NewManagedContribution(contract, snapshot.States()[0].CanonicalProjection())
		if err != nil {
			t.Fatal(err)
		}
		state, err := durable.NewManagedAggregateState(item.SubjectID(), previous)
		if err != nil {
			t.Fatal(err)
		}
		states = []durable.ManagedAggregateState{state}
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: states})
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lock.NewLockedSection(append([]lock.LockedSubjectContract{observed.SourceContract()}, projections...), nil)
	if err != nil {
		t.Fatal(err)
	}
	desiredItems := []aggregate.SubjectContribution{item}
	if kind == reconcile.AggregateRemove {
		locked = lock.LockedSection{}
		projections = nil
		desiredItems = nil
	}
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetPi})
	if err != nil {
		t.Fatal(err)
	}
	return projectionbuild.AggregateInput{
		Locked: locked, Expected: projections, Desired: desiredItems, States: states,
		Evidence: []observe.AggregateEvidence{evidence}, SelectedTargets: selected,
		ManageUnmanagedMatches: kind == reconcile.AggregateRecord, Codecs: catalog,
	}, before
}

func TestNativeOperationAdmitsStaticPlanningAndStateButNotOrdinaryExecution(t *testing.T) {
	for _, kind := range []reconcile.AggregateDecisionKind{reconcile.AggregateCreate, reconcile.AggregateReplace, reconcile.AggregateRecord, reconcile.AggregateNoOp, reconcile.AggregateRemove} {
		t.Run(string(kind), func(t *testing.T) {
			input, before := nativeAggregatePlanningInput(t, kind)
			decisions, err := projectionbuild.BuildAggregateDecisions(input)
			expectedDocumentKind := kind
			if kind == reconcile.AggregateRemove {
				expectedDocumentKind = reconcile.AggregateReplace
			}
			if err != nil || len(decisions) != 1 {
				t.Fatal("source/codec facts did not produce one native document decision", err)
			}
			if decisions[0].Kind() != expectedDocumentKind || len(decisions[0].Projections()) != 1 || decisions[0].Projections()[0].Kind() != kind {
				t.Fatalf("native %s lost logical/physical transition separation", kind)
			}
			if kind == reconcile.AggregateRemove && !decisions[0].Rendered().Document().Exists() {
				t.Fatal("selected key removal became whole-document deletion")
			}
			operation, present := decisions[0].NativeOperationContract()
			selection, selectionErr := decisions[0].BeforeSnapshot().Selection()
			if !present || selectionErr != nil || operation.ValidateSelection(selection) != nil || len(operation.RequiredObservations()) == 0 {
				t.Fatal("native decision lost exact static admission", selectionErr)
			}
			result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, Aggregates: decisions})
			if err != nil || !result.ContainsHostConfig() {
				t.Fatal(err)
			}
			withOrders, err := result.WithRelationOrders(nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, copied := range []reconcile.Result{result.Clone(), withOrders} {
				retained, present := copied.Aggregates()[0].NativeOperationContract()
				if !present || retained.ValidateSelection(selection) != nil {
					t.Fatal("Result copy lost native static admission")
				}
			}
			if effects, err := effectpreparation.AggregateEffects(decisions); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) || len(effects) != 0 {
				t.Fatal("default conversion accepted native work, including no-op", err)
			}
			prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), effectpreparation.NativeApplyStateInput{Before: before, Aggregates: decisions, StateCodec: nativePreparationStateCodec(t)})
			if err != nil {
				t.Fatal(err)
			}
			expectedRows := 1
			if kind == reconcile.AggregateRemove {
				expectedRows = 0
			}
			if !prepared.BeforeState().Equal(before) || len(prepared.AfterState().ManagedAggregates()) != expectedRows {
				t.Fatal("positive native preparation lost the actual state transition")
			}
			if expectedRows == 1 {
				after := prepared.AfterState().ManagedAggregates()[0]
				if after.Subject() != input.Desired[0].SubjectID() || !after.Contribution().Equal(input.Desired[0].Contribution()) {
					t.Fatal("native preparation changed source-derived ownership or values")
				}
			}
			for _, effect := range prepared.AggregateEffects() {
				retained, present := effect.NativeOperationContract()
				if !present || retained.ValidateSelection(selection) != nil {
					t.Fatal("Effect conversion lost the selected codec/placement contract")
				}
			}
			if before.ContainsHostConfig() != (kind != reconcile.AggregateCreate && kind != reconcile.AggregateRecord) {
				t.Fatal("native membership query lost before-state meaning")
			}
			for _, guard := range []func(reconcile.Result) error{execute.RejectUnsupportedActions, execute.RejectUnsupportedExecutableActions} {
				if err := guard(result); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
					t.Fatal("ordinary Result admission accepted native work", err)
				}
			}
			root := t.TempDir()
			resolverCalls := 0
			ordinary := execute.ApplyInput{
				AggregateEffects: prepared.AggregateEffects(), CurrentState: before,
				Paths: execute.Paths{StatefilePath: filepath.Join(root, "state.json"), RecoveryDir: filepath.Join(root, "recovery")},
				Resolver: func(output.Destination) (string, error) {
					resolverCalls++
					return "", errors.New("unexpected resolution")
				},
			}
			if _, err := execute.PrepareApplyEffectPlan(ordinary); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
				t.Fatal("native candidate became a usable ordinary structural plan", err)
			}
			if _, err := execute.MaximumForwardEffectValidationCount(ordinary); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
				t.Fatal("native candidate became an ordinary validation schedule", err)
			}
			applied, err := execute.ApplyWithOptions(t.Context(), ordinary, execute.ApplyOptions{})
			if !errors.Is(err, effectpreparation.ErrNativeOperationRequired) || applied.ExecutionAttempted {
				t.Fatal("raw native Effect escaped into ordinary execution", err)
			}
			noWork, err := execute.PrepareApplyEffectPlan(execute.ApplyInput{})
			if err != nil {
				t.Fatal(err)
			}
			applied, err = execute.ApplyWithEffectPlan(t.Context(), ordinary, noWork, execute.ApplyOptions{})
			if !errors.Is(err, effectpreparation.ErrNativeOperationRequired) || applied.ExecutionAttempted {
				t.Fatal("native Effect bypassed refusal through a prepared ordinary plan", err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 || resolverCalls != 0 {
				t.Fatal("ordinary refusal crossed a filesystem boundary", err)
			}
			assessment := readiness.Assessment{Reconciliation: result, CurrentState: before}
			if _, err := applyOperationFingerprint(commandPlan{assessment: assessment}, reconcile.ContextApply); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
				t.Fatal("ordinary fingerprint interpreted native data", err)
			}
			if _, err := runWithOptions(t.Context(), daempaths.Paths{}, desired.Environment{}, lock.File{}, targetselection.Selection{}, assessment, runOptions{}); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
				t.Fatal("native Result reached ordinary workflow execution", err)
			}
		})
	}
}

func TestNativeOperationBeforeStateAloneCannotBecomeOrdinaryNoWork(t *testing.T) {
	_, before := nativeAggregatePlanningInput(t, reconcile.AggregateNoOp)
	input := execute.ApplyInput{CurrentState: before}
	if _, err := execute.PrepareApplyEffectPlan(input); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
		t.Fatal("native before-state became a usable ordinary no-work plan", err)
	}
	if _, err := execute.MaximumForwardEffectValidationCount(input); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
		t.Fatal("native before-state disappeared from ordinary validation planning", err)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply})
	if err != nil {
		t.Fatal(err)
	}
	assessment := readiness.Assessment{CurrentState: before, Reconciliation: result}
	if _, err := applyOperationFingerprint(commandPlan{assessment: assessment}, reconcile.ContextApply); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
		t.Fatal("native before-state disappeared from ordinary fingerprinting", err)
	}
	if _, err := runWithOptions(t.Context(), daempaths.Paths{}, desired.Environment{}, lock.File{}, targetselection.Selection{}, assessment, runOptions{}); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
		t.Fatal("native before-state reached ordinary state authority selection", err)
	}
}

func TestNativeOperationDecisionRejectsMissingOrMismatchedContract(t *testing.T) {
	planning, _ := nativeAggregatePlanningInput(t, reconcile.AggregateRecord)
	decisions, err := projectionbuild.BuildAggregateDecisions(planning)
	if err != nil || len(decisions) != 1 {
		t.Fatal(err)
	}
	decision := decisions[0]
	projection := decision.Projections()[0]
	desired, _ := projection.Desired()
	operation, _ := decision.NativeOperationContract()
	input := reconcile.AggregateDecisionInput{
		Kind: decision.Kind(), Reason: decision.Reason(), DocumentAddress: decision.DocumentAddress(), CodecContractID: decision.CodecContractID(),
		Document: decision.BeforeDocument(), Snapshot: decision.BeforeSnapshot(), CodecPlan: decision.CodecPlan(), Rendered: decision.Rendered(), Evidence: decision.Evidence(), NativeOperation: &operation,
		Projections: []reconcile.AggregateProjectionDecisionInput{{
			Kind: projection.Kind(), Reason: projection.Reason(), Contract: projection.Contract(), Desired: &desired, Before: projection.Before(), Expected: projection.Expected(),
			Subjects: []reconcile.AggregateSubjectDecisionInput{{Subject: projection.Subjects()[0], Contract: projection.Contract(), Kind: reconcile.AggregateRecord, Reason: reconcile.ReasonManagedExisting}},
		}},
	}
	if _, err := reconcile.NewAggregateDecision(input); err != nil {
		t.Fatal("control input is not a usable source-derived native record", err)
	}
	input.NativeOperation = nil
	if _, err := reconcile.NewAggregateDecision(input); err == nil {
		t.Fatal("native decision inferred static admission without a contract")
	}
	key, _ := output.NewKeyPath("other")
	footprint, _ := output.NewKeyFootprint(key)
	other, err := profile.HostConfigProjectionContract(operation.Placement(), footprint)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{other})
	if err != nil {
		t.Fatal(err)
	}
	mismatched, err := profile.NewNativeAggregateOperationContract(planning.Codecs, selection)
	if err != nil {
		t.Fatal(err)
	}
	input.NativeOperation = &mismatched
	if _, err := reconcile.NewAggregateDecision(input); err == nil {
		t.Fatal("native decision accepted another key's static admission")
	}
	planning.Codecs = aggregatecodec.Catalog()
	if result, err := projectionbuild.BuildAggregateDecisions(planning); err == nil || len(result) != 0 {
		t.Fatal("native planning silently fell back to the ordinary codec catalog")
	}
}

func TestNativeOperationWholeNoOpCannotBecomeOrdinaryWork(t *testing.T) {
	facts := nativeActionPreparationInput(t, configdocument.JSON, `{"theme":1}`, `{"theme":1}`, `{"theme":1}`, hostconfig.OwnershipDocument)
	baseline := facts.Baseline
	decision, err := reconcile.NewManagedPathDecision(reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathNoOp, Subject: baseline.Subject(), ConsumerTargets: baseline.ConsumerTargets(), Scope: baseline.Scope(), Destination: baseline.Destination(),
		ContentKind: baseline.ContentKind(), PermissionPolicy: baseline.PermissionPolicy(), DesiredHash: baseline.ContentHash(), LiveHash: baseline.ContentHash(),
		DesiredFileMode: baseline.FileMode(), LiveFileMode: baseline.FileMode(), Previous: &baseline, Reason: reconcile.ReasonAlreadyCurrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, ManagedPaths: []reconcile.ManagedPathDecision{decision}})
	if err != nil || !result.ContainsHostConfig() {
		t.Fatal("native whole Result lost its family identity", err)
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{baseline}})
	if err != nil || !before.ContainsHostConfig() {
		t.Fatal("native whole before-state lost its family identity", err)
	}
	if err := execute.RejectUnsupportedExecutableActions(result); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
		t.Fatal("ordinary Result guard skipped a native whole no-op", err)
	}
	if _, err := execute.PrepareApplyEffectPlan(execute.ApplyInput{CurrentState: before}); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
		t.Fatal("native whole state became an ordinary no-work plan", err)
	}
	if _, err := applyOperationFingerprint(commandPlan{assessment: readiness.Assessment{Reconciliation: result, CurrentState: before}}, reconcile.ContextApply); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
		t.Fatal("ordinary fingerprint skipped a native whole no-op", err)
	}
}
