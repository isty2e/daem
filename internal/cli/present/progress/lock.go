package progress

import (
	"fmt"
	"io"
	"sync"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

// LockProgressRendererOptions configures lock/outdated human progress rendering.
type LockProgressRendererOptions struct {
	Output io.Writer
}

// LockEvent carries present-owned lock progress facts.
type LockEvent struct {
	Kind            string
	TaskID          acquisition.RequestID
	Stage           string
	Ordinal         int
	EntityID        entity.ID
	SkillGroupIndex *int
	Count           int
	Err             error
}

// LockEventSink observes lock progress events. Nil sinks are no-ops.
type LockEventSink func(LockEvent)

// LockProgressRenderer renders lock/source progress facts as line-oriented human text.
type LockProgressRenderer struct {
	mu           sync.Mutex
	line         ephemeralLine
	labelsByTask map[acquisition.RequestID]string
	completed    int
	finished     map[string]struct{}
}

// NewLockProgressRenderer constructs a concurrency-safe progress renderer.
func NewLockProgressRenderer(options LockProgressRendererOptions) *LockProgressRenderer {
	return &LockProgressRenderer{
		line:         newEphemeralLine(options.Output),
		labelsByTask: make(map[acquisition.RequestID]string),
		finished:     make(map[string]struct{}),
	}
}

// SourceSink returns the source event sink used by lock/outdated workflows.
func (renderer *LockProgressRenderer) SourceSink() acquisition.EventSink {
	if renderer == nil {
		return nil
	}
	return renderer.renderSourceEvent
}

// LockSink returns the lock progress sink used by lock/outdated workflows.
func (renderer *LockProgressRenderer) LockSink() LockEventSink {
	if renderer == nil {
		return nil
	}
	return renderer.renderLockEvent
}

// Close clears any active ephemeral progress line.
func (renderer *LockProgressRenderer) Close() {
	if renderer == nil {
		return
	}
	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	renderer.line.close()
}

func (renderer *LockProgressRenderer) renderSourceEvent(event acquisition.Event) {
	if renderer == nil {
		return
	}

	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	if event.Kind() != acquisition.EventStarted && event.Kind() != acquisition.EventCompleted && event.Kind() != acquisition.EventFailed {
		return
	}
	label := renderer.labelsByTask[event.Request().ID()]
	line := fmt.Sprintf("Resolving sources %d", renderer.completed)
	if label != "" {
		line += ": " + escapeText(label)
	}
	if event.Kind() == acquisition.EventFailed {
		line += ": failed"
	}
	renderer.line.write(line)
}

func (renderer *LockProgressRenderer) renderLockEvent(event LockEvent) {
	if renderer == nil {
		return
	}

	label := lockEventLabel(event)
	renderer.mu.Lock()
	defer renderer.mu.Unlock()

	if label != "" && event.TaskID != "" {
		renderer.labelsByTask[event.TaskID] = label
	}
	if event.Kind == "resource_locked" || event.Kind == "skill_group_expanded" {
		completionKey := string(event.TaskID)
		if completionKey == "" {
			completionKey = label
		}
		if _, exists := renderer.finished[completionKey]; !exists {
			renderer.finished[completionKey] = struct{}{}
			renderer.completed++
		}
	}
	line := fmt.Sprintf("Resolving sources %d", renderer.completed)
	if label != "" {
		line += ": " + escapeText(label)
	}
	if event.Err != nil {
		line += ": failed"
	}
	renderer.line.write(line)
}

func lockEventLabel(event LockEvent) string {
	if event.EntityID != (entity.ID{}) {
		return string(event.EntityID.Kind()) + "/" + event.EntityID.Name()
	}
	if event.SkillGroupIndex != nil {
		return fmt.Sprintf("skill_group[%d]", *event.SkillGroupIndex)
	}
	return ""
}
