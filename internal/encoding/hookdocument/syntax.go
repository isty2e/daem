// Package hookdocument owns the bounded strict-JSON grammar shared by hook
// host-document import and managed projection.
package hookdocument

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

const (
	// MaximumBytes bounds one hook host document at every ingress.
	MaximumBytes int64 = 4 << 20
	// MaximumDepth bounds JSON nesting at every hook host-document ingress.
	MaximumDepth = 64
	// MaximumEventBytes bounds one event identity before materialization.
	MaximumEventBytes = 256
	// MaximumEvents bounds event cardinality in one host document.
	MaximumEvents = 256
	// MaximumGroups bounds aggregate group cardinality in one host document.
	MaximumGroups = 4096
	// MaximumHandlers bounds aggregate handler cardinality in one host document.
	MaximumHandlers = 4096
)

// ErrTooLarge classifies a hook host document beyond its byte budget.
var ErrTooLarge = errors.New("hook host document size limit exceeded")

// ErrStructuralBudgetExceeded classifies excessive Hook cardinality.
var ErrStructuralBudgetExceeded = errors.New("hook host document structural budget exceeded")

// Validate requires the byte grammar shared by hook import and managed hook
// projection.
func Validate(content []byte) error {
	if int64(len(content)) > MaximumBytes {
		return fmt.Errorf("%w: maximum=%d bytes", ErrTooLarge, MaximumBytes)
	}
	if err := validateDocumentStructure(content); err != nil {
		return err
	}
	return jsonstrict.Validate(content, "hook host document", MaximumDepth)
}

// ValidateProjection applies the same bounds to one canonical `hooks` object.
func ValidateProjection(content []byte) error {
	if int64(len(content)) > MaximumBytes {
		return fmt.Errorf("%w: maximum=%d bytes", ErrTooLarge, MaximumBytes)
	}
	if err := validateProjectionStructure(content); err != nil {
		return err
	}
	return jsonstrict.Validate(content, "Hook projection", MaximumDepth)
}

// ValidateEventBudget checks one encoded Hook event identity before a
// projection is materialized.
func ValidateEventBudget(event string) error {
	if len(event) <= MaximumEventBytes {
		return nil
	}
	return fmt.Errorf(
		"%w: event_bytes=%d maximum=%d",
		ErrStructuralBudgetExceeded,
		len(event),
		MaximumEventBytes,
	)
}

// ValidateCardinality checks the structural counts of one Hook projection
// before its arrays and maps are materialized.
func ValidateCardinality(events int, groups int, handlers int) error {
	if events < 0 || groups < 0 || handlers < 0 {
		return fmt.Errorf("hook projection cardinality must not be negative")
	}
	if events <= MaximumEvents && groups <= MaximumGroups && handlers <= MaximumHandlers {
		return nil
	}
	return fmt.Errorf(
		"%w: events=%d/%d groups=%d/%d handlers=%d/%d",
		ErrStructuralBudgetExceeded,
		events,
		MaximumEvents,
		groups,
		MaximumGroups,
		handlers,
		MaximumHandlers,
	)
}
