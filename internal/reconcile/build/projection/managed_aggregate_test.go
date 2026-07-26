package projection

import (
	"os"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/hook"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

func TestAggregatePlannerCreatesAndThenConvergesSharedHookSet(t *testing.T) {
	guard := aggregateHookContract(t, "guard", "echo guard")
	audit := aggregateHookContract(t, "audit", "echo audit")
	locked := aggregateLockedSection(t, guard, audit)
	desired := aggregateItems(t, guard, audit)
	contract := desired[0].Contribution().Contract()

	created, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: locked, Expected: []lock.LockedSubjectContract{guard, audit}, Desired: desired,
		Evidence:        []observe.AggregateEvidence{aggregateEvidence(t, contract, aggregate.AbsentDocument())},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil {
		t.Fatalf("buildAggregateDecisionsForTest(create) returned error: %v", err)
	}
	decision := onlyAggregateDecision(t, created)
	if decision.Kind() != reconcile.AggregateCreate || decision.Reason() != reconcile.ReasonMissingOutput || len(decision.Subjects()) != 2 {
		t.Fatalf("create decision = kind %q reason %q subjects %#v", decision.Kind(), decision.Reason(), decision.Subjects())
	}

	current := decision.Rendered().Document()
	states := aggregateStates(t, desired...)
	converged, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: locked, Expected: []lock.LockedSubjectContract{guard, audit}, Desired: desired,
		States: states, Evidence: []observe.AggregateEvidence{aggregateEvidence(t, contract, current)},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil {
		t.Fatalf("buildAggregateDecisionsForTest(noop) returned error: %v", err)
	}
	if decision := onlyAggregateDecision(t, converged); decision.Kind() != reconcile.AggregateNoOp || decision.Reason() != reconcile.ReasonAlreadyCurrent {
		t.Fatalf("converged decision = kind %q reason %q", decision.Kind(), decision.Reason())
	}
}

func TestAggregatePlannerRemovesOneSubjectBeforeFinalProjectionCleanup(t *testing.T) {
	guard := aggregateHookContract(t, "guard", "echo guard")
	audit := aggregateHookContract(t, "audit", "echo audit")
	beforeItems := aggregateItems(t, guard, audit)
	beforeStates := aggregateStates(t, beforeItems...)
	contract := beforeItems[0].Contribution().Contract()
	current := aggregateDocumentFor(t, contract, beforeItems)

	remainingItems := aggregateItems(t, audit)
	partial, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: aggregateLockedSection(t, audit), Expected: []lock.LockedSubjectContract{audit}, Desired: remainingItems,
		States: beforeStates, Evidence: []observe.AggregateEvidence{aggregateEvidence(t, contract, current)},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil {
		t.Fatalf("buildAggregateDecisionsForTest(partial) returned error: %v", err)
	}
	partialDecision := onlyAggregateDecision(t, partial)
	if partialDecision.Kind() != reconcile.AggregateReplace || len(partialDecision.Subjects()) != 2 {
		t.Fatalf("partial decision = kind %q subjects %#v", partialDecision.Kind(), partialDecision.Subjects())
	}
	if !partialDecision.Rendered().Document().Exists() {
		t.Fatal("partial removal deleted the aggregate document")
	}
	if got := partialDecision.MutatingSubjects(); len(got) != 1 || got[0] != guard.SubjectID() {
		t.Fatalf("partial mutating subjects = %#v, want removed subject %q", got, guard.SubjectID())
	}
	partialPlan := mustReconciliationResult(t, nil, partial)
	partialSubjects := aggregateSubjectDecisionsBySubject(t, partialPlan.Decisions())
	if got := partialSubjects[guard.SubjectID()]; got.Kind() != reconcile.AggregateRemove ||
		got.Reason() != reconcile.ReasonRemovedFromManifest || !got.MutatesState() || !got.MutatesHost() {
		t.Fatalf(
			"removed subject = kind %q reason %q mutates_state %t mutates_host %t",
			got.Kind(), got.Reason(), got.MutatesState(), got.MutatesHost(),
		)
	}
	if got := partialSubjects[audit.SubjectID()]; got.Kind() != reconcile.AggregateNoOp ||
		got.Reason() != reconcile.ReasonAlreadyCurrent || got.MutatesState() || got.MutatesHost() {
		t.Fatalf(
			"unchanged subject = kind %q reason %q mutates_state %t mutates_host %t",
			got.Kind(), got.Reason(), got.MutatesState(), got.MutatesHost(),
		)
	}
	mutating := partialPlan.MutatingDecisions()
	if len(mutating) != 1 {
		t.Fatalf("partial mutating decisions = %d, want 1", len(mutating))
	}
	mutatingAggregate, ok := mutating[0].Aggregate()
	if !ok || mutatingAggregate.Subject() != guard.SubjectID() {
		t.Fatalf("partial mutating decision = %#v, want removed subject %q", mutating[0], guard.SubjectID())
	}

	final, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: aggregateLockedSection(t), States: beforeStates,
		Evidence:        []observe.AggregateEvidence{aggregateEvidence(t, contract, current)},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil {
		t.Fatalf("buildAggregateDecisionsForTest(final) returned error: %v", err)
	}
	finalDecision := onlyAggregateDecision(t, final)
	if finalDecision.Kind() != reconcile.AggregateRemove || finalDecision.Rendered().Document().Exists() {
		t.Fatalf("final decision = kind %q document exists %t", finalDecision.Kind(), finalDecision.Rendered().Document().Exists())
	}
}

func TestAggregatePlannerBlocksUnmanagedOrDriftedHookProjection(t *testing.T) {
	guard := aggregateHookContract(t, "guard", "echo guard")
	desired := aggregateItems(t, guard)
	contract := desired[0].Contribution().Contract()
	current := aggregateDocumentFor(t, contract, desired)
	base := AggregateInput{
		Locked: aggregateLockedSection(t, guard), Expected: []lock.LockedSubjectContract{guard}, Desired: desired,
		Evidence:        []observe.AggregateEvidence{aggregateEvidence(t, contract, current)},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	}

	unmanaged, err := buildAggregateDecisionsForTest(base)
	if err != nil {
		t.Fatal(err)
	}
	if decision := onlyAggregateDecision(t, unmanaged); decision.Kind() != reconcile.AggregateBlocked || decision.Reason() != reconcile.ReasonUnmanagedOutputExists {
		t.Fatalf("unmanaged decision = kind %q reason %q", decision.Kind(), decision.Reason())
	}

	base.ManageUnmanagedMatches = true
	recorded, err := buildAggregateDecisionsForTest(base)
	if err != nil {
		t.Fatal(err)
	}
	if decision := onlyAggregateDecision(t, recorded); decision.Kind() != reconcile.AggregateRecord || decision.Reason() != reconcile.ReasonManagedExisting {
		t.Fatalf("record decision = kind %q reason %q", decision.Kind(), decision.Reason())
	} else if !decision.Rendered().Document().Equal(decision.BeforeDocument()) {
		t.Fatal("record decision rewrites the physical document candidate")
	}

	base.ManageUnmanagedMatches = false
	base.States = aggregateStates(t, desired...)
	driftedDocument := aggregateDocumentFor(t, contract, aggregateItems(t, aggregateHookContract(t, "guard", "echo changed")))
	base.Evidence = []observe.AggregateEvidence{aggregateEvidence(t, contract, driftedDocument)}
	drifted, err := buildAggregateDecisionsForTest(base)
	if err != nil {
		t.Fatal(err)
	}
	if decision := onlyAggregateDecision(t, drifted); decision.Kind() != reconcile.AggregateBlocked || decision.Reason() != reconcile.ReasonDriftedOutput {
		t.Fatalf("drift decision = kind %q reason %q", decision.Kind(), decision.Reason())
	}
}

func TestAggregatePlannerClassifiesFailedUnmanagedObservationByAdoptionPolicy(t *testing.T) {
	guard := aggregateHookContract(t, "guard", "echo guard")
	desired := aggregateItems(t, guard)
	contract := desired[0].Contribution().Contract()
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	codecFailure, err := aggregate.NewCodecFailure(
		aggregate.CodecFailureSelectedShapeUnsupported,
		contract.Address().ContentPath(),
	)
	if err != nil {
		t.Fatal(err)
	}
	failure, err := observe.NewAggregateObservationFailure(
		aggregate.ExistingDocument([]byte(`{"hooks":null}`)),
		selection,
		aggregate.DocumentFileMode,
		codecFailure,
	)
	if err != nil {
		t.Fatal(err)
	}
	base := AggregateInput{
		Locked: aggregateLockedSection(t, guard), Expected: []lock.LockedSubjectContract{guard},
		Desired: desired, ObservationFailures: []observe.AggregateObservationFailure{failure},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	}

	blocked, err := buildAggregateDecisionsForTest(base)
	if err != nil {
		t.Fatal(err)
	}
	if decision := onlyAggregateDecision(t, blocked); decision.Reason() != reconcile.ReasonInvalidDesiredState {
		t.Fatalf("ordinary failed observation reason = %q, want %q", decision.Reason(), reconcile.ReasonInvalidDesiredState)
	}

	base.ManageUnmanagedMatches = true
	blocked, err = buildAggregateDecisionsForTest(base)
	if err != nil {
		t.Fatal(err)
	}
	if decision := onlyAggregateDecision(t, blocked); decision.Reason() != reconcile.ReasonUnmanagedOutputExists {
		t.Fatalf("adoption failed observation reason = %q, want %q", decision.Reason(), reconcile.ReasonUnmanagedOutputExists)
	}
}

func TestAggregatePlannerReplacesAdoptedHookProjectionWithWrongMode(t *testing.T) {
	guard := aggregateHookContract(t, "guard", "echo guard")
	desired := aggregateItems(t, guard)
	contract := desired[0].Contribution().Contract()
	current := aggregateDocumentFor(t, contract, desired)
	evidence := aggregateEvidenceWithMode(t, contract, current, 0o644)

	decisions, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: aggregateLockedSection(t, guard), Expected: []lock.LockedSubjectContract{guard}, Desired: desired,
		Evidence:               []observe.AggregateEvidence{evidence},
		SelectedTargets:        planSelectedTargets(t, target.TargetCodex),
		ManageUnmanagedMatches: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	decision := onlyAggregateDecision(t, decisions)
	if decision.Kind() != reconcile.AggregateReplace || decision.Reason() != reconcile.ReasonFileModeChanged {
		t.Fatalf("wrong-mode adoption = kind %q reason %q, want replace/%q", decision.Kind(), decision.Reason(), reconcile.ReasonFileModeChanged)
	}
}

func TestAggregatePlannerCreatesHookProjectionWithoutClaimingExistingSiblingDocument(t *testing.T) {
	guard := aggregateHookContract(t, "guard", "echo guard")
	desired := aggregateItems(t, guard)
	contract := desired[0].Contribution().Contract()
	current := aggregate.ExistingDocument([]byte("{\n  \"unmanaged\": true\n}\n"))

	decisions, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: aggregateLockedSection(t, guard), Expected: []lock.LockedSubjectContract{guard}, Desired: desired,
		Evidence:        []observe.AggregateEvidence{aggregateEvidence(t, contract, current)},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil {
		t.Fatal(err)
	}
	decision := onlyAggregateDecision(t, decisions)
	if decision.Kind() != reconcile.AggregateReplace || decision.Reason() != reconcile.ReasonContentChanged {
		t.Fatalf("sibling document decision = kind %q reason %q, want replace/%q", decision.Kind(), decision.Reason(), reconcile.ReasonContentChanged)
	}
	subject := aggregateSubjectDecisionsBySubject(
		t,
		mustReconciliationResult(t, nil, decisions).Decisions(),
	)[guard.SubjectID()]
	if subject.Kind() != reconcile.AggregateCreate || subject.Reason() != reconcile.ReasonMissingOutput {
		t.Fatalf("sibling projection decision = kind %q reason %q, want create/%q", subject.Kind(), subject.Reason(), reconcile.ReasonMissingOutput)
	}
	if got := string(decision.Rendered().Document().Content()); !strings.Contains(got, `"unmanaged": true`) {
		t.Fatalf("rendered document = %s, want unmanaged sibling preserved", got)
	}
}

func TestAggregatePlannerIgnoresUnmanagedSiblingAndFormattingDrift(t *testing.T) {
	guard := aggregateHookContract(t, "guard", "echo guard")
	desired := aggregateItems(t, guard)
	contract := desired[0].Contribution().Contract()
	current := aggregate.ExistingDocument([]byte(
		`{"unmanaged":{"changed":true},"hooks":{"Stop":[{"hooks":[{"command":"echo guard","type":"command"}]}]}}` + "\n",
	))
	base := AggregateInput{
		Locked: aggregateLockedSection(t, guard), Expected: []lock.LockedSubjectContract{guard}, Desired: desired,
		Evidence:        []observe.AggregateEvidence{aggregateEvidence(t, contract, current)},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	}

	base.States = aggregateStates(t, desired...)
	decisions, err := buildAggregateDecisionsForTest(base)
	if err != nil {
		t.Fatal(err)
	}
	if decision := onlyAggregateDecision(t, decisions); decision.Kind() != reconcile.AggregateNoOp || decision.Reason() != reconcile.ReasonAlreadyCurrent {
		t.Fatalf("sibling drift decision = kind %q reason %q", decision.Kind(), decision.Reason())
	}

	base.States = nil
	base.ManageUnmanagedMatches = true
	decisions, err = buildAggregateDecisionsForTest(base)
	if err != nil {
		t.Fatal(err)
	}
	if decision := onlyAggregateDecision(t, decisions); decision.Kind() != reconcile.AggregateRecord || decision.Reason() != reconcile.ReasonManagedExisting {
		t.Fatalf("sibling adoption decision = kind %q reason %q", decision.Kind(), decision.Reason())
	}
}

func TestAggregatePlannerRepairsStaleMembershipWithoutRewritingCurrentProjection(t *testing.T) {
	previousContract := aggregateHookContract(t, "guard", "echo guard")
	desiredContract := aggregateHookContract(t, "audit", "echo guard")
	previousItems := aggregateItems(t, previousContract)
	desiredItems := aggregateItems(t, desiredContract)
	contract := desiredItems[0].Contribution().Contract()
	current := aggregateDocumentFor(t, contract, desiredItems)

	decisions, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: aggregateLockedSection(t, desiredContract), Expected: []lock.LockedSubjectContract{desiredContract},
		Desired: desiredItems, States: aggregateStates(t, previousItems...),
		Evidence:        []observe.AggregateEvidence{aggregateEvidence(t, contract, current)},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil {
		t.Fatalf("BuildAggregateDecisions returned error: %v", err)
	}
	decision := onlyAggregateDecision(t, decisions)
	if decision.Kind() != reconcile.AggregateRecord || decision.Reason() != reconcile.ReasonStateStale || decision.MutatesHost() {
		t.Fatalf(
			"stale membership decision = kind %q reason %q mutates_host %t, want record/%q/false",
			decision.Kind(), decision.Reason(), decision.MutatesHost(), reconcile.ReasonStateStale,
		)
	}
	if got := decision.Rendered().Document(); !got.Equal(current) {
		t.Fatalf("record candidate = %#v, want current document preserved", got)
	}
	if got := decision.MutatingSubjects(); len(got) != 2 {
		t.Fatalf("stale membership mutating subjects = %#v, want removed and created subjects", got)
	}
	views := aggregateSubjectDecisionsBySubject(t, mustReconciliationResult(t, nil, decisions).Decisions())
	if got := views[previousContract.SubjectID()]; got.Kind() != reconcile.AggregateRemove ||
		got.Reason() != reconcile.ReasonRemovedFromManifest || !got.MutatesState() || got.MutatesHost() {
		t.Fatalf(
			"stale previous subject = kind %q reason %q mutates_state %t mutates_host %t",
			got.Kind(), got.Reason(), got.MutatesState(), got.MutatesHost(),
		)
	}
	if got := views[desiredContract.SubjectID()]; got.Kind() != reconcile.AggregateCreate ||
		got.Reason() != reconcile.ReasonMissingOutput || !got.MutatesState() || got.MutatesHost() {
		t.Fatalf(
			"stale desired subject = kind %q reason %q mutates_state %t mutates_host %t",
			got.Kind(), got.Reason(), got.MutatesState(), got.MutatesHost(),
		)
	}
}

func TestAggregatePlannerRefreshesStaleContributionWhenLiveAlreadyMatchesDesired(t *testing.T) {
	previous := aggregateMCPContract(t, "context7", "npx", []string{"old"})
	desiredContract := aggregateMCPContract(t, "context7", "npx", []string{"new"})
	desired := aggregateItems(t, desiredContract)
	contract := desired[0].Contribution().Contract()
	current := aggregateDocumentFor(t, contract, desired)

	decisions, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: aggregateLockedSection(t, desiredContract),
		Expected: []lock.LockedSubjectContract{
			desiredContract,
		},
		Desired: desired,
		States:  aggregateStates(t, aggregateItems(t, previous)...),
		Evidence: []observe.AggregateEvidence{
			aggregateEvidence(t, contract, current),
		},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil {
		t.Fatal(err)
	}
	decision := onlyAggregateDecision(t, decisions)
	if decision.Kind() != reconcile.AggregateRecord ||
		decision.Reason() != reconcile.ReasonStateStale ||
		decision.MutatesHost() {
		t.Fatalf(
			"stale contribution decision = kind %q reason %q mutates_host %t, want record/%q/false",
			decision.Kind(),
			decision.Reason(),
			decision.MutatesHost(),
			reconcile.ReasonStateStale,
		)
	}
	if !decision.Rendered().Document().Equal(current) {
		t.Fatal("stale contribution refresh rewrote the current document")
	}
}

func TestAggregatePlannerBatchesMixedMCPProjectionTransitionsByDocument(t *testing.T) {
	alphaBefore := aggregateMCPContract(t, "alpha", "node", []string{"old.js"})
	alphaAfter := aggregateMCPContract(t, "alpha", "node", []string{"new.js"})
	beta := aggregateMCPContract(t, "beta", "node", []string{"beta.js"})
	gamma := aggregateMCPContract(t, "gamma", "node", []string{"gamma.js"})
	adopt := aggregateMCPContract(t, "adopt", "node", []string{"adopt.js"})
	beforeItems := aggregateItems(t, alphaBefore, beta, gamma, adopt)
	contracts := aggregateContractsFromItems(beforeItems)
	current := aggregateDocumentForSelections(t, beforeItems)
	desiredItems := aggregateItems(t, alphaAfter, gamma, adopt)

	decisions, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: aggregateLockedSection(t, alphaAfter, gamma, adopt),
		Expected: []lock.LockedSubjectContract{
			alphaAfter,
			gamma,
			adopt,
		},
		Desired: desiredItems,
		States: aggregateStates(
			t,
			aggregateItems(t, alphaBefore, beta, gamma)...,
		),
		Evidence: []observe.AggregateEvidence{
			aggregateEvidenceForContracts(t, contracts, current),
		},
		SelectedTargets:        planSelectedTargets(t, target.TargetCodex),
		ManageUnmanagedMatches: true,
	})
	if err != nil {
		t.Fatalf("BuildAggregateDecisions returned error: %v", err)
	}
	decision := onlyAggregateDecision(t, decisions)
	if decision.Kind() != reconcile.AggregateReplace {
		t.Fatalf("document decision kind = %q, want replace", decision.Kind())
	}
	if states := decision.BeforeSnapshot().States(); len(states) != 4 {
		t.Fatalf("document decision before states = %d, want 4", len(states))
	}
	if intents := decision.CodecPlan().Intents(); len(intents) != 4 {
		t.Fatalf("document decision intents = %d, want 4", len(intents))
	}

	views := aggregateSubjectDecisionsBySubject(t, mustReconciliationResult(t, nil, decisions).Decisions())
	assertAggregateSubjectDecision(
		t,
		views[alphaAfter.SubjectID()],
		reconcile.AggregateReplace,
		reconcile.ReasonContentChanged,
		true,
		true,
	)
	assertAggregateSubjectDecision(
		t,
		views[beta.SubjectID()],
		reconcile.AggregateRemove,
		reconcile.ReasonRemovedFromManifest,
		true,
		true,
	)
	assertAggregateSubjectDecision(
		t,
		views[gamma.SubjectID()],
		reconcile.AggregateNoOp,
		reconcile.ReasonAlreadyCurrent,
		false,
		false,
	)
	assertAggregateSubjectDecision(
		t,
		views[adopt.SubjectID()],
		reconcile.AggregateRecord,
		reconcile.ReasonManagedExisting,
		true,
		false,
	)
}

