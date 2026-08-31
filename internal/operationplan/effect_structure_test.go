package operationplan

import (
	"strings"
	"testing"
)

func TestEffectStructureDerivesSequenceChoiceAndRepeatDemand(t *testing.T) {
	t.Parallel()
	ensure := mustEffectStep(t, "ensure", effectStepEstablishStateDir)
	barrierBefore := mustEffectStep(t, "barrier-before", effectStepValidateBarrier)
	barrierAfter := mustEffectStep(t, "barrier-after", effectStepValidateBarrier)
	stateDirBranch := mustEffectStep(t, "state-dir-branch", effectStepValidateStateDir)
	stateDirRepeat := mustEffectStep(t, "state-dir-repeat", effectStepValidateStateDir)
	validate := mustEffectStep(t, "statefile-validate", effectStepValidateDescendant)
	commit := mustEffectStep(t, "statefile-commit", effectStepPublishDescendant)
	observed := mustEffectStep(t, "observe", effectStepObservation)

	branch, err := newEffectChoice(
		"persist-or-observe",
		newEffectSequence(barrierBefore, barrierAfter, validate, commit),
		newEffectSequence(stateDirBranch, observed),
	)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := newEffectRepeat(3, stateDirRepeat)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(newEffectSequence(ensure, branch, repeated))
	if err != nil {
		t.Fatal(err)
	}

	want := effectDemand{
		ensureCalls:             1,
		barrierValidationCalls:  2,
		stateDirValidationCalls: 4,
		descendantValidations:   1,
		descendantFileCommits:   1,
	}
	upperBound, err := structure.legacyUpperBound()
	if err != nil {
		t.Fatal(err)
	}
	if upperBound != want {
		t.Fatalf("legacy upper bound = %#v, want %#v", upperBound, want)
	}
}

func TestEffectStructureChoiceUsesComponentwiseMaximum(t *testing.T) {
	t.Parallel()
	barrierOne := mustEffectStep(t, "barrier-1", effectStepValidateBarrier)
	barrierTwo := mustEffectStep(t, "barrier-2", effectStepValidateBarrier)
	barrierThree := mustEffectStep(t, "barrier-3", effectStepValidateBarrier)
	validate := mustEffectStep(t, "validate", effectStepValidateDescendant)
	commit := mustEffectStep(t, "commit", effectStepPublishDescendant)
	stateDirOne := mustEffectStep(t, "state-dir-1", effectStepValidateStateDir)
	stateDirTwo := mustEffectStep(t, "state-dir-2", effectStepValidateStateDir)

	choice, err := newEffectChoice(
		"componentwise",
		newEffectSequence(barrierOne, barrierTwo, barrierThree, commit),
		newEffectSequence(stateDirOne, stateDirTwo, validate),
	)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(choice)
	if err != nil {
		t.Fatal(err)
	}
	want := effectDemand{
		barrierValidationCalls:  3,
		stateDirValidationCalls: 2,
		descendantValidations:   1,
		descendantFileCommits:   1,
	}
	upperBound, err := structure.legacyUpperBound()
	if err != nil {
		t.Fatal(err)
	}
	if upperBound != want {
		t.Fatalf("choice upper bound = %#v, want %#v", upperBound, want)
	}
}

