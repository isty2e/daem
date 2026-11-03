package nativecodec

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// ValidateContributions admits native desired values without a host document or
// document capability. Value decoding uses the supplied live context and limits.
func ValidateContributions(ctx context.Context, contractID aggregate.CodecContractID, set aggregate.ContributionSet, limits configdocument.Limits) error {
	if ctx == nil {
		return fmt.Errorf("native contribution context is required")
	}
	if err := ctx.Err(); err != nil {
		return nativeCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, set.Address().KeyPath())
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	syntax, err := nativeSyntax(contractID)
	if err != nil {
		return err
	}
	_, failure := contributionValue(ctx, contractID, syntax, limits, set)
	if failure != nil {
		return failure
	}
	return nil
}

func nativeSyntax(contractID aggregate.CodecContractID) (configdocument.Syntax, error) {
	name, ok := aggregate.HostConfigSyntaxForCodec(contractID)
	if !ok {
		return 0, fmt.Errorf("native codec contract is unsupported")
	}
	return configdocument.ParseSyntax(name)
}

func validNativeContract(contractID aggregate.CodecContractID, contract aggregate.ProjectionContract) bool {
	if contract.Validate() != nil || contract.CodecContractID() != contractID {
		return false
	}
	address := contract.Address()
	document := address.Document()
	expected, err := aggregate.NewHostConfigProjectionContract(document.Target(), document.Scope(), document.AggregateRoot(), address.Footprint(), contractID)
	return err == nil && expected.Equal(contract)
}

func contributionValue(ctx context.Context, contractID aggregate.CodecContractID, syntax configdocument.Syntax, limits configdocument.Limits, set aggregate.ContributionSet) (configdocument.Value, *aggregate.CodecFailure) {
	key := set.Address().KeyPath()
	if err := ctx.Err(); err != nil {
		return configdocument.Value{}, nativeCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, key)
	}
	canonical, err := aggregate.NewContributionSet(set.Contributions())
	if err != nil || !validNativeContract(contractID, canonical.Contract()) {
		return configdocument.Value{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, key)
	}
	items := canonical.Contributions()
	if len(items) != 1 || aggregate.ValidateSubjectContract(items[0].SubjectID(), canonical.Contract()) != nil {
		return configdocument.Value{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, key)
	}
	value, err := configdocument.DecodeValue(ctx, syntax, items[0].Contribution().CanonicalContribution(), limits)
	if err != nil {
		return configdocument.Value{}, nativeCodecFailure(err, aggregate.CodecFailureCanonicalInvalid, key)
	}
	if !value.IsAtomic() {
		return configdocument.Value{}, nativeFailure(aggregate.CodecFailureCanonicalInvalid, key)
	}
	return value, nil
}

func nativeCodecFailure(err error, fallback aggregate.CodecFailureReason, key output.KeyPath) *aggregate.CodecFailure {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		fallback = aggregate.CodecFailureOperationCanceled
	case errors.Is(err, configdocument.ErrLimitExceeded):
		fallback = aggregate.CodecFailureResourceLimitExceeded
	}
	return nativeFailure(fallback, key)
}

func nativeFailure(reason aggregate.CodecFailureReason, key output.KeyPath) *aggregate.CodecFailure {
	failure, _ := aggregate.NewCodecFailure(reason, key)
	return failure
}
