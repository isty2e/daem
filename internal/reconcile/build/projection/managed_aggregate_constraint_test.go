package projection

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestAggregateSubjectConstraintBlocksAnOtherwiseWritableProjection(t *testing.T) {
	input := aggregateConstraintTestInput(t)
	subject := input.Expected[0].SubjectID()
	constraint, err := NewAggregateSubjectConstraint(
		subject,
		reconcile.ReasonEffectiveStateConflict,
		"provider-effective source defines the same name",
	)
	if err != nil {
		t.Fatalf("NewAggregateSubjectConstraint returned error: %v", err)
	}
	input.Constraints = []AggregateSubjectConstraint{constraint}

	decisions, err := buildAggregateDecisionsForTest(input)
	if err != nil {
		t.Fatalf("BuildAggregateDecisions returned error: %v", err)
	}
	decision := onlyAggregateDecision(t, decisions)
	if !decision.IsBlocked() ||
		decision.Reason() != reconcile.ReasonEffectiveStateConflict ||
		decision.MutatesHost() {
		t.Fatalf(
			"decision = %q/%q mutates=%t, want blocked effective conflict",
			decision.Kind(),
			decision.Reason(),
			decision.MutatesHost(),
		)
	}
}

func TestAggregateSubjectConstraintPreservesNonLockCauseWithinProjection(t *testing.T) {
	guard := aggregateHookContract(t, "guard", "echo guard")
	audit := aggregateHookContract(t, "audit", "echo audit")
	items := aggregateItems(t, guard, audit)
	constraint, err := NewAggregateSubjectConstraint(
		guard.SubjectID(),
		reconcile.ReasonEffectiveStateUnobserved,
		"effective provider state is unobserved",
	)
	if err != nil {
		t.Fatal(err)
	}

	decisions, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked:          aggregateLockedSection(t, guard, audit),
		Expected:        []lock.LockedSubjectContract{guard, audit},
		Desired:         items,
		Evidence:        []observe.AggregateEvidence{aggregateEvidence(t, items[0].Contribution().Contract(), aggregate.AbsentDocument())},
		Constraints:     []AggregateSubjectConstraint{constraint},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil {
		t.Fatal(err)
	}
	result := mustReconciliationResult(t, nil, decisions)
	views := aggregateSubjectDecisionsBySubject(t, result.Decisions())
	for _, subject := range []topology.SubjectID{guard.SubjectID(), audit.SubjectID()} {
		if got := views[subject]; got.Reason() != reconcile.ReasonEffectiveStateUnobserved {
			t.Fatalf("subject %q reason = %q, want effective state unobserved", subject, got.Reason())
		}
	}
	if result.HasLockReadinessErrors() {
		t.Fatal("non-lock constraint within one projection requested a lock rebuild")
	}
}