func TestAggregatePlannerCanonicalizesMultiProjectionInputOrder(t *testing.T) {
	alpha := aggregateMCPContract(t, "alpha", "node", []string{"alpha.js"})
	beta := aggregateMCPContract(t, "beta", "node", []string{"beta.js"})
	desiredForward := aggregateItems(t, alpha, beta)
	desiredReverse := aggregateItems(t, beta, alpha)
	contracts := aggregateContractsFromItems(desiredForward)
	evidence := aggregateEvidenceForContracts(t, contracts, aggregate.AbsentDocument())

	build := func(expected []lock.LockedSubjectContract, desired []aggregate.SubjectContribution) reconcile.AggregateDecision {
		t.Helper()
		decisions, err := buildAggregateDecisionsForTest(AggregateInput{
			Locked:          aggregateLockedSection(t, expected...),
			Expected:        expected,
			Desired:         desired,
			Evidence:        []observe.AggregateEvidence{evidence},
			SelectedTargets: planSelectedTargets(t, target.TargetCodex),
		})
		if err != nil {
			t.Fatal(err)
		}
		return onlyAggregateDecision(t, decisions)
	}
	forward := build([]lock.LockedSubjectContract{alpha, beta}, desiredForward)
	reverse := build([]lock.LockedSubjectContract{beta, alpha}, desiredReverse)
	if !forward.Rendered().Document().Equal(reverse.Rendered().Document()) ||
		!forward.BeforeSnapshot().Equal(reverse.BeforeSnapshot()) {
		t.Fatal("multi-projection planning depends on input order")
	}
	if len(forward.Subjects()) != 2 || len(reverse.Subjects()) != 2 {
		t.Fatal("multi-projection document decision did not retain both subjects")
	}
}

