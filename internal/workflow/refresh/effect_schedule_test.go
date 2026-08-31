package refresh

import (
	"testing"

	"github.com/isty2e/daem/internal/operationplan"
)

func TestRefreshEffectStructureMatchesLegacyReservationDemand(t *testing.T) {
	t.Parallel()
	structure, err := compileRefreshEffectStructure()
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := structure.LegacyDemand()
	if err != nil {
		t.Fatal(err)
	}
	legacy := operationplan.CompileRefresh("/state.json").Demand()
	if compiled.EnsureCalls() != legacy.EnsureCalls() ||
		compiled.BarrierValidationCalls() != legacy.BarrierValidationCalls() ||
		compiled.StateDirValidationCalls() != legacy.StateDirValidationCalls() ||
		compiled.DescendantValidations() != legacy.DescendantValidations() ||
		compiled.DescendantFileCommits() != legacy.DescendantFileCommits() {
		t.Fatalf("compiled demand = %#v, legacy = %#v", compiled, legacy)
	}
}

func TestRefreshEffectStructureCoversRuntimeBranches(t *testing.T) {
	t.Parallel()
	structure, err := compileRefreshEffectStructure()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("command not started", func(t *testing.T) {
		cursor := structure.Begin()
		consumeRefreshStep(t, cursor, "refresh/pre-attempt-barrier", operationplan.EffectStepValidateBarrier)
		if err := cursor.SelectAlternative(refreshAttemptStartChoice, 0); err != nil {
			t.Fatal(err)
		}
		consumeRefreshStep(t, cursor, "refresh/not-started-observation", operationplan.EffectStepObservation)
		consumeRefreshStep(t, cursor, "refresh/not-started-terminal", operationplan.EffectStepTerminal)
		if err := cursor.FinishSuccess(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("started but unpersisted", func(t *testing.T) {
		cursor := structure.Begin()
		consumeRefreshStartedPrefix(t, cursor)
		if err := cursor.SelectAlternative(refreshPersistenceChoice, 0); err != nil {
			t.Fatal(err)
		}
		consumeRefreshStep(t, cursor, "refresh/unpersisted-terminal", operationplan.EffectStepTerminal)
		if err := cursor.FinishSuccess(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("started and persisted", func(t *testing.T) {
		cursor := structure.Begin()
		consumeRefreshStartedPrefix(t, cursor)
		if err := cursor.SelectAlternative(refreshPersistenceChoice, 1); err != nil {
			t.Fatal(err)
		}
		for _, step := range []struct {
			id   string
			kind operationplan.EffectStepKind
		}{
			{id: "refresh/persistence-establish-state-dir", kind: operationplan.EffectStepEstablishStateDir},
			{id: "refresh/pre-persistence-barrier", kind: operationplan.EffectStepValidateBarrier},
			{id: "refresh/pre-persistence-descendant", kind: operationplan.EffectStepValidateDescendant},
			{id: "refresh/publish-attempt", kind: operationplan.EffectStepPublishDescendant},
			{id: "refresh/post-persistence-descendant", kind: operationplan.EffectStepValidateDescendant},
			{id: "refresh/post-persistence-barrier", kind: operationplan.EffectStepValidateBarrier},
			{id: "refresh/persistence-settlement", kind: operationplan.EffectStepPersistence},
			{id: "refresh/persisted-terminal", kind: operationplan.EffectStepTerminal},
		} {
			consumeRefreshStep(t, cursor, step.id, step.kind)
		}
		if err := cursor.FinishSuccess(); err != nil {
			t.Fatal(err)
		}
	})
}

func consumeRefreshStartedPrefix(t *testing.T, cursor *operationplan.EffectCursor) {
	t.Helper()
	consumeRefreshStep(t, cursor, "refresh/pre-attempt-barrier", operationplan.EffectStepValidateBarrier)
	if err := cursor.SelectAlternative(refreshAttemptStartChoice, 1); err != nil {
		t.Fatal(err)
	}
	consumeRefreshStep(t, cursor, "refresh/started-external", operationplan.EffectStepExternal)
	consumeRefreshStep(t, cursor, "refresh/post-attempt-barrier", operationplan.EffectStepValidateBarrier)
	consumeRefreshStep(t, cursor, "refresh/started-observation", operationplan.EffectStepObservation)
}

func consumeRefreshStep(
	t *testing.T,
	cursor *operationplan.EffectCursor,
	id string,
	kind operationplan.EffectStepKind,
) {
	t.Helper()
	if err := cursor.Consume(id, kind); err != nil {
		t.Fatalf("consume %s: %v", id, err)
	}
}
