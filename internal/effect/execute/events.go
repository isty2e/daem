package execute

import (
	"github.com/isty2e/daem/internal/output"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// EventKind identifies an execute-owned apply progress fact.
type EventKind string

// EventStage identifies the closed apply execution phase axis.
type EventStage string

const (
	EventJournalCaptureStarted EventKind = "journal_capture_started"
	EventJournalCaptured       EventKind = "journal_captured"
	EventJournalCaptureFailed  EventKind = "journal_capture_failed"

	EventRollbackStageStarted EventKind = "rollback_stage_started"
	EventRollbackStaged       EventKind = "rollback_staged"
	EventRollbackStageFailed  EventKind = "rollback_stage_failed"

	EventActionStarted EventKind = "action_started"
	EventActionDone    EventKind = "action_done"
	EventActionFailed  EventKind = "action_failed"

	EventRelationOrderStarted EventKind = "relation_order_started"
	EventRelationOrderDone    EventKind = "relation_order_done"
	EventRelationOrderFailed  EventKind = "relation_order_failed"

	EventStatefileWriteStarted EventKind = "statefile_write_started"
	EventStatefileWritten      EventKind = "statefile_written"
	EventStatefileWriteFailed  EventKind = "statefile_write_failed"

	EventRollbackRestoreStarted EventKind = "rollback_restore_started"
	EventRollbackRestored       EventKind = "rollback_restored"
	EventRollbackRestoreFailed  EventKind = "rollback_restore_failed"

	EventJournalCleanupStarted EventKind = "journal_cleanup_started"
	EventJournalCleaned        EventKind = "journal_cleaned"
	EventJournalCleanupFailed  EventKind = "journal_cleanup_failed"
)

const (
	EventStageJournalCapture  EventStage = "journal_capture"
	EventStageRollbackStage   EventStage = "rollback_stage"
	EventStageAction          EventStage = "action"
	EventStageRelationOrder   EventStage = "relation_order"
	EventStageStatefileWrite  EventStage = "statefile_write"
	EventStageRollbackRestore EventStage = "rollback_restore"
	EventStageJournalCleanup  EventStage = "journal_cleanup"
)

// ActionEventFacts is the execute-owned action projection for apply progress.
type ActionEventFacts struct {
	Index           int
	ManagedPathKind ManagedPathEffectKind
	AggregateKind   AggregateEffectKind
	Subject         topology.SubjectID
	Target          target.Target
	ConsumerTargets []target.Target
	Scope           target.Scope
	Destination     output.Destination
}

// RelationOrderEventFacts identifies one independently mutable extension
// sequence without exposing host-native document values.
type RelationOrderEventFacts struct {
	Target     target.Target
	Scope      target.Scope
	SequenceID hostrelation.PhysicalSequenceID
	Changed    bool
}

func managedPathEventFacts(index int, effect ManagedPathEffect) ActionEventFacts {
	return ActionEventFacts{
		Index: index, ManagedPathKind: effect.Kind(), Subject: effect.Subject(),
		ConsumerTargets: effect.ConsumerTargets(), Scope: effect.Scope(), Destination: effect.Destination(),
	}
}

func cloneActionEventFacts(facts ActionEventFacts) *ActionEventFacts {
	cloned := facts
	return &cloned
}

// Event is an execute-owned apply progress fact.
type Event struct {
	Kind          EventKind
	Stage         EventStage
	Action        *ActionEventFacts
	RelationOrder *RelationOrderEventFacts
	TotalActions  int
	Err           error
}

// EventSink observes apply execution events. Nil sinks are no-ops.
type EventSink func(Event)

// Emit emits event when sink is non-nil.
func (sink EventSink) Emit(event Event) {
	if sink != nil {
		sink(event)
	}
}