func assertAggregateSubjectDecision(
	t *testing.T,
	decision reconcile.AggregateSubjectDecision,
	kind reconcile.AggregateDecisionKind,
	reason reconcile.ActionReason,
	mutatesState bool,
	mutatesHost bool,
) {
	t.Helper()
	if decision.Kind() != kind ||
		decision.Reason() != reason ||
		decision.MutatesState() != mutatesState ||
		decision.MutatesHost() != mutatesHost {
		t.Fatalf(
			"subject decision = kind %q reason %q state %t host %t, want %q/%q/%t/%t",
			decision.Kind(),
			decision.Reason(),
			decision.MutatesState(),
			decision.MutatesHost(),
			kind,
			reason,
			mutatesState,
			mutatesHost,
		)
	}
}

func aggregateSubjectDecisionsBySubject(t *testing.T, decisions []reconcile.Decision) map[topology.SubjectID]reconcile.AggregateSubjectDecision {
	t.Helper()
	result := make(map[topology.SubjectID]reconcile.AggregateSubjectDecision, len(decisions))
	for index, decision := range decisions {
		aggregateDecision, ok := decision.Aggregate()
		if !ok {
			t.Fatalf("decision[%d] is not an aggregate subject decision", index)
		}
		if _, duplicate := result[aggregateDecision.Subject()]; duplicate {
			t.Fatalf("duplicate aggregate subject decision %q", aggregateDecision.Subject())
		}
		result[aggregateDecision.Subject()] = aggregateDecision
	}
	return result
}

