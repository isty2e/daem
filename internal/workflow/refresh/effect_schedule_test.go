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
		consumeRefreshAttemptPrefix(t, cursor)
		if err := cursor.SelectAlternative(refreshAttemptOutcomeChoice, 0); err != nil {
			t.Fatal(err)
		}
		consumeRefreshStep(t, cursor, refreshStepNotStartedObservation, operationplan.EffectStepObservation)
		if err := cursor.SelectAlternative(refreshNotStartedClassificationChoice, 1); err != nil {
			t.Fatal(err)
		}
		consumeRefreshStep(t, cursor, refreshStepNotStartedTerminal, operationplan.EffectStepTerminal)
		if err := cursor.FinishSuccess(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("command not started and classification failed", func(t *testing.T) {
		cursor := structure.Begin()
		consumeRefreshAttemptPrefix(t, cursor)
		if err := cursor.SelectAlternative(refreshAttemptOutcomeChoice, 0); err != nil {
			t.Fatal(err)
		}
		consumeRefreshStep(t, cursor, refreshStepNotStartedObservation, operationplan.EffectStepObservation)
		if err := cursor.SelectAlternative(refreshNotStartedClassificationChoice, 0); err != nil {
			t.Fatal(err)
		}
		consumeRefreshStep(t, cursor, refreshStepNotStartedClassificationFailed, operationplan.EffectStepTerminal)
		if err := cursor.FinishSuccess(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("started and classification failed", func(t *testing.T) {
		cursor := structure.Begin()
		consumeRefreshStartedObservation(t, cursor)
		if err := cursor.SelectAlternative(refreshStartedClassificationChoice, 0); err != nil {
			t.Fatal(err)
		}
		consumeRefreshStep(t, cursor, refreshStepStartedClassificationFailed, operationplan.EffectStepTerminal)
		if err := cursor.FinishSuccess(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("started but persistence not attempted", func(t *testing.T) {
		cursor := structure.Begin()
		consumeRefreshPersistencePrefix(t, cursor)
		if err := cursor.SelectAlternative(refreshPersistenceChoice, 0); err != nil {
			t.Fatal(err)
		}
		consumeRefreshStep(t, cursor, refreshStepUnpersistedTerminal, operationplan.EffectStepTerminal)
		if err := cursor.FinishSuccess(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("started and persisted", func(t *testing.T) {
		cursor := structure.Begin()
		consumeRefreshPersistencePrefix(t, cursor)
		if err := cursor.SelectAlternative(refreshPersistenceChoice, 1); err != nil {
			t.Fatal(err)
		}
		for _, checkpoint := range refreshPersistenceCheckpoints() {
			consumeRefreshStep(t, cursor, checkpoint.stepID, checkpoint.kind)
			if err := cursor.SelectAlternative(checkpoint.choiceID, 1); err != nil {
				t.Fatal(err)
			}
		}
		consumeRefreshStep(t, cursor, refreshStepPersistenceSettlement, operationplan.EffectStepPersistence)
		consumeRefreshStep(t, cursor, refreshStepPersistedTerminal, operationplan.EffectStepTerminal)
		if err := cursor.FinishSuccess(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRefreshEffectStructureCoversEveryPersistencePrefixFailure(t *testing.T) {
	t.Parallel()
	structure, err := compileRefreshEffectStructure()
	if err != nil {
		t.Fatal(err)
	}
	checkpoints := refreshPersistenceCheckpoints()
	for failureIndex, failure := range checkpoints {
		t.Run(failure.choiceID, func(t *testing.T) {
			cursor := structure.Begin()
			consumeRefreshPersistencePrefix(t, cursor)
			if err := cursor.SelectAlternative(refreshPersistenceChoice, 1); err != nil {
				t.Fatal(err)
			}
			for index, checkpoint := range checkpoints[:failureIndex+1] {
				consumeRefreshStep(t, cursor, checkpoint.stepID, checkpoint.kind)
				alternative := 1
				if index == failureIndex {
					alternative = 0
				}
				if err := cursor.SelectAlternative(checkpoint.choiceID, alternative); err != nil {
					t.Fatal(err)
				}
			}
			consumeRefreshStep(t, cursor, failure.failureTerminalID, operationplan.EffectStepTerminal)
			if err := cursor.FinishSuccess(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type refreshPersistenceCheckpoint struct {
	stepID            string
	kind              operationplan.EffectStepKind
	choiceID          string
	failureTerminalID string
}

func refreshPersistenceCheckpoints() []refreshPersistenceCheckpoint {
	return []refreshPersistenceCheckpoint{
		{
			stepID:            refreshStepPersistenceEstablishStateDir,
			kind:              operationplan.EffectStepEstablishStateDir,
			choiceID:          refreshPersistenceAuthorityChoice,
			failureTerminalID: refreshStepPersistenceAuthorityFailed,
		},
		{
			stepID:            refreshStepPrePersistenceBarrier,
			kind:              operationplan.EffectStepValidateBarrier,
			choiceID:          refreshPrePersistenceBarrierChoice,
			failureTerminalID: refreshStepPrePersistenceBarrierFailed,
		},
		{
			stepID:            refreshStepPrePersistenceDescendant,
			kind:              operationplan.EffectStepValidateDescendant,
			choiceID:          refreshPrePersistenceDescendantChoice,
			failureTerminalID: refreshStepPrePersistenceDescendantFailed,
		},
		{
			stepID:            refreshStepPublishAttempt,
			kind:              operationplan.EffectStepPublishDescendant,
			choiceID:          refreshPublicationChoice,
			failureTerminalID: refreshStepPublicationFailed,
		},
		{
			stepID:            refreshStepPostPersistenceDescendant,
			kind:              operationplan.EffectStepValidateDescendant,
			choiceID:          refreshPostPersistenceDescendantChoice,
			failureTerminalID: refreshStepPostPersistenceDescendantFailed,
		},
		{
			stepID:            refreshStepPostPersistenceBarrier,
			kind:              operationplan.EffectStepValidateBarrier,
			choiceID:          refreshPostPersistenceBarrierChoice,
			failureTerminalID: refreshStepPostPersistenceBarrierFailed,
		},
	}
}

func consumeRefreshAttemptPrefix(t *testing.T, cursor *operationplan.EffectCursor) {
	t.Helper()
	consumeRefreshStep(t, cursor, refreshStepPreAttemptBarrier, operationplan.EffectStepValidateBarrier)
	consumeRefreshStep(t, cursor, refreshStepInvokeExternal, operationplan.EffectStepExternal)
}

func consumeRefreshStartedObservation(t *testing.T, cursor *operationplan.EffectCursor) {
	t.Helper()
	consumeRefreshAttemptPrefix(t, cursor)
	if err := cursor.SelectAlternative(refreshAttemptOutcomeChoice, 1); err != nil {
		t.Fatal(err)
	}
	consumeRefreshStep(t, cursor, refreshStepPostAttemptBarrier, operationplan.EffectStepValidateBarrier)
	consumeRefreshStep(t, cursor, refreshStepStartedObservation, operationplan.EffectStepObservation)
}

func consumeRefreshPersistencePrefix(t *testing.T, cursor *operationplan.EffectCursor) {
	t.Helper()
	consumeRefreshStartedObservation(t, cursor)
	if err := cursor.SelectAlternative(refreshStartedClassificationChoice, 1); err != nil {
		t.Fatal(err)
	}
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
