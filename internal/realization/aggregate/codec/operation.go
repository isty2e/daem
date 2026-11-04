package aggregatecodec

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
)

// ForOperation binds requested native codecs to this operation; fixed codecs
// retain their own limits. It does not widen the default product catalog.
func ForOperation(ctx context.Context, required []aggregate.CodecContractID, nativeDocumentLimits, nativeValueLimits configdocument.Limits) (aggregate.CodecCatalog, error) {
	codecs, err := collectRequired(ctx, required, func(id aggregate.CodecContractID) (aggregate.Codec, error) {
		if _, native := aggregate.HostConfigSyntaxForCodec(id); native {
			return nativecodec.New(ctx, id, nativeDocumentLimits, nativeValueLimits)
		}
		codec, admitted := catalog.Lookup(id)
		if !admitted {
			return nil, fmt.Errorf("aggregate codec %q is not admitted", id)
		}
		return codec, nil
	})
	if err != nil {
		return aggregate.CodecCatalog{}, err
	}
	return aggregate.NewCodecCatalog(codecs)
}

// ForContributions admits selected values without document or effect capabilities.
func ForContributions(ctx context.Context, required []aggregate.CodecContractID, nativeValueLimits configdocument.Limits) (aggregate.ContributionCatalog, error) {
	validators, err := collectRequired(ctx, required, func(id aggregate.CodecContractID) (aggregate.ContributionValidator, error) {
		if _, native := aggregate.HostConfigSyntaxForCodec(id); native {
			return nativecodec.NewContributionValidator(ctx, id, nativeValueLimits)
		}
		validator, admitted := catalog.Contributions().Lookup(id)
		if !admitted {
			return nil, fmt.Errorf("aggregate codec %q is not admitted", id)
		}
		return validator, nil
	})
	if err != nil {
		return aggregate.ContributionCatalog{}, err
	}
	return aggregate.NewContributionCatalog(validators)
}

// ContractIDs returns all supported contracts, including context-bound native codecs.
func ContractIDs() []aggregate.CodecContractID {
	var required []aggregate.CodecContractID
	for _, placement := range aggregate.ImplementedHookPlacements() {
		required = append(required, placement.CodecContractID())
	}
	for _, placement := range aggregate.ImplementedMCPContracts() {
		required = append(required, placement.CodecContractID())
	}
	for _, syntax := range []string{"json", "jsonc", "json-line-comments", "toml"} {
		id, _ := aggregate.HostConfigCodecForSyntax(syntax)
		required = append(required, id)
	}
	return required
}

func collectRequired[Port aggregate.ContributionValidator](ctx context.Context, required []aggregate.CodecContractID, resolve func(aggregate.CodecContractID) (Port, error)) ([]Port, error) {
	if ctx == nil {
		return nil, fmt.Errorf("aggregate catalog context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(required) == 0 {
		return nil, fmt.Errorf("aggregate catalog requires at least one contract")
	}

	seen := make(map[aggregate.CodecContractID]struct{})
	var ports []Port
	for _, id := range required {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		port, err := resolve(id)
		if err != nil {
			return nil, err
		}
		seen[id] = struct{}{}
		ports = append(ports, port)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ports, nil
}
