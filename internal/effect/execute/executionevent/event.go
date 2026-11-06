package executionevent

import (
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type (
	Kind  string
	Stage string
)

const (
	JournalCaptureStarted Kind = "journal_capture_started"
	JournalCaptured       Kind = "journal_captured"
	JournalCaptureFailed  Kind = "journal_capture_failed"

	RollbackStageStarted Kind = "rollback_stage_started"
	RollbackStaged       Kind = "rollback_staged"
	RollbackStageFailed  Kind = "rollback_stage_failed"

	ActionStarted Kind = "action_started"
	ActionDone    Kind = "action_done"
	ActionFailed  Kind = "action_failed"

	RelationOrderStarted Kind = "relation_order_started"
	RelationOrderDone    Kind = "relation_order_done"
	RelationOrderFailed  Kind = "relation_order_failed"

	StatefileWriteStarted Kind = "statefile_write_started"
	StatefileWritten      Kind = "statefile_written"
	StatefileWriteFailed  Kind = "statefile_write_failed"

	RollbackRestoreStarted Kind = "rollback_restore_started"
	RollbackRestored       Kind = "rollback_restored"
	RollbackRestoreFailed  Kind = "rollback_restore_failed"

	JournalCleanupStarted Kind = "journal_cleanup_started"
	JournalCleaned        Kind = "journal_cleaned"
	JournalCleanupFailed  Kind = "journal_cleanup_failed"
)

const (
	StageJournalCapture  Stage = "journal_capture"
	StageRollbackStage   Stage = "rollback_stage"
	StageAction          Stage = "action"
	StageRelationOrder   Stage = "relation_order"
	StageStatefileWrite  Stage = "statefile_write"
	StageRollbackRestore Stage = "rollback_restore"
	StageJournalCleanup  Stage = "journal_cleanup"
)

type ActionFacts struct {
	Index           int
	ManagedPathKind effectpreparation.ManagedPathEffectKind
	AggregateKind   effectpreparation.AggregateEffectKind
	Subject         topology.SubjectID
	Target          target.Target
	ConsumerTargets []target.Target
	Scope           target.Scope
	Destination     output.Destination
}

// RelationOrderFacts identifies an independently mutable extension sequence,
// without exposing host-native document values.
type RelationOrderFacts struct {
	Target     target.Target
	Scope      target.Scope
	SequenceID hostrelation.PhysicalSequenceID
	Changed    bool
}

func ManagedPathFacts(index int, effect effectpreparation.ManagedPathEffect) ActionFacts {
	return ActionFacts{
		Index: index, ManagedPathKind: effect.Kind(), Subject: effect.Subject(),
		ConsumerTargets: effect.ConsumerTargets(), Scope: effect.Scope(), Destination: effect.Destination(),
	}
}

func AggregateFacts(index int, kind effectpreparation.AggregateEffectKind, subject topology.SubjectID, contract aggregate.ProjectionContract) ActionFacts {
	return ActionFacts{
		Index: index, AggregateKind: kind, Subject: subject,
		Target: contract.Address().Document().Target(), Scope: contract.Address().Document().Scope(),
		Destination: contract.Address().Document().AggregateRoot(),
	}
}

func (facts ActionFacts) Snapshot() *ActionFacts {
	cloned := facts
	return &cloned
}

type Event struct {
	Kind          Kind
	Stage         Stage
	Action        *ActionFacts
	RelationOrder *RelationOrderFacts
	TotalActions  int
	Err           error
}

// Sink observes execution events. Nil sinks are no-ops.
type Sink func(Event)

func (sink Sink) Emit(event Event) {
	if sink != nil {
		sink(event)
	}
}

type Emitter struct {
	sink         Sink
	totalActions int
}

func NewEmitter(sink Sink, totalActions int) Emitter {
	return Emitter{sink: sink, totalActions: totalActions}
}

func (emitter Emitter) Emit(kind Kind, stage Stage, action *ActionFacts, err error) {
	emitter.sink.Emit(Event{
		Kind: kind, Stage: stage, Action: action,
		TotalActions: emitter.totalActions, Err: err,
	})
}
