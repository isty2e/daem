package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/execute/executionevent"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// ApplyProgressRendererOptions configures apply human progress rendering.
type ApplyProgressRendererOptions struct {
	Output io.Writer
}

// ApplyProgressRenderer renders execute apply progress facts as line-oriented human text.
type ApplyProgressRenderer struct {
	mu        sync.Mutex
	line      ephemeralLine
	complete  int
	finished  map[int]struct{}
	reference func(entity.Kind, string) string
}

// NewApplyProgressRenderer constructs a progress renderer for execute apply events.
func NewApplyProgressRenderer(options ApplyProgressRendererOptions, reference func(entity.Kind, string) string) *ApplyProgressRenderer {
	if reference == nil {
		panic("resource reference projector is required")
	}
	return &ApplyProgressRenderer{
		line:      newEphemeralLine(options.Output),
		finished:  make(map[int]struct{}),
		reference: reference,
	}
}

// Sink returns the execute event sink used by apply workflows.
func (renderer *ApplyProgressRenderer) Sink() executionevent.Sink {
	if renderer == nil {
		return nil
	}
	return renderer.renderEvent
}

// Close clears any active ephemeral progress line.
func (renderer *ApplyProgressRenderer) Close() {
	if renderer == nil {
		return
	}
	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	renderer.line.close()
}

func (renderer *ApplyProgressRenderer) renderEvent(event executionevent.Event) {
	if renderer == nil {
		return
	}

	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	if event.TotalActions == 0 {
		if event.RelationOrder == nil {
			return
		}
	}
	if event.RelationOrder != nil {
		renderer.renderRelationOrder(event)
		return
	}
	if event.Kind != executionevent.ActionStarted && event.Kind != executionevent.ActionDone && event.Kind != executionevent.ActionFailed {
		return
	}
	if event.Kind == executionevent.ActionDone && event.Action != nil {
		if _, exists := renderer.finished[event.Action.Index]; !exists {
			renderer.finished[event.Action.Index] = struct{}{}
			renderer.complete++
		}
	}
	label := ""
	if event.Action != nil {
		label = renderer.actionLabel(event.Action)
	}
	line := fmt.Sprintf("Applying %d/%d", renderer.complete, event.TotalActions)
	if label != "" {
		line += ": " + label
	}
	if event.Err != nil {
		line += ": failed"
	}
	renderer.line.write(line)
}

func (renderer *ApplyProgressRenderer) renderRelationOrder(event executionevent.Event) {
	if event.Kind != executionevent.RelationOrderStarted &&
		event.Kind != executionevent.RelationOrderDone &&
		event.Kind != executionevent.RelationOrderFailed {
		return
	}
	facts := event.RelationOrder
	line := fmt.Sprintf(
		"Applying extension order: %s %s %s",
		facts.Target,
		facts.Scope,
		facts.SequenceID,
	)
	switch event.Kind {
	case executionevent.RelationOrderDone:
		if facts.Changed {
			line += ": converged"
		} else {
			line += ": exact"
		}
	case executionevent.RelationOrderFailed:
		line += ": failed"
	}
	renderer.line.write(line)
}

func (renderer *ApplyProgressRenderer) actionLabel(action *executionevent.ActionFacts) string {
	parts := make([]string, 0, 2)
	if entityID, ok := topologyprojection.EntityID(action.Subject); ok {
		parts = append(parts, renderer.reference(entityID.Kind(), entityID.Name()))
	}
	if action.Destination.Validate() == nil {
		parts = append(parts, action.Destination.String())
	}
	return escapeText(strings.Join(parts, " -> "))
}
