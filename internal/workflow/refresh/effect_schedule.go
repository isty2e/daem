package refresh

import "github.com/isty2e/daem/internal/operationplan"

const (
	refreshAttemptOutcomeChoice                = "refresh-attempt-outcome"
	refreshNotStartedClassificationChoice      = "refresh-not-started-classification"
	refreshStartedClassificationChoice         = "refresh-started-classification"
	refreshPersistenceChoice                   = "refresh-attempt-persistence"
	refreshPersistenceAuthorityChoice          = "refresh-persistence-authority"
	refreshPrePersistenceBarrierChoice         = "refresh-pre-persistence-barrier-result"
	refreshPrePersistenceDescendantChoice      = "refresh-pre-persistence-descendant-result"
	refreshPublicationChoice                   = "refresh-publication-result"
	refreshPostPersistenceDescendantChoice     = "refresh-post-persistence-descendant-result"
	refreshPostPersistenceBarrierChoice        = "refresh-post-persistence-barrier-result"
	refreshStepPreAttemptBarrier               = "refresh/pre-attempt-barrier"
	refreshStepInvokeExternal                  = "refresh/invoke-external"
	refreshStepNotStartedObservation           = "refresh/not-started-observation"
	refreshStepNotStartedClassificationFailed  = "refresh/not-started-classification-failed"
	refreshStepNotStartedTerminal              = "refresh/not-started-terminal"
	refreshStepPostAttemptBarrier              = "refresh/post-attempt-barrier"
	refreshStepStartedObservation              = "refresh/started-observation"
	refreshStepStartedClassificationFailed     = "refresh/started-classification-failed"
	refreshStepUnpersistedTerminal             = "refresh/unpersisted-terminal"
	refreshStepPersistenceEstablishStateDir    = "refresh/persistence-establish-state-dir"
	refreshStepPersistenceAuthorityFailed      = "refresh/persistence-authority-failed"
	refreshStepPrePersistenceBarrier           = "refresh/pre-persistence-barrier"
	refreshStepPrePersistenceBarrierFailed     = "refresh/pre-persistence-barrier-failed"
	refreshStepPrePersistenceDescendant        = "refresh/pre-persistence-descendant"
	refreshStepPrePersistenceDescendantFailed  = "refresh/pre-persistence-descendant-failed"
	refreshStepPublishAttempt                  = "refresh/publish-attempt"
	refreshStepPublicationFailed               = "refresh/publication-failed"
	refreshStepPostPersistenceDescendant       = "refresh/post-persistence-descendant"
	refreshStepPostPersistenceDescendantFailed = "refresh/post-persistence-descendant-failed"
	refreshStepPostPersistenceBarrier          = "refresh/post-persistence-barrier"
	refreshStepPostPersistenceBarrierFailed    = "refresh/post-persistence-barrier-failed"
	refreshStepPersistenceSettlement           = "refresh/persistence-settlement"
	refreshStepPersistedTerminal               = "refresh/persisted-terminal"
)

func compileRefreshEffectStructure() (operationplan.EffectStructure, error) {
	var builder operationplan.EffectStructureBuilder
	persisted := operationplan.EffectSequence(
		builder.Step(
			refreshStepPersistenceSettlement,
			operationplan.EffectStepPersistence,
		),
		builder.Step(refreshStepPersistedTerminal, operationplan.EffectStepTerminal),
	)
	postPersistenceBarrier := operationplan.EffectSequence(
		builder.Step(
			refreshStepPostPersistenceBarrier,
			operationplan.EffectStepValidateBarrier,
		),
		builder.Choice(
			refreshPostPersistenceBarrierChoice,
			builder.Step(
				refreshStepPostPersistenceBarrierFailed,
				operationplan.EffectStepTerminal,
			),
			persisted,
		),
	)
	postPersistenceDescendant := operationplan.EffectSequence(
		builder.Step(
			refreshStepPostPersistenceDescendant,
			operationplan.EffectStepValidateDescendant,
		),
		builder.Choice(
			refreshPostPersistenceDescendantChoice,
			builder.Step(
				refreshStepPostPersistenceDescendantFailed,
				operationplan.EffectStepTerminal,
			),
			postPersistenceBarrier,
		),
	)
	publication := operationplan.EffectSequence(
		builder.Step(
			refreshStepPublishAttempt,
			operationplan.EffectStepPublishDescendant,
		),
		builder.Choice(
			refreshPublicationChoice,
			builder.Step(refreshStepPublicationFailed, operationplan.EffectStepTerminal),
			postPersistenceDescendant,
		),
	)
	prePersistenceDescendant := operationplan.EffectSequence(
		builder.Step(
			refreshStepPrePersistenceDescendant,
			operationplan.EffectStepValidateDescendant,
		),
		builder.Choice(
			refreshPrePersistenceDescendantChoice,
			builder.Step(
				refreshStepPrePersistenceDescendantFailed,
				operationplan.EffectStepTerminal,
			),
			publication,
		),
	)
	prePersistenceBarrier := operationplan.EffectSequence(
		builder.Step(
			refreshStepPrePersistenceBarrier,
			operationplan.EffectStepValidateBarrier,
		),
		builder.Choice(
			refreshPrePersistenceBarrierChoice,
			builder.Step(
				refreshStepPrePersistenceBarrierFailed,
				operationplan.EffectStepTerminal,
			),
			prePersistenceDescendant,
		),
	)
	persistenceAuthority := operationplan.EffectSequence(
		builder.Step(
			refreshStepPersistenceEstablishStateDir,
			operationplan.EffectStepEstablishStateDir,
		),
		builder.Choice(
			refreshPersistenceAuthorityChoice,
			builder.Step(
				refreshStepPersistenceAuthorityFailed,
				operationplan.EffectStepTerminal,
			),
			prePersistenceBarrier,
		),
	)
	persistence := builder.Choice(
		refreshPersistenceChoice,
		builder.Step(refreshStepUnpersistedTerminal, operationplan.EffectStepTerminal),
		persistenceAuthority,
	)
	started := operationplan.EffectSequence(
		builder.Step(
			refreshStepPostAttemptBarrier,
			operationplan.EffectStepValidateBarrier,
		),
		builder.Step(refreshStepStartedObservation, operationplan.EffectStepObservation),
		builder.Choice(
			refreshStartedClassificationChoice,
			builder.Step(
				refreshStepStartedClassificationFailed,
				operationplan.EffectStepTerminal,
			),
			persistence,
		),
	)
	notStarted := operationplan.EffectSequence(
		builder.Step(refreshStepNotStartedObservation, operationplan.EffectStepObservation),
		builder.Choice(
			refreshNotStartedClassificationChoice,
			builder.Step(
				refreshStepNotStartedClassificationFailed,
				operationplan.EffectStepTerminal,
			),
			builder.Step(refreshStepNotStartedTerminal, operationplan.EffectStepTerminal),
		),
	)
	return builder.Compile(operationplan.EffectSequence(
		builder.Step(refreshStepPreAttemptBarrier, operationplan.EffectStepValidateBarrier),
		builder.Step(refreshStepInvokeExternal, operationplan.EffectStepExternal),
		builder.Choice(refreshAttemptOutcomeChoice, notStarted, started),
	))
}
