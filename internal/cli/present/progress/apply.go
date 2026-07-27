package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/isty2e/daem/internal/effect/execute"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// ApplyProgressRendererOptions configures apply human progress rendering.
type ApplyProgressRendererOptions struct {
	Output io.Writer
}

// ApplyProgressRenderer renders execute apply progress facts as line-oriented human text.
type ApplyProgressRenderer struct {
	mu       sync.Mutex
	line     ephemeralLine
	complete int
	finished map[int]struct{}
}

// NewApplyProgressRenderer constructs a progress renderer for execute apply events.
func NewApplyProgressRenderer(options ApplyProgressRendererOptions) *ApplyProgressRenderer {
	return &ApplyProgressRenderer{
		line:     newEphemeralLine(options.Output),
		finished: make(map[int]struct{}),
	}
}

// Sink returns the execute event sink used by apply workflows.
func (renderer *ApplyProgressRenderer) Sink() execute.EventSink {
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

func (renderer *ApplyProgressRenderer) renderEvent(event execute.Event) {
	if renderer == nil {
		return
	}

	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	if event.TotalActions == 0 {
		return
	}
	if event.Kind != execute.EventActionStarted && event.Kind != execute.EventActionDone && event.Kind != execute.EventActionFailed {
		return
	}
	if event.Kind == execute.EventActionDone && event.Action != nil {
		if _, exists := renderer.finished[event.Action.Index]; !exists {
			renderer.finished[event.Action.Index] = struct{}{}
			renderer.complete++
		}
	}
	label := ""
	if event.Action != nil {
		label = progressActionLabel(event.Action)
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

func progressActionLabel(action *execute.ActionEventFacts) string {
	parts := make([]string, 0, 2)
	if entityID, ok := topologyprojection.EntityID(action.Subject); ok {
		parts = append(parts, string(entityID.Kind())+"/"+entityID.Name())
	}
	if action.Destination.Validate() == nil {
		parts = append(parts, action.Destination.String())
	}
	return escapeText(strings.Join(parts, " -> "))
}
