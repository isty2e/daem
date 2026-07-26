package acquisition

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/supply/source"
)

// RequestID is an opaque caller-owned source operation correlation id.
type RequestID string

// Operation identifies the closed source acquisition operation axis.
type Operation string

const (
	OperationResolve  Operation = "resolve"
	OperationListRoot Operation = "list_root"
)

// Request is one validated source operation slot in a batch.
type Request struct {
	id        RequestID
	ordinal   int
	operation Operation
	source    source.Source
}

// NewRequest constructs one source acquisition request.
func NewRequest(id RequestID, ordinal int, operation Operation, sourceSpec source.Source) (Request, error) {
	request := Request{id: id, ordinal: ordinal, operation: operation, source: sourceSpec}
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}

// Validate rejects malformed source operation metadata or source locators.
func (request Request) Validate() error {
	value := string(request.id)
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("source acquisition request id is required and must be trimmed")
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("source acquisition request id must be valid UTF-8")
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("source acquisition request id contains an unsafe control character")
	}
	if request.ordinal < 0 {
		return fmt.Errorf("source acquisition request %q ordinal must be non-negative", request.id)
	}
	if err := request.operation.Validate(); err != nil {
		return fmt.Errorf("source acquisition request %q: %w", request.id, err)
	}
	if _, err := source.SourceIDFor(request.source); err != nil {
		return fmt.Errorf("source acquisition request %q source: %w", request.id, err)
	}
	return nil
}

// Validate rejects unknown source acquisition operations.
func (operation Operation) Validate() error {
	switch operation {
	case OperationResolve, OperationListRoot:
		return nil
	default:
		return fmt.Errorf("unknown source acquisition operation %q", operation)
	}
}

// ID returns the caller-owned correlation id.
func (request Request) ID() RequestID { return request.id }

// Ordinal returns the caller-owned stable result ordinal.
func (request Request) Ordinal() int { return request.ordinal }

// Operation returns the requested operation.
func (request Request) Operation() Operation { return request.operation }

// Source returns the canonical source locator.
func (request Request) Source() source.Source { return request.source }

// Equal reports whether two requests have identical canonical facts.
func (request Request) Equal(other Request) bool { return request == other }
