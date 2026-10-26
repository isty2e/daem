package nativefragment

import (
	"context"
	"fmt"

	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// HostConfigKeyContributions refines loaded source data, not source locators or
// host observations. Limits belong to the calling operation's admission policy.
func HostConfigKeyContributions(ctx context.Context, config desiredhostconfig.Config, content []byte, limits configdocument.Limits) ([]aggregate.SubjectContribution, error) {
	if ctx == nil {
		return nil, fmt.Errorf("configuration refinement context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Ownership() != desiredhostconfig.OwnershipKeys {
		return nil, fmt.Errorf("native key refinement requires key ownership")
	}
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{
		Target: config.Target(), Scope: config.Scope(), Path: config.Path(),
		Ownership: config.Ownership(), Syntax: config.Syntax(),
	})
	if err != nil {
		return nil, err
	}
	syntax, err := configdocument.ParseSyntax(string(placement.Grammar()))
	if err != nil {
		return nil, err
	}
	document, err := configdocument.Parse(ctx, content, syntax, limits)
	if err != nil {
		return nil, err
	}
	leaves, err := document.Leaves()
	if err != nil {
		return nil, err
	}

	contributions := make([]aggregate.SubjectContribution, 0, len(leaves))
	for _, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		footprint, err := output.NewKeyFootprint(leaf.Path)
		if err != nil {
			return nil, err
		}
		contract, err := placement.ProjectionContract(footprint)
		if err != nil {
			return nil, err
		}
		subject, err := topologyprojection.HostConfigKeySubject(config.ID(), config.Target(), config.Scope(), leaf.Path)
		if err != nil {
			return nil, err
		}
		contribution, err := aggregate.NewManagedContribution(contract, leaf.Value.Canonical())
		if err != nil {
			return nil, err
		}
		item, err := aggregate.NewSubjectContribution(subject, contribution)
		if err != nil {
			return nil, err
		}
		contributions = append(contributions, item)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return contributions, nil
}