func TestAggregatePlannerReportsMissingPortableLockSubject(t *testing.T) {
	guard := aggregateHookContract(t, "guard", "echo guard")
	desired := aggregateItems(t, guard)
	decisions, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: aggregateLockedSection(t), Expected: []lock.LockedSubjectContract{guard}, Desired: desired,
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision := onlyAggregateDecision(t, decisions); decision.Kind() != reconcile.AggregateBlocked || decision.Reason() != reconcile.ReasonMissingLock {
		t.Fatalf("missing-lock decision = kind %q reason %q", decision.Kind(), decision.Reason())
	}
}

func TestAggregatePlannerKeepsSharedSubjectsVisibleWhenOneLockIsMissing(t *testing.T) {
	guard := aggregateHookContract(t, "guard", "echo guard")
	audit := aggregateHookContract(t, "audit", "echo audit")
	decisions, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: aggregateLockedSection(t, audit), Expected: []lock.LockedSubjectContract{guard, audit},
		Desired:         aggregateItems(t, guard, audit),
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := mustReconciliationResult(t, nil, decisions)
	views := aggregateSubjectDecisionsBySubject(t, plan.Decisions())
	if len(views) != 2 {
		t.Fatalf("shared lock-readiness decisions = %d, want one per Hook subject", len(views))
	}
	if got := views[guard.SubjectID()]; !got.IsBlocked() || got.Reason() != reconcile.ReasonMissingLock {
		t.Fatalf("missing subject = kind %q reason %q, want missing lock", got.Kind(), got.Reason())
	}
	if got := views[audit.SubjectID()]; !got.IsBlocked() || got.Reason() != reconcile.ReasonAggregateLockBlocked {
		t.Fatalf("shared sibling = kind %q reason %q, want aggregate lock block", got.Kind(), got.Reason())
	}
	if !plan.HasLockReadinessErrors() {
		t.Fatal("shared aggregate lock block did not retain lock-readiness semantics")
	}
}