func TestAggregateSubjectConstraintDoesNotBecomeLockBlockerOnObservationFailure(t *testing.T) {
	context7 := aggregateMCPContract(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"})
	filesystem := aggregateMCPContract(t, "filesystem", "npx", []string{"-y", "@modelcontextprotocol/server-filesystem", "."})
	items := aggregateItems(t, context7, filesystem)
	constraint, err := NewAggregateSubjectConstraint(
		context7.SubjectID(),
		reconcile.ReasonProviderVersionIncompatible,
		"provider version is incompatible",
	)
	if err != nil {
		t.Fatal(err)
	}
	failure := aggregateConstraintObservationFailure(
		t,
		aggregateContractsFromItems(items),
		aggregate.ExistingDocument([]byte(`{"mcpServers":null}`)),
	)

	decisions, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked:              aggregateLockedSection(t, context7, filesystem),
		Expected:            []lock.LockedSubjectContract{context7, filesystem},
		Desired:             items,
		ObservationFailures: []observe.AggregateObservationFailure{failure},
		Constraints:         []AggregateSubjectConstraint{constraint},
		SelectedTargets:     planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil {
		t.Fatal(err)
	}
	result := mustReconciliationResult(t, nil, decisions)
	views := aggregateSubjectDecisionsBySubject(t, result.Decisions())
	if got := views[context7.SubjectID()]; got.Reason() != reconcile.ReasonProviderVersionIncompatible {
		t.Fatalf("constrained subject reason = %q, want provider version incompatible", got.Reason())
	}
	if got := views[filesystem.SubjectID()]; got.Reason() != reconcile.ReasonInvalidDesiredState {
		t.Fatalf("unconstrained sibling reason = %q, want invalid desired state", got.Reason())
	}
	if result.HasLockReadinessErrors() {
		t.Fatal("provider constraint plus observation failure requested a lock rebuild")
	}
	decision := onlyAggregateDecision(t, decisions)
	if decision.MutatesHost() || decision.MutatesState() {
		t.Fatal("blocked aggregate decision mutates host or state")
	}
}

func TestAggregateSubjectConstraintDoesNotBecomeLockBlockerOnPreconditionFailure(t *testing.T) {
	context7 := aggregateOpenCodeMCPContract(t, "context7", "npx", []string{"-y", "@upstash/context7-mcp"})
	filesystem := aggregateOpenCodeMCPContract(t, "filesystem", "npx", []string{"-y", "@modelcontextprotocol/server-filesystem", "."})
	items := aggregateItems(t, context7, filesystem)
	contracts := aggregateContractsFromItems(items)
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	preconditions, admitted, err := aggregate.OperationPreconditionsForSelection(selection)
	if err != nil {
		t.Fatal(err)
	}
	if !admitted || len(preconditions) != 1 {
		t.Fatalf("operation preconditions = %#v admitted=%t, want one", preconditions, admitted)
	}
	preconditionEvidence, err := observe.NewAggregatePreconditionEvidence(
		selection.DocumentAddress(),
		preconditions[0],
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	constraint, err := NewAggregateSubjectConstraint(
		context7.SubjectID(),
		reconcile.ReasonEffectiveStateConflict,
		"effective provider state conflicts",
	)
	if err != nil {
		t.Fatal(err)
	}

	decisions, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked:               aggregateLockedSection(t, context7, filesystem),
		Expected:             []lock.LockedSubjectContract{context7, filesystem},
		Desired:              items,
		Evidence:             []observe.AggregateEvidence{aggregateEvidenceForContracts(t, contracts, aggregate.AbsentDocument())},
		PreconditionEvidence: []observe.AggregatePreconditionEvidence{preconditionEvidence},
		Constraints:          []AggregateSubjectConstraint{constraint},
		SelectedTargets:      planSelectedTargets(t, target.TargetOpenCode),
	})
	if err != nil {
		t.Fatal(err)
	}
	result := mustReconciliationResult(t, nil, decisions)
	views := aggregateSubjectDecisionsBySubject(t, result.Decisions())
	if got := views[context7.SubjectID()]; got.Reason() != reconcile.ReasonEffectiveStateConflict {
		t.Fatalf("constrained subject reason = %q, want effective state conflict", got.Reason())
	}
	if got := views[filesystem.SubjectID()]; got.Reason() != reconcile.ReasonInvalidDesiredState {
		t.Fatalf("unconstrained sibling reason = %q, want invalid desired state", got.Reason())
	}
	if result.HasLockReadinessErrors() {
		t.Fatal("effective-state constraint plus precondition failure requested a lock rebuild")
	}
	decision := onlyAggregateDecision(t, decisions)
	if decision.MutatesHost() || decision.MutatesState() {
		t.Fatal("blocked aggregate decision mutates host or state")
	}
}

func TestAggregateSubjectConstraintRejectsUnknownOrDuplicateSubjects(t *testing.T) {
	input := aggregateConstraintTestInput(t)
	subject := input.Expected[0].SubjectID()
	first, err := NewAggregateSubjectConstraint(
		subject,
		reconcile.ReasonEffectiveStateUnobserved,
		"provider-effective source is opaque",
	)
	if err != nil {
		t.Fatal(err)
	}
	input.Constraints = []AggregateSubjectConstraint{first, first}
	_, err = buildAggregateDecisionsForTest(input)
	if err == nil || !strings.Contains(err.Error(), "duplicate aggregate constraint") {
		t.Fatalf("duplicate constraint error = %v", err)
	}

	foreign := syntheticManagedFileSubject(t, "foreign")
	unknown, err := NewAggregateSubjectConstraint(
		foreign,
		reconcile.ReasonEffectiveStateConflict,
		"foreign constraint",
	)
	if err != nil {
		t.Fatal(err)
	}
	input.Constraints = []AggregateSubjectConstraint{unknown}
	_, err = buildAggregateDecisionsForTest(input)
	if err == nil || !strings.Contains(err.Error(), "has no selected expectation") {
		t.Fatalf("foreign constraint error = %v", err)
	}
}

func aggregateConstraintTestInput(t *testing.T) AggregateInput {
	t.Helper()
	contract := aggregateHookContract(t, "guard", "echo guard")
	desired := aggregateItems(t, contract)
	projection := desired[0].Contribution().Contract()
	return AggregateInput{
		Locked:          aggregateLockedSection(t, contract),
		Expected:        []lock.LockedSubjectContract{contract},
		Desired:         desired,
		Evidence:        []observe.AggregateEvidence{aggregateEvidence(t, projection, aggregate.AbsentDocument())},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	}
}

func aggregateConstraintObservationFailure(
	t *testing.T,
	contracts []aggregate.ProjectionContract,
	document aggregate.Document,
) observe.AggregateObservationFailure {
	t.Helper()
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	failure, err := aggregate.NewCodecFailure(
		aggregate.CodecFailureSelectedShapeUnsupported,
		contracts[0].Address().ContentPath(),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := observe.NewAggregateObservationFailure(
		document,
		selection,
		aggregate.DocumentFileMode,
		failure,
	)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
