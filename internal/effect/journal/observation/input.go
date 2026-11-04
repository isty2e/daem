package observation

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output/ownership"
)

// CaptureInputSpec supplies canonical candidate facts, not a publication seal.
type CaptureInputSpec struct {
	Owner       stateauthority.Authority
	OperationID string
	Claims      *ownershipmutation.DocumentClaimTransition
	Provisional []journaldocument.ProvisionalAcquire
	Bindings    []ownership.DocumentBindingIntent
	GroupIndex  int
	Limits      configdocument.Limits
	Budget      *physicalwork.Budget
}

// CaptureInput freezes candidate ownership facts for one document group.
// Context validation is separate from admission by the containing record format.
type CaptureInput struct {
	owner       stateauthority.Authority
	operationID string
	claims      *ownershipmutation.DocumentClaimTransition
	provisional []journaldocument.ProvisionalAcquire
	bindings    []ownership.DocumentBindingIntent
	groupIndex  int
	limits      configdocument.Limits
	budget      *physicalwork.Budget
}

func NewCaptureInput(input CaptureInputSpec) CaptureInput {
	result := CaptureInput{owner: input.Owner, operationID: input.OperationID, provisional: slices.Clone(input.Provisional), bindings: slices.Clone(input.Bindings), groupIndex: input.GroupIndex, limits: input.Limits, budget: input.Budget}
	if input.Claims != nil {
		claims := *input.Claims
		result.claims = &claims
	}
	return result
}

func (input CaptureInput) Owner() stateauthority.Authority { return input.owner }
func (input CaptureInput) OperationID() string             { return input.operationID }
func (input CaptureInput) Claims() *ownershipmutation.DocumentClaimTransition {
	if input.claims == nil {
		return nil
	}
	claims := *input.claims
	return &claims
}

func (input CaptureInput) Provisional() []journaldocument.ProvisionalAcquire {
	return slices.Clone(input.provisional)
}

func (input CaptureInput) Bindings() []ownership.DocumentBindingIntent {
	return slices.Clone(input.bindings)
}
func (input CaptureInput) GroupIndex() int               { return input.groupIndex }
func (input CaptureInput) Limits() configdocument.Limits { return input.limits }
func (input CaptureInput) Budget() *physicalwork.Budget  { return input.budget }

func (input CaptureInput) Validate(ctx context.Context) error {
	if ctx == nil || input.budget == nil || input.groupIndex < 0 {
		return fmt.Errorf("native document capture requires context, budget and a nonnegative ordinal")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := input.owner.Validate(); err != nil {
		return err
	}
	if err := ownership.ValidateOperationID(input.operationID); err != nil {
		return err
	}
	return input.limits.Validate()
}
