package nativecodec

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type contributionValidator struct {
	ctx        context.Context
	contractID aggregate.CodecContractID
	limits     configdocument.Limits
}

// NewContributionValidator binds value-only admission to a live operation and
// value budget. It neither selects a document budget nor enables host effects.
func NewContributionValidator(ctx context.Context, contractID aggregate.CodecContractID, limits configdocument.Limits) (aggregate.ContributionValidator, error) {
	if ctx == nil {
		return nil, fmt.Errorf("native contribution context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if _, err := nativeSyntax(contractID); err != nil {
		return nil, err
	}
	return contributionValidator{ctx: ctx, contractID: contractID, limits: limits}, nil
}

func (validator contributionValidator) ContractID() aggregate.CodecContractID {
	return validator.contractID
}

func (validator contributionValidator) ValidateContributions(set aggregate.ContributionSet) error {
	return ValidateContributions(validator.ctx, validator.contractID, set, validator.limits)
}
