package build

import (
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

// EventKind identifies a lock/build-owned progress fact.
type EventKind string

// EventStage identifies the closed lock/build stage axis.
type EventStage string

const (
	EventSkillGroupListStarted    EventKind = "skill_group_list_started"
	EventSkillGroupExpanded       EventKind = "skill_group_expanded"
	EventSkillGroupListFailed     EventKind = "skill_group_list_failed"
	EventResourceResolveStarted   EventKind = "resource_resolve_started"
	EventResourceResolved         EventKind = "resource_resolved"
	EventResourceResolveFailed    EventKind = "resource_resolve_failed"
	EventResourceLocked           EventKind = "resource_locked"
	EventResourceLockFailed       EventKind = "resource_lock_failed"
	EventSnapshotValidated        EventKind = "snapshot_validated"
	EventSnapshotValidationFailed EventKind = "snapshot_validation_failed"
)

const (
	EventStageSkillGroupRoot EventStage = "skill_group_root"
	EventStageSkill          EventStage = "skill"
	EventStageHookAsset      EventStage = "hook_asset"
	EventStageInstructions   EventStage = "instructions"
	EventStageSnapshot       EventStage = "snapshot"
)

// Event is a lock/build-owned progress fact.
type Event struct {
	Kind            EventKind
	TaskID          acquisition.RequestID
	Stage           EventStage
	Ordinal         int
	EntityID        entity.ID
	SourceID        artifact.SourceID
	SkillGroupIndex *int
	Count           int
	Err             error
}

// EventSink observes lock/build events. Nil sinks are no-ops.
type EventSink func(Event)

// Emit emits event when sink is non-nil.
func (sink EventSink) Emit(event Event) {
	if sink != nil {
		sink(event)
	}
}