func aggregateMCPContract(
	t *testing.T,
	serverID string,
	command string,
	args []string,
) lock.LockedSubjectContract {
	t.Helper()
	canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(
		mcpcodec.CodexProjectMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            append([]string(nil), args...),
			AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
		PlacementID:         aggregate.MCPPlacementCodexProject,
		ServerID:            serverID,
		LauncherCommand:     command,
		LauncherArgs:        append([]string(nil), args...),
		CanonicalProjection: string(canonical),
	})
}

func aggregateHookContract(t *testing.T, name string, command string) lock.LockedSubjectContract {
	t.Helper()
	value := desiredtest.Hook(t, hook.Spec{
		Name: name, Event: "Stop", Type: hook.TypeCommand, Command: command,
		Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
	})
	lowered, err := topologyhook.Lower(nil, []hook.Hook{value})
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := refine.HookContributions(
		[]hook.Hook{value},
		lowered,
		hookcodec.CanonicalHookContribution,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts) != 1 {
		t.Fatalf("HookContributions returned %d contracts, want 1", len(contracts))
	}
	return contracts[0]
}

func aggregateContractsFromItems(items []aggregate.SubjectContribution) []aggregate.ProjectionContract {
	contracts := make([]aggregate.ProjectionContract, len(items))
	for index, item := range items {
		contracts[index] = item.Contribution().Contract()
	}
	return contracts
}

