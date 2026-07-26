package cli

import (
	"io"

	cliprogress "github.com/isty2e/daem/internal/cli/present/progress"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func newLockProgressRenderer(jsonOutput bool, stderr io.Writer, options commandOptions) *cliprogress.LockProgressRenderer {
	if jsonOutput || !options.stderrIsTerminal || stderr == nil {
		return nil
	}

	return cliprogress.NewLockProgressRenderer(cliprogress.LockProgressRendererOptions{Output: stderr})
}

func lockWorkflowProgressSink(renderer *cliprogress.LockProgressRenderer) workflowlock.ProgressEventSink {
	if renderer == nil {
		return nil
	}
	sink := renderer.LockSink()
	return func(event workflowlock.ProgressEvent) {
		sink(cliprogress.LockEvent{
			Kind:            event.Kind,
			TaskID:          event.TaskID,
			Stage:           event.Stage,
			Ordinal:         event.Ordinal,
			EntityID:        event.EntityID,
			SkillGroupIndex: event.SkillGroupIndex,
			Count:           event.Count,
			Err:             event.Err,
		})
	}
}

func newApplyProgressRenderer(jsonOutput bool, stderr io.Writer, options commandOptions) *cliprogress.ApplyProgressRenderer {
	if jsonOutput || !options.stderrIsTerminal || stderr == nil {
		return nil
	}

	return cliprogress.NewApplyProgressRenderer(cliprogress.ApplyProgressRendererOptions{Output: stderr})
}
