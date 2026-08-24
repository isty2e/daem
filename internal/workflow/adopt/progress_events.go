package adopt

import "github.com/isty2e/daem/internal/target"

// ProgressPhase identifies one provisional import workflow phase.
type ProgressPhase string

const (
	ProgressPhaseDiscovery    ProgressPhase = "discovery"
	ProgressPhaseRevalidation ProgressPhase = "revalidation"
	ProgressPhasePublication  ProgressPhase = "publication"
)

// ProgressEventKind identifies one import progress transition.
type ProgressEventKind string

const (
	ProgressEventPhaseStarted         ProgressEventKind = "phase_started"
	ProgressEventTargetScopeStarted   ProgressEventKind = "target_scope_started"
	ProgressEventTargetScopeCompleted ProgressEventKind = "target_scope_completed"
	ProgressEventPhaseCompleted       ProgressEventKind = "phase_completed"
)

// ProgressEvent carries bounded provisional import facts for command subscribers.
type ProgressEvent struct {
	Kind      ProgressEventKind
	Phase     ProgressPhase
	Target    target.Target
	Scope     target.Scope
	Completed int
	Total     int
}

// ProgressEventSink observes import progress events. Nil sinks are no-ops.
type ProgressEventSink func(ProgressEvent)

func (sink ProgressEventSink) emit(event ProgressEvent) {
	if sink != nil {
		sink(event)
	}
}

func importProgressTotal(requestTargets []target.Target, requestScopes []target.Scope) int {
	return len(requestTargets) * len(requestScopes)
}
