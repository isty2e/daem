package lock

import lockbuild "github.com/isty2e/daem/internal/realization/lock/build"

func lockBuildProgressSink(sink ProgressEventSink) lockbuild.EventSink {
	if sink == nil {
		return nil
	}
	return func(event lockbuild.Event) {
		sink.emit(ProgressEvent{
			Kind:            string(event.Kind),
			TaskID:          event.TaskID,
			Stage:           string(event.Stage),
			Ordinal:         event.Ordinal,
			EntityID:        event.EntityID,
			SkillGroupIndex: event.SkillGroupIndex,
			Count:           event.Count,
			Err:             event.Err,
		})
	}
}
