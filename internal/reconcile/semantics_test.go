package reconcile

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestActionReasonOwnershipBlockClassification(t *testing.T) {
	for _, reason := range []ActionReason{
		ReasonOwnershipObservationMissing,
		ReasonOwnershipClaimMissing,
		ReasonOwnershipConflict,
		ReasonOwnershipReserved,
		ReasonOwnershipStateConflict,
	} {
		if !reason.IsOwnershipBlock() {
			t.Fatalf("reason %q is not classified as an ownership block", reason)
		}
	}
	if ReasonUnmanagedOutputExists.IsOwnershipBlock() {
		t.Fatal("unmanaged output reason was classified as an ownership block")
	}
}

func TestManagedPathDecisionKindClassification(t *testing.T) {
	tests := []struct {
		kind         ManagedPathDecisionKind
		actionKind   ActionKind
		mutatesHost  bool
		mutatesState bool
	}{
		{kind: ManagedPathCreate, actionKind: ActionKindCreate, mutatesHost: true, mutatesState: true},
		{kind: ManagedPathReplace, actionKind: ActionKindUpdate, mutatesHost: true, mutatesState: true},
		{kind: ManagedPathRemove, actionKind: ActionKindDelete, mutatesHost: true, mutatesState: true},
		{kind: ManagedPathRecord, actionKind: ActionKindRecord, mutatesState: true},
		{kind: ManagedPathNoOp, actionKind: ActionKindNoOp},
		{kind: ManagedPathBlocked, actionKind: ActionKindError},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			if got := test.kind.actionKind(); got != test.actionKind {
				t.Fatalf("action kind = %q, want %q", got, test.actionKind)
			}
			if got := test.kind.MutatesHost(); got != test.mutatesHost {
				t.Fatalf("MutatesHost() = %t, want %t", got, test.mutatesHost)
			}
			if got := test.kind.MutatesState(); got != test.mutatesState {
				t.Fatalf("MutatesState() = %t, want %t", got, test.mutatesState)
			}
		})
	}
}

func TestManagedPathDecisionDelegatesMutationClassificationToKind(t *testing.T) {
	facts := managedPathDecisionFacts{}
	tests := []struct {
		name         string
		decision     ManagedPathDecision
		mutatesHost  bool
		mutatesState bool
	}{
		{
			name: "create", decision: newManagedPathCreate(facts, ReasonMissingOutput),
			mutatesHost: true, mutatesState: true,
		},
		{
			name: "record", decision: newManagedPathRecord(facts, ReasonStateStale, ""),
			mutatesState: true,
		},
		{
			name: "blocked", decision: newManagedPathBlocked(facts, ReasonUnmanagedOutputExists, ""),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.decision.MutatesHost(); got != test.mutatesHost {
				t.Fatalf("MutatesHost() = %t, want %t", got, test.mutatesHost)
			}
			if got := test.decision.MutatesState(); got != test.mutatesState {
				t.Fatalf("MutatesState() = %t, want %t", got, test.mutatesState)
			}
		})
	}
}

func TestManagedPathDecisionKindClassificationRejectsUnsupportedKind(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unsupported managed-path kind did not panic")
		}
	}()
	ManagedPathDecisionKind("future-kind").MutatesHost()
}

func TestNewManagedPathDecisionRejectsUnsupportedKindWithoutClassifyingIt(t *testing.T) {
	_, err := NewManagedPathDecision(ManagedPathDecisionInput{
		Kind: "future-kind",
	})
	if err == nil || !strings.Contains(err.Error(), `kind "future-kind" is unsupported`) {
		t.Fatalf("NewManagedPathDecision error = %v, want unsupported-kind error", err)
	}
}

func TestResultHasLockReadinessErrorsIncludesUnexpectedManagedPathProjection(t *testing.T) {
	t.Parallel()

	projection, err := topology.NewSubjectID(topology.SubjectProjection, "test.plan", "oracle")
	if err != nil {
		t.Fatal(err)
	}
	plan := mustReconciliationResult(t, []ManagedPathDecision{
		newManagedPathBlocked(managedPathDecisionFacts{
			subject:         projection,
			consumerTargets: []target.Target{target.TargetCodex},
			scope:           target.ScopeProject,
			destination:     "AGENTS.md",
		}, ReasonUnexpectedLockSubject, "unexpected locked projection"),
	}, nil)
	if !plan.HasLockReadinessErrors() {
		t.Fatal("HasLockReadinessErrors() = false, want true for unexpected managed path projection")
	}
}