func aggregateDocumentForSelections(
	t *testing.T,
	items []aggregate.SubjectContribution,
) aggregate.Document {
	t.Helper()
	contracts := aggregateContractsFromItems(items)
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	states := make([]aggregate.ProjectionState, 0, len(contracts))
	intents := make([]aggregate.ProjectionIntent, 0, len(contracts))
	for _, item := range items {
		state, err := aggregate.NewProjectionState(item.Contribution().Contract(), false, false, "")
		if err != nil {
			t.Fatal(err)
		}
		set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
		if err != nil {
			t.Fatal(err)
		}
		intent, err := aggregate.NewProjectionIntent(state, &set)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
		intents = append(intents, intent)
	}
	snapshot, err := aggregate.NewSnapshot(false, selection, states)
	if err != nil {
		t.Fatal(err)
	}
	codecPlan, err := aggregate.NewPlan(snapshot, intents)
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := aggregatecodec.Catalog().Lookup(selection.CodecContractID())
	if !ok {
		t.Fatal("aggregate codec is missing")
	}
	rendered, failure := codec.Render(aggregate.AbsentDocument(), codecPlan)
	if failure != nil {
		t.Fatal(failure)
	}
	return rendered.Document()
}

func aggregateLockedSection(t *testing.T, contracts ...lock.LockedSubjectContract) lock.LockedSection {
	t.Helper()
	section, err := lock.NewLockedSection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	return section
}

