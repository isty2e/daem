package operationplan

import (
	"strings"
	"testing"
)

func TestEffectStructureDerivesSequenceChoiceAndRepeatDemand(t *testing.T) {
	t.Parallel()
	ensure := mustEffectStep(t, "ensure", EffectStepEstablishStateDir)
	barrierBefore := mustEffectStep(t, "barrier-before", EffectStepValidateBarrier)
	barrierAfter := mustEffectStep(t, "barrier-after", EffectStepValidateBarrier)
	stateDirBranch := mustEffectStep(t, "state-dir-branch", EffectStepValidateStateDir)
	stateDirRepeat := mustEffectStep(t, "state-dir-repeat", EffectStepValidateStateDir)
	validate := mustEffectStep(t, "statefile-validate", EffectStepValidateDescendant)
	commit := mustEffectStep(t, "statefile-commit", EffectStepPublishDescendant)
	observed := mustEffectStep(t, "observe", EffectStepObservation)

	branch, err := newEffectChoice(
		"persist-or-observe",
		EffectSequence(barrierBefore, barrierAfter, validate, commit),
		EffectSequence(stateDirBranch, observed),
	)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := newEffectRepeat(3, stateDirRepeat)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(EffectSequence(ensure, branch, repeated))
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
	barrierOne := mustEffectStep(t, "barrier-1", EffectStepValidateBarrier)
	barrierTwo := mustEffectStep(t, "barrier-2", EffectStepValidateBarrier)
	barrierThree := mustEffectStep(t, "barrier-3", EffectStepValidateBarrier)
	validate := mustEffectStep(t, "validate", EffectStepValidateDescendant)
	commit := mustEffectStep(t, "commit", EffectStepPublishDescendant)
	stateDirOne := mustEffectStep(t, "state-dir-1", EffectStepValidateStateDir)
	stateDirTwo := mustEffectStep(t, "state-dir-2", EffectStepValidateStateDir)

	choice, err := newEffectChoice(
		"componentwise",
		EffectSequence(barrierOne, barrierTwo, barrierThree, commit),
		EffectSequence(stateDirOne, stateDirTwo, validate),
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

func TestEffectForwardPhaseDerivesEstablishmentAndIdentityChecks(t *testing.T) {
	t.Parallel()
	var builder EffectStructureBuilder
	phase := builder.ForwardPhase(
		"final",
		EffectSequence(
			builder.Step("effect-1", EffectStepForwardEffect),
			builder.Step("observe", EffectStepObservation),
			builder.Step("effect-2", EffectStepForwardEffect),
			builder.Step("effect-3", EffectStepForwardEffect),
		),
	)
	structure, err := builder.Compile(phase)
	if err != nil {
		t.Fatal(err)
	}
	demand, err := structure.LegacyDemand()
	if err != nil {
		t.Fatal(err)
	}
	if demand.EnsureCalls() != 1 || demand.StateDirValidationCalls() != 2 {
		t.Fatalf(
			"forward phase ensure/StateDir = %d/%d, want 1/2",
			demand.EnsureCalls(),
			demand.StateDirValidationCalls(),
		)
	}
	cursor := structure.Begin()
	for _, step := range []struct {
		id   string
		kind EffectStepKind
	}{
		{id: "effect-1", kind: EffectStepForwardEffect},
		{id: "observe", kind: EffectStepObservation},
		{id: "effect-2", kind: EffectStepForwardEffect},
		{id: "effect-3", kind: EffectStepForwardEffect},
	} {
		if err := cursor.Consume(step.id, step.kind); err != nil {
			t.Fatal(err)
		}
	}
	if err := cursor.FinishSuccess(); err != nil {
		t.Fatal(err)
	}
}

func TestEffectForwardPhasePreservesChoiceBeforeLegacyProjection(t *testing.T) {
	t.Parallel()
	var builder EffectStructureBuilder
	choice := builder.Choice(
		"mutate-or-noop",
		builder.Step("noop-terminal", EffectStepTerminal),
		EffectSequence(
			builder.Step("mutate-1", EffectStepForwardEffect),
			builder.Step("mutate-2", EffectStepForwardEffect),
			builder.Step("mutate-terminal", EffectStepTerminal),
		),
	)
	structure, err := builder.Compile(builder.ForwardPhase("order", choice))
	if err != nil {
		t.Fatal(err)
	}
	demand, err := structure.LegacyDemand()
	if err != nil {
		t.Fatal(err)
	}
	if demand.EnsureCalls() != 1 || demand.StateDirValidationCalls() != 1 {
		t.Fatalf(
			"choice forward phase ensure/StateDir = %d/%d, want 1/1",
			demand.EnsureCalls(),
			demand.StateDirValidationCalls(),
		)
	}
}

func TestEffectTriggerActivatesOneSharedFollowUp(t *testing.T) {
	t.Parallel()
	var builder EffectStructureBuilder
	first := builder.Choice(
		"promotion-1",
		builder.Step("promotion-1-noop", EffectStepTerminal),
		builder.Trigger(
			"ownership-finalization",
			builder.Step("promotion-1-effect", EffectStepForwardEffect),
		),
	)
	second := builder.Choice(
		"promotion-2",
		builder.Step("promotion-2-noop", EffectStepTerminal),
		builder.Trigger(
			"ownership-finalization",
			builder.Step("promotion-2-effect", EffectStepForwardEffect),
		),
	)
	followUp := builder.Conditional(
		"ownership-finalization",
		builder.Step("ownership-finalization-effect", EffectStepForwardEffect),
	)
	structure, err := builder.Compile(builder.ForwardPhase(
		"apply",
		EffectSequence(first, second, followUp),
	))
	if err != nil {
		t.Fatal(err)
	}
	demand, err := structure.LegacyDemand()
	if err != nil {
		t.Fatal(err)
	}
	if demand.EnsureCalls() != 1 || demand.StateDirValidationCalls() != 2 {
		t.Fatalf(
			"promotion/follow-up ensure/StateDir = %d/%d, want 1/2",
			demand.EnsureCalls(),
			demand.StateDirValidationCalls(),
		)
	}

	t.Run("no promotion skips follow-up", func(t *testing.T) {
		cursor := structure.Begin()
		if err := cursor.SelectAlternative("promotion-1", 0); err != nil {
			t.Fatal(err)
		}
		if err := cursor.Consume("promotion-1-noop", EffectStepTerminal); err != nil {
			t.Fatal(err)
		}
		if err := cursor.SelectAlternative("promotion-2", 0); err != nil {
			t.Fatal(err)
		}
		if err := cursor.Consume("promotion-2-noop", EffectStepTerminal); err != nil {
			t.Fatal(err)
		}
		if err := cursor.FinishSuccess(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("any promotion requires one follow-up", func(t *testing.T) {
		cursor := structure.Begin()
		if err := cursor.SelectAlternative("promotion-1", 1); err != nil {
			t.Fatal(err)
		}
		if err := cursor.Consume("promotion-1-effect", EffectStepForwardEffect); err != nil {
			t.Fatal(err)
		}
		if err := cursor.SelectAlternative("promotion-2", 0); err != nil {
			t.Fatal(err)
		}
		if err := cursor.Consume("promotion-2-noop", EffectStepTerminal); err != nil {
			t.Fatal(err)
		}
		if err := cursor.FinishSuccess(); err == nil ||
			!strings.Contains(err.Error(), "ownership-finalization-effect") {
			t.Fatalf("missing follow-up error = %v", err)
		}
		if err := cursor.Consume(
			"ownership-finalization-effect",
			EffectStepForwardEffect,
		); err != nil {
			t.Fatal(err)
		}
		if err := cursor.FinishSuccess(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestEffectCursorRejectsLateOrUnreachedTriggeredFollowUp(t *testing.T) {
	t.Parallel()

	t.Run("trigger after conditional", func(t *testing.T) {
		var builder EffectStructureBuilder
		structure, err := builder.Compile(EffectSequence(
			builder.Conditional(
				"late",
				builder.Step("late-follow-up", EffectStepObservation),
			),
			builder.Trigger(
				"late",
				builder.Step("late-trigger", EffectStepObservation),
			),
		))
		if err != nil {
			t.Fatal(err)
		}
		cursor := structure.Begin()
		if err := cursor.Consume("late-trigger", EffectStepObservation); err == nil ||
			!strings.Contains(err.Error(), "after its conditional") {
			t.Fatalf("late trigger error = %v", err)
		}
	})

	t.Run("triggered conditional hidden by branch", func(t *testing.T) {
		var builder EffectStructureBuilder
		followUpChoice := builder.Choice(
			"follow-up-location",
			builder.Step("without-follow-up", EffectStepNoOp),
			builder.Conditional(
				"hidden",
				builder.Step("hidden-follow-up", EffectStepObservation),
			),
		)
		structure, err := builder.Compile(EffectSequence(
			builder.Trigger(
				"hidden",
				builder.Step("hidden-trigger", EffectStepObservation),
			),
			followUpChoice,
		))
		if err != nil {
			t.Fatal(err)
		}
		cursor := structure.Begin()
		if err := cursor.Consume("hidden-trigger", EffectStepObservation); err != nil {
			t.Fatal(err)
		}
		if err := cursor.SelectAlternative("follow-up-location", 0); err != nil {
			t.Fatal(err)
		}
		if err := cursor.Consume("without-follow-up", EffectStepNoOp); err != nil {
			t.Fatal(err)
		}
		if err := cursor.FinishSuccess(); err == nil ||
			!strings.Contains(err.Error(), "was not reached") {
			t.Fatalf("hidden follow-up error = %v", err)
		}
	})
}

func TestEffectStructureRejectsUnpairedTriggersAndConditionals(t *testing.T) {
	t.Parallel()
	trigger, err := newEffectTrigger(
		"orphan",
		mustEffectStep(t, "trigger-body", EffectStepObservation),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileEffectStructure(trigger); err == nil ||
		!strings.Contains(err.Error(), "has no conditional follow-up") {
		t.Fatalf("orphan trigger error = %v", err)
	}
	conditional, err := newEffectConditional(
		"orphan",
		mustEffectStep(t, "conditional-body", EffectStepObservation),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileEffectStructure(conditional); err == nil ||
		!strings.Contains(err.Error(), "has no trigger") {
		t.Fatalf("orphan conditional error = %v", err)
	}
}

func TestEffectStructureLowersEachChoiceBeforeTakingMaximum(t *testing.T) {
	t.Parallel()
	barrierOne := mustEffectStep(t, "barrier-1", EffectStepValidateBarrier)
	barrierTwo := mustEffectStep(t, "barrier-2", EffectStepValidateBarrier)
	barrierThree := mustEffectStep(t, "barrier-3", EffectStepValidateBarrier)
	commit := mustEffectStep(t, "commit", EffectStepPublishDescendant)
	choice, err := newEffectChoice(
		"physical-alternatives",
		EffectSequence(barrierOne, barrierTwo, barrierThree),
		commit,
	)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(choice)
	if err != nil {
		t.Fatal(err)
	}

	cost, err := lowerTestEffectCost(structure.root, func(kind EffectStepKind) (int, error) {
		switch kind {
		case EffectStepValidateBarrier:
			return 10, nil
		case EffectStepPublishDescendant:
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
	step := mustEffectStep(t, "shared", EffectStepValidateBarrier)
	repeated, err := newEffectRepeat(int(^uint(0)>>1), step)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileEffectStructure(EffectSequence(repeated, step)); err == nil ||
		!strings.Contains(err.Error(), "duplicate effect step") {
		t.Fatalf("duplicate error = %v", err)
	}

	other := mustEffectStep(t, "other", EffectStepValidateBarrier)
	overflow, err := newEffectRepeat(int(^uint(0)>>1), other)
	if err != nil {
		t.Fatal(err)
	}
	last := mustEffectStep(t, "last", EffectStepValidateBarrier)
	overflowStructure, err := compileEffectStructure(EffectSequence(overflow, last))
	if err != nil {
		t.Fatalf("canonical structure rejected legacy projection overflow: %v", err)
	}
	if _, err := overflowStructure.legacyUpperBound(); err == nil ||
		!strings.Contains(err.Error(), "overflows") {
		t.Fatalf("legacy overflow error = %v", err)
	}

	left, err := newEffectChoice(
		"duplicate-choice",
		mustEffectStep(t, "left-a", EffectStepObservation),
		mustEffectStep(t, "left-b", EffectStepObservation),
	)
	if err != nil {
		t.Fatal(err)
	}
	right, err := newEffectChoice(
		"duplicate-choice",
		mustEffectStep(t, "right-a", EffectStepObservation),
		mustEffectStep(t, "right-b", EffectStepObservation),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileEffectStructure(EffectSequence(left, right)); err == nil ||
		!strings.Contains(err.Error(), "duplicate effect choice") {
		t.Fatalf("choice duplicate error = %v", err)
	}
}

func TestEffectStructureOwnsInputCopies(t *testing.T) {
	t.Parallel()
	first := mustEffectStep(t, "first", EffectStepValidateBarrier)
	second := mustEffectStep(t, "second", EffectStepValidateStateDir)
	parts := []EffectNode{first, second}
	sequence := EffectSequence(parts...)
	parts[0] = mustEffectStep(t, "replacement", EffectStepPublishDescendant)

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

	sequence.children[0] = mustEffectStep(t, "mutated", EffectStepPublishDescendant)
	upperBound, err = structure.legacyUpperBound()
	if err != nil {
		t.Fatal(err)
	}
	if upperBound != want {
		t.Fatalf("compiled upper bound changed through source node: %#v", upperBound)
	}
}

func TestEffectStructureEqualityIsExactAndValueBased(t *testing.T) {
	t.Parallel()
	left, err := compileEffectStructure(EffectSequence())
	if err != nil {
		t.Fatal(err)
	}
	right, err := compileEffectStructure(EffectSequence())
	if err != nil {
		t.Fatal(err)
	}
	if !left.Equal(right) {
		t.Fatal("independently compiled equal structures differ")
	}

	var firstBuilder EffectStructureBuilder
	first, err := firstBuilder.Compile(firstBuilder.ForwardPhase(
		"apply",
		firstBuilder.Step("one", EffectStepForwardEffect),
	))
	if err != nil {
		t.Fatal(err)
	}
	var secondBuilder EffectStructureBuilder
	second, err := secondBuilder.Compile(secondBuilder.ForwardPhase(
		"apply",
		secondBuilder.Step("two", EffectStepForwardEffect),
	))
	if err != nil {
		t.Fatal(err)
	}
	if first.Equal(second) {
		t.Fatal("structures with different step references compare equal")
	}
}

func TestEffectCursorConsumesSelectedBranchInOrder(t *testing.T) {
	t.Parallel()
	barrier := mustEffectStep(t, "barrier", EffectStepValidateBarrier)
	external := mustEffectStep(t, "external", EffectStepExternal)
	validate := mustEffectStep(t, "validate", EffectStepValidateDescendant)
	persist := mustEffectStep(t, "persist", EffectStepPersistence)
	observe := mustEffectStep(t, "observe", EffectStepObservation)
	terminal := mustEffectStep(t, "terminal", EffectStepTerminal)
	branch, err := newEffectChoice(
		"attempt-result",
		external,
		EffectSequence(validate, persist),
	)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := newEffectRepeat(2, observe)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(
		EffectSequence(barrier, branch, repeated, terminal),
	)
	if err != nil {
		t.Fatal(err)
	}
	cursor := structure.Begin()

	if err := cursor.Consume("barrier", EffectStepValidateBarrier); err != nil {
		t.Fatal(err)
	}
	if err := cursor.SelectAlternative("attempt-result", 1); err != nil {
		t.Fatal(err)
	}
	for _, step := range []effectStep{
		{id: "validate", kind: EffectStepValidateDescendant},
		{id: "persist", kind: EffectStepPersistence},
		{id: "observe", kind: EffectStepObservation},
		{id: "observe", kind: EffectStepObservation},
		{id: "terminal", kind: EffectStepTerminal},
	} {
		if err := cursor.Consume(step.id, step.kind); err != nil {
			t.Fatalf("Consume %#v: %v", step, err)
		}
	}
	if err := cursor.FinishSuccess(); err != nil {
		t.Fatal(err)
	}
	if err := cursor.FinishSuccess(); err == nil || !strings.Contains(err.Error(), "already finished") {
		t.Fatalf("second finish error = %v", err)
	}
}

func TestEffectCursorClassifiesForwardEffectsWithinEachPhase(t *testing.T) {
	t.Parallel()
	var builder EffectStructureBuilder
	structure, err := builder.Compile(EffectSequence(
		builder.ForwardPhase(
			"provider",
			EffectSequence(
				builder.Step("provider-first", EffectStepForwardEffect),
				builder.Step("provider-second", EffectStepForwardEffect),
			),
		),
		builder.ForwardPhase(
			"final",
			builder.Step("final-first", EffectStepForwardEffect),
		),
	))
	if err != nil {
		t.Fatal(err)
	}

	cursor := structure.Begin()
	for _, test := range []struct {
		id   string
		want ForwardEffectCheckpoint
	}{
		{id: "provider-first", want: ForwardEffectEstablishStateDir},
		{id: "provider-second", want: ForwardEffectValidateStateDir},
		{id: "final-first", want: ForwardEffectEstablishStateDir},
	} {
		got, consumeErr := cursor.ConsumeForwardEffect(test.id)
		if consumeErr != nil {
			t.Fatalf("ConsumeForwardEffect(%q): %v", test.id, consumeErr)
		}
		if got != test.want {
			t.Fatalf("ConsumeForwardEffect(%q) = %d, want %d", test.id, got, test.want)
		}
	}
	if err := cursor.FinishSuccess(); err != nil {
		t.Fatal(err)
	}
}

func TestEffectCursorGenericForwardConsumptionAdvancesPhase(t *testing.T) {
	t.Parallel()
	var builder EffectStructureBuilder
	structure, err := builder.Compile(builder.ForwardPhase(
		"apply",
		EffectSequence(
			builder.Step("first", EffectStepForwardEffect),
			builder.Step("second", EffectStepForwardEffect),
		),
	))
	if err != nil {
		t.Fatal(err)
	}

	cursor := structure.Begin()
	if err := cursor.Consume("first", EffectStepForwardEffect); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := cursor.ConsumeForwardEffect("second")
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint != ForwardEffectValidateStateDir {
		t.Fatalf("second checkpoint = %d, want StateDir validation", checkpoint)
	}
}

func TestEffectCursorRejectsWrongBranchOrderAndUnderConsumption(t *testing.T) {
	t.Parallel()
	first := mustEffectStep(t, "first", EffectStepValidateBarrier)
	left := mustEffectStep(t, "left", EffectStepObservation)
	right := mustEffectStep(t, "right", EffectStepObservation)
	choice, err := newEffectChoice("branch", left, right)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(EffectSequence(first, choice))
	if err != nil {
		t.Fatal(err)
	}

	cursor := structure.Begin()
	if err := cursor.Consume("right", EffectStepObservation); err == nil ||
		!strings.Contains(err.Error(), "next effect step") {
		t.Fatalf("out-of-order error = %v", err)
	}
	if err := cursor.Consume("first", EffectStepValidateBarrier); err != nil {
		t.Fatal(err)
	}
	if err := cursor.Consume("left", EffectStepObservation); err == nil ||
		!strings.Contains(err.Error(), "must be selected") {
		t.Fatalf("unselected-branch error = %v", err)
	}
	if err := cursor.SelectAlternative("other", 0); err == nil ||
		!strings.Contains(err.Error(), "not \"other\"") {
		t.Fatalf("wrong-choice error = %v", err)
	}
	if err := cursor.SelectAlternative("branch", 0); err != nil {
		t.Fatal(err)
	}
	if err := cursor.FinishSuccess(); err == nil || !strings.Contains(err.Error(), "mandatory effect step") {
		t.Fatalf("under-consumption error = %v", err)
	}
	if err := cursor.Consume("left", EffectStepObservation); err != nil {
		t.Fatal(err)
	}
	if err := cursor.Consume("left", EffectStepObservation); err == nil ||
		!strings.Contains(err.Error(), "no remaining step") {
		t.Fatalf("duplicate/extra error = %v", err)
	}
}

func TestEffectCursorAllowsOnlyPreEffectAbort(t *testing.T) {
	t.Parallel()
	barrier := mustEffectStep(t, "barrier", EffectStepValidateBarrier)
	external := mustEffectStep(t, "external", EffectStepExternal)
	structure, err := compileEffectStructure(EffectSequence(barrier, external))
	if err != nil {
		t.Fatal(err)
	}

	before := structure.Begin()
	if err := before.Consume("barrier", EffectStepValidateBarrier); err != nil {
		t.Fatal(err)
	}
	if err := before.AbortBeforeEffect(); err != nil {
		t.Fatal(err)
	}
	if err := before.Consume("external", EffectStepExternal); err == nil ||
		!strings.Contains(err.Error(), "already aborted") {
		t.Fatalf("post-abort Consume error = %v", err)
	}

	after := structure.Begin()
	if err := after.Consume("barrier", EffectStepValidateBarrier); err != nil {
		t.Fatal(err)
	}
	if err := after.Consume("external", EffectStepExternal); err != nil {
		t.Fatal(err)
	}
	if err := after.AbortBeforeEffect(); err == nil || !strings.Contains(err.Error(), "after an effect started") {
		t.Fatalf("post-effect abort error = %v", err)
	}
}

func TestEffectStructureBuilderRetainsFirstConstructionError(t *testing.T) {
	t.Parallel()
	var builder EffectStructureBuilder
	invalid := builder.Step(" ", EffectStepExternal)
	_ = builder.Step("later", EffectStepObservation)
	if _, err := builder.Compile(EffectSequence(invalid)); err == nil ||
		!strings.Contains(err.Error(), "reference is empty") {
		t.Fatalf("builder error = %v", err)
	}

	var valid EffectStructureBuilder
	structure, err := valid.Compile(EffectSequence(
		valid.Step("observe", EffectStepObservation),
		valid.Step("terminal", EffectStepTerminal),
	))
	if err != nil {
		t.Fatal(err)
	}
	cursor := structure.Begin()
	if err := cursor.Consume("observe", EffectStepObservation); err != nil {
		t.Fatal(err)
	}
	if err := cursor.Consume("terminal", EffectStepTerminal); err != nil {
		t.Fatal(err)
	}
	if err := cursor.FinishSuccess(); err != nil {
		t.Fatal(err)
	}
}

func TestEffectStructureRejectsInvalidReferencesAndChoices(t *testing.T) {
	t.Parallel()
	if _, err := newEffectStep(" ", EffectStepExternal); err == nil {
		t.Fatal("empty step reference accepted")
	}
	if _, err := newEffectStep(" step ", EffectStepExternal); err == nil {
		t.Fatal("non-canonical step reference accepted")
	}
	if _, err := newEffectStep("step", 0); err == nil {
		t.Fatal("invalid step kind accepted")
	}
	if _, err := newEffectChoice(
		"single",
		mustEffectStep(t, "only", EffectStepObservation),
	); err == nil {
		t.Fatal("single-alternative choice accepted")
	}
	if _, err := newEffectRepeat(-1, mustEffectStep(t, "repeat", EffectStepObservation)); err == nil {
		t.Fatal("negative repetition accepted")
	}
	forward := mustEffectStep(t, "forward", EffectStepForwardEffect)
	if _, err := compileEffectStructure(forward); err == nil ||
		!strings.Contains(err.Error(), "outside a forward phase") {
		t.Fatalf("unscoped forward effect error = %v", err)
	}
	inner, err := newEffectForwardPhase("inner", forward)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := newEffectForwardPhase("outer", inner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileEffectStructure(outer); err == nil ||
		!strings.Contains(err.Error(), "must not nest") {
		t.Fatalf("nested forward phase error = %v", err)
	}
}

func lowerTestEffectCost(
	node EffectNode,
	stepCost func(EffectStepKind) (int, error),
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
	case effectNodeForwardPhase, effectNodeTrigger, effectNodeConditional:
		return lowerTestEffectCost(node.children[0], stepCost)
	default:
		return 0, nil
	}
}

func mustEffectStep(t *testing.T, id string, kind EffectStepKind) EffectNode {
	t.Helper()
	step, err := newEffectStep(id, kind)
	if err != nil {
		t.Fatal(err)
	}
	return step
}

func TestEffectStructureDemandAlternativesPreserveIncomparableChoices(t *testing.T) {
	t.Parallel()
	barrierOne := mustEffectStep(t, "barrier-1", EffectStepValidateBarrier)
	barrierTwo := mustEffectStep(t, "barrier-2", EffectStepValidateBarrier)
	barrierThree := mustEffectStep(t, "barrier-3", EffectStepValidateBarrier)
	commit := mustEffectStep(t, "commit", EffectStepPublishDescendant)
	choice, err := newEffectChoice(
		"physical-alternatives",
		EffectSequence(barrierOne, barrierTwo, barrierThree),
		commit,
	)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(choice)
	if err != nil {
		t.Fatal(err)
	}

	alternatives, err := structure.DemandAlternatives()
	if err != nil {
		t.Fatal(err)
	}
	if len(alternatives) != 2 {
		t.Fatalf("alternatives = %d, want 2", len(alternatives))
	}
	if alternatives[0].BarrierValidationCalls() != 0 ||
		alternatives[0].DescendantFileCommits() != 1 {
		t.Fatalf("first alternative = %+v, want one descendant commit", alternatives[0])
	}
	if alternatives[1].BarrierValidationCalls() != 3 ||
		alternatives[1].DescendantFileCommits() != 0 {
		t.Fatalf("second alternative = %+v, want three barrier validations", alternatives[1])
	}
}

func TestEffectStructureDemandAlternativesPruneDominatedForwardChoices(t *testing.T) {
	t.Parallel()
	forwardOne := mustEffectStep(t, "forward-1", EffectStepForwardEffect)
	forwardTwo := mustEffectStep(t, "forward-2", EffectStepForwardEffect)
	choice, err := newEffectChoice(
		"forward-alternatives",
		EffectSequence(),
		EffectSequence(forwardOne, forwardTwo),
	)
	if err != nil {
		t.Fatal(err)
	}
	phase, err := newEffectForwardPhase("forward-phase", choice)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(phase)
	if err != nil {
		t.Fatal(err)
	}

	alternatives, err := structure.DemandAlternatives()
	if err != nil {
		t.Fatal(err)
	}
	if len(alternatives) != 1 {
		t.Fatalf("alternatives = %d, want 1 nondominated alternative", len(alternatives))
	}
	if alternatives[0].EnsureCalls() != 1 ||
		alternatives[0].StateDirValidationCalls() != 1 {
		t.Fatalf("alternative = %+v, want ensure=1 StateDir-validation=1", alternatives[0])
	}
}

func TestEffectStructureDemandAlternativesRespectTriggerReachability(t *testing.T) {
	t.Parallel()
	triggerOne, err := newEffectTrigger(
		"ownership",
		mustEffectStep(t, "trigger-forward-1", EffectStepForwardEffect),
	)
	if err != nil {
		t.Fatal(err)
	}
	triggerTwo, err := newEffectTrigger(
		"ownership",
		mustEffectStep(t, "trigger-forward-2", EffectStepForwardEffect),
	)
	if err != nil {
		t.Fatal(err)
	}
	conditional, err := newEffectConditional(
		"ownership",
		mustEffectStep(t, "conditional-forward", EffectStepForwardEffect),
	)
	if err != nil {
		t.Fatal(err)
	}
	firstChoice, err := newEffectChoice("first", EffectSequence(), triggerOne)
	if err != nil {
		t.Fatal(err)
	}
	secondChoice, err := newEffectChoice("second", EffectSequence(), triggerTwo)
	if err != nil {
		t.Fatal(err)
	}
	phase, err := newEffectForwardPhase(
		"forward-phase",
		EffectSequence(firstChoice, secondChoice, conditional),
	)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(phase)
	if err != nil {
		t.Fatal(err)
	}

	alternatives, err := structure.DemandAlternatives()
	if err != nil {
		t.Fatal(err)
	}
	if len(alternatives) != 1 {
		t.Fatalf("alternatives = %d, want one nondominated alternative", len(alternatives))
	}
	if alternatives[0].EnsureCalls() != 1 ||
		alternatives[0].StateDirValidationCalls() != 2 {
		t.Fatalf("alternative = %+v, want three reachable forward effects", alternatives[0])
	}
}

func TestEffectStructureDemandAlternativesRejectLateTriggerOnlyPath(t *testing.T) {
	t.Parallel()
	conditional, err := newEffectConditional(
		"ownership",
		mustEffectStep(t, "conditional", EffectStepValidateBarrier),
	)
	if err != nil {
		t.Fatal(err)
	}
	trigger, err := newEffectTrigger(
		"ownership",
		mustEffectStep(t, "trigger", EffectStepValidateBarrier),
	)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := compileEffectStructure(EffectSequence(conditional, trigger))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := structure.DemandAlternatives(); err == nil ||
		!strings.Contains(err.Error(), "no cursor-reachable") {
		t.Fatalf("late-trigger alternatives error = %v", err)
	}
}

func TestEffectStructureDemandAlternativesBoundNondeterministicRepetition(t *testing.T) {
	t.Parallel()
	var builder EffectStructureBuilder
	choice := builder.Choice(
		"repeated-choice",
		builder.Step("barrier", EffectStepValidateBarrier),
		builder.Step("commit", EffectStepPublishDescendant),
	)
	repeated := builder.Repeat(maximumEffectRepeatIterations+1, choice)
	structure, err := builder.Compile(repeated)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := structure.DemandAlternatives(); err == nil ||
		!strings.Contains(err.Error(), "nondeterministic effect repetition") {
		t.Fatalf("repetition bound error = %v", err)
	}
}
