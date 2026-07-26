package lock

import (
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

// ProgressEvent carries lock workflow progress facts for command boundary subscribers.
type ProgressEvent struct {
	Kind            string
	TaskID          acquisition.RequestID
	Stage           string
	Ordinal         int
	EntityID        entity.ID
	SkillGroupIndex *int
	Count           int
	Err             error
}

// ProgressEventSink observes lock workflow progress events. Nil sinks are no-ops.
type ProgressEventSink func(ProgressEvent)

func (sink ProgressEventSink) emit(event ProgressEvent) {
	if sink != nil {
		sink(event)
	}
}
