package skill

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/supply/source"
)

// Expand constructs deterministic canonical Skills from one matching supplied listing.
func (set SkillSet) Expand(ctx context.Context, listing source.RootListing) ([]Skill, error) {
	return set.ExpandWithBudget(ctx, listing, NewExpansionBudget())
}

// ExpandWithBudget constructs deterministic canonical Skills while charging
// one caller-owned lock-build expansion budget.
func (set SkillSet) ExpandWithBudget(
	ctx context.Context,
	listing source.RootListing,
	budget *ExpansionBudget,
) ([]Skill, error) {
	if ctx == nil {
		return nil, fmt.Errorf("skill set expansion context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := set.Validate(); err != nil {
		return nil, err
	}
	if err := listing.ValidateFor(set.source); err != nil {
		return nil, fmt.Errorf("skill set source listing: %w", err)
	}
	if !listing.IsDirectory() {
		return nil, fmt.Errorf("skill set source must resolve to a directory")
	}

	selectedNames, err := selectNames(ctx, listing.ChildNames(), set.include, set.exclude, budget)
	if err != nil {
		return nil, err
	}

	skills := make([]Skill, 0, len(selectedNames))
	for _, name := range selectedNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		childSource, err := set.source.ResolvedChild(name, listing.ResolvedRef())
		if err != nil {
			return nil, err
		}
		child, err := set.child(name, childSource)
		if err != nil {
			return nil, err
		}
		skills = append(skills, child)
	}
	return skills, nil
}
