package progress

import (
	"fmt"
	"io"
	"sync"

	"github.com/isty2e/daem/internal/target"
	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

// ImportProgressRenderer renders provisional import progress as one ephemeral line.
type ImportProgressRenderer struct {
	mu   sync.Mutex
	line ephemeralLine
}

// NewImportProgressRenderer constructs a concurrency-safe import progress renderer.
func NewImportProgressRenderer(output io.Writer) *ImportProgressRenderer {
	return &ImportProgressRenderer{line: newEphemeralLine(output)}
}

// Sink returns the import progress sink used by adopt workflows.
func (renderer *ImportProgressRenderer) Sink() workflowadopt.ProgressEventSink {
	if renderer == nil {
		return nil
	}
	return renderer.renderEvent
}

// Close clears any active ephemeral progress line.
func (renderer *ImportProgressRenderer) Close() {
	if renderer == nil {
		return
	}
	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	renderer.line.close()
}

func (renderer *ImportProgressRenderer) renderEvent(event workflowadopt.ProgressEvent) {
	if renderer == nil {
		return
	}

	line := importProgressLine(event)
	if line == "" {
		return
	}
	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	renderer.line.write(line)
}

func importProgressLine(event workflowadopt.ProgressEvent) string {
	switch event.Kind {
	case workflowadopt.ProgressEventPhaseStarted:
		switch event.Phase {
		case workflowadopt.ProgressPhaseDiscovery:
			return "Discovering import candidates"
		case workflowadopt.ProgressPhaseRevalidation:
			return "Revalidating import sources"
		case workflowadopt.ProgressPhasePublication:
			return "Publishing import changes"
		}
	case workflowadopt.ProgressEventTargetScopeStarted,
		workflowadopt.ProgressEventTargetScopeCompleted:
		verb := ""
		switch event.Phase {
		case workflowadopt.ProgressPhaseDiscovery:
			verb = "Discovering"
		case workflowadopt.ProgressPhaseRevalidation:
			verb = "Revalidating"
		default:
			return ""
		}
		if _, err := target.ParseTarget(string(event.Target)); err != nil {
			return ""
		}
		if _, err := target.ParseScope(string(event.Scope)); err != nil {
			return ""
		}
		if event.Completed < 0 || event.Total <= 0 || event.Completed > event.Total {
			return ""
		}
		return fmt.Sprintf(
			"%s import candidates %d/%d: %s",
			verb,
			event.Completed,
			event.Total,
			escapeText(string(event.Target)+"/"+string(event.Scope)),
		)
	}
	return ""
}