func TestEffectStructureLowersEachChoiceBeforeTakingMaximum(t *testing.T) {
	t.Parallel()
	barrierOne := mustEffectStep(t, "barrier-1", effectStepValidateBarrier)
	barrierTwo := mustEffectStep(t, "barrier-2", effectStepValidateBarrier)
	barrierThree := mustEffectStep(t, "barrier-3", effectStepValidateBarrier)
	commit := mustEffectStep(t, "commit", effectStepPublishDescendant)
	choice, err := newEffectChoice(
		"physical-alternatives",
		newEffectSequence(barrierOne, barrierTwo, barrierThree),
		commit,
	)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(choice)
	if err != nil {
		t.Fatal(err)
	}

	cost, err := lowerTestEffectCost(structure.root, func(kind effectStepKind) (int, error) {
		switch kind {
		case effectStepValidateBarrier:
			return 10, nil
		case effectStepPublishDescendant:
			return 25, nil
		default:
			return 0, nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cost != 30 {
		t.Fatalf("lowered choice cost = %d, want reachable maximum 30", cost)
	}
	upperBound, err := structure.legacyUpperBound()
	if err != nil {
		t.Fatal(err)
	}
	if upperBound.barrierValidationCalls != 3 ||
		upperBound.descendantFileCommits != 1 {
		t.Fatalf("legacy upper bound = %#v", upperBound)
	}
}

func TestEffectStructureRejectsOverflowAndDuplicateReferences(t *testing.T) {
	t.Parallel()
	step := mustEffectStep(t, "shared", effectStepValidateBarrier)
	repeated, err := newEffectRepeat(int(^uint(0)>>1), step)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileEffectStructure(newEffectSequence(repeated, step)); err == nil ||
		!strings.Contains(err.Error(), "duplicate effect step") {
		t.Fatalf("duplicate error = %v", err)
	}

	other := mustEffectStep(t, "other", effectStepValidateBarrier)
	overflow, err := newEffectRepeat(int(^uint(0)>>1), other)
	if err != nil {
		t.Fatal(err)
	}
	last := mustEffectStep(t, "last", effectStepValidateBarrier)
	overflowStructure, err := compileEffectStructure(newEffectSequence(overflow, last))
	if err != nil {
		t.Fatalf("canonical structure rejected legacy projection overflow: %v", err)
	}
	if _, err := overflowStructure.legacyUpperBound(); err == nil ||
		!strings.Contains(err.Error(), "overflows") {
		t.Fatalf("legacy overflow error = %v", err)
	}

	left, err := newEffectChoice(
		"duplicate-choice",
		mustEffectStep(t, "left-a", effectStepObservation),
		mustEffectStep(t, "left-b", effectStepObservation),
	)
	if err != nil {
		t.Fatal(err)
	}
	right, err := newEffectChoice(
		"duplicate-choice",
		mustEffectStep(t, "right-a", effectStepObservation),
		mustEffectStep(t, "right-b", effectStepObservation),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileEffectStructure(newEffectSequence(left, right)); err == nil ||
		!strings.Contains(err.Error(), "duplicate effect choice") {
		t.Fatalf("choice duplicate error = %v", err)
	}
}

func TestEffectStructureOwnsInputCopies(t *testing.T) {
	t.Parallel()
	first := mustEffectStep(t, "first", effectStepValidateBarrier)
	second := mustEffectStep(t, "second", effectStepValidateStateDir)
	parts := []effectNode{first, second}
	sequence := newEffectSequence(parts...)
	parts[0] = mustEffectStep(t, "replacement", effectStepPublishDescendant)

	structure, err := compileEffectStructure(sequence)
	if err != nil {
		t.Fatal(err)
	}
	want := effectDemand{
		barrierValidationCalls:  1,
		stateDirValidationCalls: 1,
	}
	upperBound, err := structure.legacyUpperBound()
	if err != nil {
		t.Fatal(err)
	}
	if upperBound != want {
		t.Fatalf("upper bound changed through caller slice: %#v", upperBound)
	}

	sequence.children[0] = mustEffectStep(t, "mutated", effectStepPublishDescendant)
	upperBound, err = structure.legacyUpperBound()
	if err != nil {
		t.Fatal(err)
	}
	if upperBound != want {
		t.Fatalf("compiled upper bound changed through source node: %#v", upperBound)
	}
}

func TestEffectCursorConsumesSelectedBranchInOrder(t *testing.T) {
	t.Parallel()
	barrier := mustEffectStep(t, "barrier", effectStepValidateBarrier)
	external := mustEffectStep(t, "external", effectStepExternal)
	validate := mustEffectStep(t, "validate", effectStepValidateDescendant)
	persist := mustEffectStep(t, "persist", effectStepPersistence)
	observe := mustEffectStep(t, "observe", effectStepObservation)
	terminal := mustEffectStep(t, "terminal", effectStepTerminal)
	branch, err := newEffectChoice(
		"attempt-result",
		external,
		newEffectSequence(validate, persist),
	)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := newEffectRepeat(2, observe)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(
		newEffectSequence(barrier, branch, repeated, terminal),
	)
	if err != nil {
		t.Fatal(err)
	}
	cursor := newEffectCursor(structure)

	if err := cursor.consume("barrier", effectStepValidateBarrier); err != nil {
		t.Fatal(err)
	}
	if err := cursor.selectAlternative("attempt-result", 1); err != nil {
		t.Fatal(err)
	}
	for _, step := range []effectStep{
		{id: "validate", kind: effectStepValidateDescendant},
		{id: "persist", kind: effectStepPersistence},
		{id: "observe", kind: effectStepObservation},
		{id: "observe", kind: effectStepObservation},
		{id: "terminal", kind: effectStepTerminal},
	} {
		if err := cursor.consume(step.id, step.kind); err != nil {
			t.Fatalf("consume %#v: %v", step, err)
		}
	}
	if err := cursor.finishSuccess(); err != nil {
		t.Fatal(err)
	}
	if err := cursor.finishSuccess(); err == nil || !strings.Contains(err.Error(), "already finished") {
		t.Fatalf("second finish error = %v", err)
	}
}

func TestEffectCursorRejectsWrongBranchOrderAndUnderConsumption(t *testing.T) {
	t.Parallel()
	first := mustEffectStep(t, "first", effectStepValidateBarrier)
	left := mustEffectStep(t, "left", effectStepObservation)
	right := mustEffectStep(t, "right", effectStepObservation)
	choice, err := newEffectChoice("branch", left, right)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(newEffectSequence(first, choice))
	if err != nil {
		t.Fatal(err)
	}

	cursor := newEffectCursor(structure)
	if err := cursor.consume("right", effectStepObservation); err == nil ||
		!strings.Contains(err.Error(), "next effect step") {
		t.Fatalf("out-of-order error = %v", err)
	}
	if err := cursor.consume("first", effectStepValidateBarrier); err != nil {
		t.Fatal(err)
	}
	if err := cursor.consume("left", effectStepObservation); err == nil ||
		!strings.Contains(err.Error(), "must be selected") {
		t.Fatalf("unselected-branch error = %v", err)
	}
	if err := cursor.selectAlternative("other", 0); err == nil ||
		!strings.Contains(err.Error(), "not \"other\"") {
		t.Fatalf("wrong-choice error = %v", err)
	}
	if err := cursor.selectAlternative("branch", 0); err != nil {
		t.Fatal(err)
	}
	if err := cursor.finishSuccess(); err == nil || !strings.Contains(err.Error(), "mandatory effect step") {
		t.Fatalf("under-consumption error = %v", err)
	}
	if err := cursor.consume("left", effectStepObservation); err != nil {
		t.Fatal(err)
	}
	if err := cursor.consume("left", effectStepObservation); err == nil ||
		!strings.Contains(err.Error(), "no remaining step") {
		t.Fatalf("duplicate/extra error = %v", err)
	}
}

func TestEffectCursorAllowsOnlyPreEffectAbort(t *testing.T) {
	t.Parallel()
	barrier := mustEffectStep(t, "barrier", effectStepValidateBarrier)
	external := mustEffectStep(t, "external", effectStepExternal)
	structure, err := compileEffectStructure(newEffectSequence(barrier, external))
	if err != nil {
		t.Fatal(err)
	}

	before := newEffectCursor(structure)
	if err := before.consume("barrier", effectStepValidateBarrier); err != nil {
		t.Fatal(err)
	}
	if err := before.abortBeforeEffect(); err != nil {
		t.Fatal(err)
	}
	if err := before.consume("external", effectStepExternal); err == nil ||
		!strings.Contains(err.Error(), "already aborted") {
		t.Fatalf("post-abort consume error = %v", err)
	}

	after := newEffectCursor(structure)
	if err := after.consume("barrier", effectStepValidateBarrier); err != nil {
		t.Fatal(err)
	}
	if err := after.consume("external", effectStepExternal); err != nil {
		t.Fatal(err)
	}
	if err := after.abortBeforeEffect(); err == nil || !strings.Contains(err.Error(), "after an effect started") {
		t.Fatalf("post-effect abort error = %v", err)
	}
}

func TestEffectStructureRejectsInvalidReferencesAndChoices(t *testing.T) {
	t.Parallel()
	if _, err := newEffectStep(" ", effectStepExternal); err == nil {
		t.Fatal("empty step reference accepted")
	}
	if _, err := newEffectStep(" step ", effectStepExternal); err == nil {
		t.Fatal("non-canonical step reference accepted")
	}
	if _, err := newEffectStep("step", 0); err == nil {
		t.Fatal("invalid step kind accepted")
	}
	if _, err := newEffectChoice(
		"single",
		mustEffectStep(t, "only", effectStepObservation),
	); err == nil {
		t.Fatal("single-alternative choice accepted")
	}
	if _, err := newEffectRepeat(-1, mustEffectStep(t, "repeat", effectStepObservation)); err == nil {
		t.Fatal("negative repetition accepted")
	}
}

func lowerTestEffectCost(
	node effectNode,
	stepCost func(effectStepKind) (int, error),
) (int, error) {
	switch node.kind {
	case effectNodeEmpty:
		return 0, nil
	case effectNodeStep:
		return stepCost(node.step.kind)
	case effectNodeSequence:
		cost := 0
		for _, child := range node.children {
			childCost, err := lowerTestEffectCost(child, stepCost)
			if err != nil {
				return 0, err
			}
			cost, err = checkedAdd(cost, childCost)
			if err != nil {
				return 0, err
			}
		}
		return cost, nil
	case effectNodeChoice:
		cost := 0
		for _, child := range node.children {
			childCost, err := lowerTestEffectCost(child, stepCost)
			if err != nil {
				return 0, err
			}
			cost = max(cost, childCost)
		}
		return cost, nil
	case effectNodeRepeat:
		childCost, err := lowerTestEffectCost(node.children[0], stepCost)
		if err != nil {
			return 0, err
		}
		return checkedMul(childCost, node.repetitions)
	default:
		return 0, nil
	}
}

func mustEffectStep(t *testing.T, id string, kind effectStepKind) effectNode {
	t.Helper()
	step, err := newEffectStep(id, kind)
	if err != nil {
		t.Fatal(err)
	}
	return step
}