func TestResultDecisionsExpandAggregateSubjectsAndOrderPublishSwitchRetire(t *testing.T) {
	assetSubject, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"hook-asset.project.data",
		"hook_asset:runner",
	)
	if err != nil {
		t.Fatal(err)
	}
	pathFacts := managedPathDecisionFacts{
		subject: assetSubject, consumerTargets: []target.Target{target.TargetCodex},
		scope: target.ScopeProject, destination: ".daem/hook-assets/runner/new/asset",
	}
	publish := newManagedPathCreate(pathFacts, ReasonMissingOutput)
	retireFacts := pathFacts
	retireFacts.destination = ".daem/hook-assets/runner/old/asset"
	retire := newManagedPathRemove(retireFacts, ReasonRemovedFromManifest)

	guard := aggregateDecisionInputForServer(t, "guard")
	audit := aggregateDecisionInputForServer(t, "audit")
	guard.Kind, guard.Reason = AggregateCreate, ReasonMissingOutput
	guard.Projections[0].Kind, guard.Projections[0].Reason = AggregateCreate, ReasonMissingOutput
	guard.Projections[0].Subjects[0].Kind = AggregateCreate
	guard.Projections[0].Subjects[0].Reason = ReasonMissingOutput
	audit.Projections[0].Kind, audit.Projections[0].Reason = AggregateCreate, ReasonMissingOutput
	audit.Projections[0].Subjects[0].Kind = AggregateCreate
	audit.Projections[0].Subjects[0].Reason = ReasonMissingOutput
	guard.Projections = append(guard.Projections, audit.Projections...)
	aggregateDecision, err := NewAggregateDecision(guard)
	if err != nil {
		t.Fatal(err)
	}
	aggregates := []AggregateDecision{aggregateDecision}

	planned := mustReconciliationResult(
		t,
		[]ManagedPathDecision{retire, publish},
		aggregates,
	)
	decisions := planned.Decisions()
	if len(decisions) != 4 {
		t.Fatalf("len(Decisions()) = %d, want publish + two aggregate subjects + retire", len(decisions))
	}
	if first, ok := decisions[0].ManagedPath(); !ok || first.Kind() != ManagedPathCreate || first.Subject() != assetSubject {
		t.Fatalf("Decisions()[0] = %#v, want HookAsset publish", decisions[0])
	}
	for index, wantSubject := range aggregates[0].Subjects() {
		view, ok := decisions[index+1].Aggregate()
		if !ok || view.Subject() != wantSubject || view.Kind() != AggregateCreate {
			t.Fatalf("Decisions()[%d] = %#v, want aggregate subject %q", index+1, decisions[index+1], wantSubject)
		}
	}
	if last, ok := decisions[3].ManagedPath(); !ok || last.Kind() != ManagedPathRemove || last.Subject() != assetSubject {
		t.Fatalf("Decisions()[3] = %#v, want HookAsset retire", decisions[3])
	}
}

func TestResultMutatingDecisionsOwnVariantSemantics(t *testing.T) {
	subject, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"instructions.project.agents",
		"instructions:project",
	)
	if err != nil {
		t.Fatal(err)
	}
	facts := managedPathDecisionFacts{
		subject: subject, consumerTargets: []target.Target{target.TargetCodex},
		scope: target.ScopeProject, destination: "AGENTS.md",
	}
	recordSubject, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"instructions.project.agents",
		"instructions:record",
	)
	if err != nil {
		t.Fatal(err)
	}
	recordFacts := facts
	recordFacts.subject = recordSubject
	recordFacts.destination = "RECORD.md"
	planned := mustReconciliationResult(t, []ManagedPathDecision{
		newManagedPathNoOp(facts, ReasonAlreadyCurrent),
		newManagedPathRecord(recordFacts, ReasonStateStale, ""),
	}, nil)
	decisions := planned.Decisions()
	noOps := 0
	mutations := 0
	for _, decision := range decisions {
		if decision.IsNoOp() {
			noOps++
		}
		if decision.IsMutation() {
			mutations++
		}
	}
	if noOps != 1 || mutations != 1 {
		t.Fatalf("Decision semantics = noops:%d mutations:%d, want 1/1", noOps, mutations)
	}
	if got := planned.MutatingDecisions(); len(got) != 1 {
		t.Fatalf("MutatingDecisions length = %d, want 1", len(got))
	}
}

func TestResultDecisionsRankSharedManagedPathsByEveryConsumer(t *testing.T) {
	subject, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"instructions.project.agents",
		"instructions:shared",
	)
	if err != nil {
		t.Fatal(err)
	}
	shared := newManagedPathCreate(managedPathDecisionFacts{
		subject: subject,
		consumerTargets: []target.Target{
			target.TargetAntigravityCLI,
			target.TargetCodex,
		},
		scope:       target.ScopeProject,
		destination: "ZZZ.md",
	}, ReasonMissingOutput)
	claudeSubject, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"hook.project.claude",
		"hook:test",
	)
	if err != nil {
		t.Fatal(err)
	}
	claude := newManagedPathCreate(managedPathDecisionFacts{
		subject: claudeSubject, consumerTargets: []target.Target{target.TargetClaudeCode},
		scope: target.ScopeProject, destination: "AAA.json",
	}, ReasonMissingOutput)

	decisions := mustReconciliationResult(t, []ManagedPathDecision{claude, shared}, nil).Decisions()
	if first, ok := decisions[0].ManagedPath(); !ok || first.Subject() != subject {
		t.Fatalf("Decisions()[0] = %#v, want shared path ranked by its Codex consumer", decisions[0])
	}
}