func aggregateItems(t *testing.T, contracts ...lock.LockedSubjectContract) []aggregate.SubjectContribution {
	t.Helper()
	items := make([]aggregate.SubjectContribution, 0, len(contracts))
	for _, contract := range contracts {
		item, ok, err := aggregateContributionFromLocked(contract)
		if err != nil || !ok {
			t.Fatalf("aggregateContributionFromLocked = %#v, %t, %v", item, ok, err)
		}
		items = append(items, item)
	}
	return items
}

func aggregateStates(t *testing.T, items ...aggregate.SubjectContribution) []durable.ManagedAggregateState {
	t.Helper()
	states := make([]durable.ManagedAggregateState, 0, len(items))
	for _, item := range items {
		state, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
	}
	return states
}

func aggregateDocumentFor(
	t *testing.T,
	contract aggregate.ProjectionContract,
	items []aggregate.SubjectContribution,
) aggregate.Document {
	t.Helper()
	set, err := aggregate.NewContributionSet(items)
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := aggregatecodec.Catalog().Lookup(contract.CodecContractID())
	if !ok {
		t.Fatal("aggregate codec is missing")
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	state, err := aggregate.NewProjectionState(contract, false, false, "")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := aggregate.NewSnapshot(false, selection, []aggregate.ProjectionState{state})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := aggregate.NewProjectionIntent(state, &set)
	if err != nil {
		t.Fatal(err)
	}
	codecPlan, err := aggregate.NewPlan(snapshot, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	rendered, failure := codec.Render(aggregate.AbsentDocument(), codecPlan)
	if failure != nil {
		t.Fatal(failure)
	}
	return rendered.Document()
}

func aggregateEvidence(
	t *testing.T,
	contract aggregate.ProjectionContract,
	document aggregate.Document,
) observe.AggregateEvidence {
	return aggregateEvidenceWithMode(t, contract, document, fileModeForAggregateDocument(document))
}

func aggregateEvidenceWithMode(
	t *testing.T,
	contract aggregate.ProjectionContract,
	document aggregate.Document,
	mode os.FileMode,
) observe.AggregateEvidence {
	t.Helper()
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := aggregatecodec.Catalog().Lookup(contract.CodecContractID())
	if !ok {
		t.Fatal("aggregate codec is missing")
	}
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	evidence, err := observe.NewAggregateEvidence(document, snapshot, mode)
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

func aggregateEvidenceForContracts(
	t *testing.T,
	contracts []aggregate.ProjectionContract,
	document aggregate.Document,
) observe.AggregateEvidence {
	t.Helper()
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := aggregatecodec.Catalog().Lookup(selection.CodecContractID())
	if !ok {
		t.Fatal("aggregate codec is missing")
	}
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	evidence, err := observe.NewAggregateEvidence(
		document,
		snapshot,
		fileModeForAggregateDocument(document),
	)
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

func fileModeForAggregateDocument(document aggregate.Document) os.FileMode {
	if document.Exists() {
		return 0o600
	}
	return 0
}

func onlyAggregateDecision(t *testing.T, decisions []reconcile.AggregateDecision) reconcile.AggregateDecision {
	t.Helper()
	if len(decisions) != 1 {
		t.Fatalf("aggregate decisions = %#v, want one", decisions)
	}
	return decisions[0]
}
