package refresh

import "github.com/isty2e/daem/internal/operationplan"

const (
	refreshAttemptStartChoice = "refresh-attempt-start"
	refreshPersistenceChoice  = "refresh-attempt-persistence"
)

func compileRefreshEffectStructure() (operationplan.EffectStructure, error) {
	var builder operationplan.EffectStructureBuilder
	persistence := builder.Choice(
		refreshPersistenceChoice,
		builder.Step("refresh/unpersisted-terminal", operationplan.EffectStepTerminal),
		operationplan.EffectSequence(
			builder.Step(
				"refresh/persistence-establish-state-dir",
				operationplan.EffectStepEstablishStateDir,
			),
			builder.Step(
				"refresh/pre-persistence-barrier",
				operationplan.EffectStepValidateBarrier,
			),
			builder.Step(
				"refresh/pre-persistence-descendant",
				operationplan.EffectStepValidateDescendant,
			),
			builder.Step("refresh/publish-attempt", operationplan.EffectStepPublishDescendant),
			builder.Step(
				"refresh/post-persistence-descendant",
				operationplan.EffectStepValidateDescendant,
			),
			builder.Step(
				"refresh/post-persistence-barrier",
				operationplan.EffectStepValidateBarrier,
			),
			builder.Step("refresh/persistence-settlement", operationplan.EffectStepPersistence),
			builder.Step("refresh/persisted-terminal", operationplan.EffectStepTerminal),
		),
	)
	attempt := builder.Choice(
		refreshAttemptStartChoice,
		operationplan.EffectSequence(
			builder.Step("refresh/not-started-observation", operationplan.EffectStepObservation),
			builder.Step("refresh/not-started-terminal", operationplan.EffectStepTerminal),
		),
		operationplan.EffectSequence(
			builder.Step("refresh/started-external", operationplan.EffectStepExternal),
			builder.Step("refresh/post-attempt-barrier", operationplan.EffectStepValidateBarrier),
			builder.Step("refresh/started-observation", operationplan.EffectStepObservation),
			persistence,
		),
	)
	return builder.Compile(operationplan.EffectSequence(
		builder.Step("refresh/pre-attempt-barrier", operationplan.EffectStepValidateBarrier),
		attempt,
	))
}
