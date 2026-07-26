package acquisition

import (
	"fmt"

	"github.com/isty2e/daem/internal/supply/source"
)

type resultKind uint8

const (
	resultKindInvalid resultKind = iota
	resultKindResolution
	resultKindListing
	resultKindFailure
)

// Result is one closed source acquisition batch outcome.
type Result struct {
	kind       resultKind
	request    Request
	resolution Resolution
	listing    source.RootListing
	err        error
}

// NewResolutionResult constructs a successful resolve outcome.
func NewResolutionResult(request Request, resolution Resolution) (Result, error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	if request.Operation() != OperationResolve {
		return Result{}, fmt.Errorf("source acquisition resolution result requires resolve request")
	}
	if err := resolution.Validate(request.Source()); err != nil {
		return Result{}, err
	}
	return Result{kind: resultKindResolution, request: request, resolution: resolution}, nil
}

// NewListingResult constructs a successful root-listing outcome.
func NewListingResult(request Request, listing source.RootListing) (Result, error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	if request.Operation() != OperationListRoot {
		return Result{}, fmt.Errorf("source acquisition listing result requires list-root request")
	}
	if err := listing.ValidateFor(request.Source()); err != nil {
		return Result{}, err
	}
	return Result{kind: resultKindListing, request: request, listing: listing}, nil
}

// NewFailureResult constructs a failed operation outcome.
func NewFailureResult(request Request, err error) (Result, error) {
	if err == nil {
		return Result{}, fmt.Errorf("source acquisition failure result requires an error")
	}
	if validateErr := request.Validate(); validateErr != nil {
		return Result{}, validateErr
	}
	return Result{kind: resultKindFailure, request: request, err: err}, nil
}

// Request returns the exact request correlated with this result.
func (result Result) Request() Request { return result.request }

// Resolution returns the resolve outcome when present.
func (result Result) Resolution() (Resolution, bool) {
	return result.resolution, result.kind == resultKindResolution
}

// Listing returns the list-root outcome when present.
func (result Result) Listing() (source.RootListing, bool) {
	return result.listing, result.kind == resultKindListing
}

// Err returns the operation error when this is a failure outcome.
func (result Result) Err() error {
	if result.kind != resultKindFailure {
		return nil
	}
	return result.err
}

// Validate rejects zero, mismatched, or multi-state outcomes.
func (result Result) Validate() error {
	switch result.kind {
	case resultKindResolution:
		_, err := NewResolutionResult(result.request, result.resolution)
		return err
	case resultKindListing:
		_, err := NewListingResult(result.request, result.listing)
		return err
	case resultKindFailure:
		_, err := NewFailureResult(result.request, result.err)
		return err
	default:
		return fmt.Errorf("source acquisition result kind is invalid")
	}
}
