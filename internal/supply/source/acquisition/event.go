package acquisition

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
)

// EventKind identifies one non-authoritative source operation progress phase.
type EventKind string

const (
	EventQueued    EventKind = "queued"
	EventStarted   EventKind = "started"
	EventCacheWait EventKind = "cache_wait"
	EventCacheHit  EventKind = "cache_hit"
	EventFetch     EventKind = "fetch"
	EventExport    EventKind = "export"
	EventDownload  EventKind = "download"
	EventHash      EventKind = "hash"
	EventPublished EventKind = "published"
	EventCompleted EventKind = "completed"
	EventFailed    EventKind = "failed"
)

// Event is one coherent, non-authoritative source operation observation.
type Event struct {
	kind        EventKind
	request     Request
	sourceID    artifact.SourceID
	resolvedRef artifact.ResolvedRef
	err         error
}

// NewEvent constructs a coherent source operation event.
func NewEvent(
	kind EventKind,
	request Request,
	sourceID artifact.SourceID,
	resolvedRef artifact.ResolvedRef,
	err error,
) (Event, error) {
	event := Event{kind: kind, request: request, sourceID: sourceID, resolvedRef: resolvedRef, err: err}
	if validateErr := event.Validate(); validateErr != nil {
		return Event{}, validateErr
	}
	return event, nil
}

// Validate rejects impossible event phase, error, and correlation states.
func (event Event) Validate() error {
	if err := event.kind.Validate(); err != nil {
		return err
	}
	if err := event.request.Validate(); err != nil {
		return err
	}
	if event.kind == EventFailed {
		if event.err == nil {
			return fmt.Errorf("failed source acquisition event requires an error")
		}
	} else if event.err != nil {
		return fmt.Errorf("source acquisition event %q must not carry an error", event.kind)
	}
	if event.kind == EventQueued {
		if event.sourceID != "" || event.resolvedRef != "" {
			return fmt.Errorf("queued source acquisition event must not carry resolved identity")
		}
		return nil
	}
	if event.sourceID == "" {
		if event.resolvedRef != "" {
			return fmt.Errorf("source acquisition event without source id must not carry a resolved ref")
		}
		if event.kind == EventFailed {
			return nil
		}
		return fmt.Errorf("source acquisition event %q requires source id", event.kind)
	}
	expectedSourceID, err := source.SourceIDFor(event.request.Source())
	if err != nil {
		return err
	}
	if event.sourceID != expectedSourceID {
		return fmt.Errorf("source acquisition event source id does not match request")
	}
	if err := validateEventResolvedRef(event.resolvedRef); err != nil {
		return err
	}
	if event.kind == EventCompleted || event.resolvedRef != "" {
		if err := source.ValidateResolutionCorrelation(event.request.Source(), event.sourceID, event.resolvedRef); err != nil {
			return err
		}
	}
	return nil
}

// Validate rejects unknown event phases.
func (kind EventKind) Validate() error {
	switch kind {
	case EventQueued,
		EventStarted,
		EventCacheWait,
		EventCacheHit,
		EventFetch,
		EventExport,
		EventDownload,
		EventHash,
		EventPublished,
		EventCompleted,
		EventFailed:
		return nil
	default:
		return fmt.Errorf("unknown source acquisition event kind %q", kind)
	}
}

// Kind returns the progress phase.
func (event Event) Kind() EventKind { return event.kind }

// Request returns the correlated operation request.
func (event Event) Request() Request { return event.request }

// SourceID returns the source identity known at this phase, when any.
func (event Event) SourceID() artifact.SourceID { return event.sourceID }

// ResolvedRef returns the immutable source revision known at this phase, when any.
func (event Event) ResolvedRef() artifact.ResolvedRef { return event.resolvedRef }

// Err returns the failure carried only by failed events.
func (event Event) Err() error { return event.err }

func validateEventResolvedRef(value artifact.ResolvedRef) error {
	text := string(value)
	if !utf8.ValidString(text) {
		return fmt.Errorf("source acquisition event resolved ref must be valid UTF-8")
	}
	if strings.TrimSpace(text) != text {
		return fmt.Errorf("source acquisition event resolved ref must be trimmed")
	}
	if strings.IndexFunc(text, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("source acquisition event resolved ref contains an unsafe control character")
	}
	return nil
}

// EventSink observes source events. Nil sinks are no-ops.
type EventSink func(Event)

// Emit emits event when sink is non-nil.
func (sink EventSink) Emit(event Event) {
	if sink != nil {
		sink(event)
	}
}

// OperationOptions carries optional per-operation event routing.
type OperationOptions struct {
	request           Request
	events            EventSink
	rootListingBudget *source.RootListingBudget
}

// WithRootListingBudget binds an operation-wide source-root enumeration budget.
func (options OperationOptions) WithRootListingBudget(
	budget *source.RootListingBudget,
) (OperationOptions, error) {
	if budget == nil {
		return OperationOptions{}, fmt.Errorf("source root listing budget is required")
	}
	options.rootListingBudget = budget
	return options, nil
}

// RootListingBudget returns the bound budget or a fresh default for a direct operation.
func (options OperationOptions) RootListingBudget() *source.RootListingBudget {
	if options.rootListingBudget != nil {
		return options.rootListingBudget
	}
	return source.NewRootListingBudget()
}

// NewOperationOptions constructs event routing for one validated request.
func NewOperationOptions(request Request, events EventSink) (OperationOptions, error) {
	if err := request.Validate(); err != nil {
		return OperationOptions{}, err
	}
	return OperationOptions{request: request, events: events}, nil
}

// Emit reports one best-effort progress event. Invalid progress facts are
// dropped because they cannot alter the authoritative operation result.
func (options OperationOptions) Emit(
	kind EventKind,
	sourceSpec source.Source,
	sourceID artifact.SourceID,
	resolvedRef artifact.ResolvedRef,
	err error,
) {
	if options.events == nil {
		return
	}
	if options.request.Source() != sourceSpec {
		return
	}
	event, eventErr := NewEvent(kind, options.request, sourceID, resolvedRef, err)
	if eventErr != nil {
		return
	}
	options.events.Emit(event)
}
