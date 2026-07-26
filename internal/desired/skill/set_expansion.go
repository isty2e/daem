package skill

import (
	"fmt"

	"github.com/isty2e/daem/internal/supply/source"
)

// Expand constructs deterministic canonical Skills from one matching supplied listing.
func (set SkillSet) Expand(listing source.RootListing) ([]Skill, error) {
	if err := set.Validate(); err != nil {
		return nil, err
	}
	if err := listing.ValidateFor(set.source); err != nil {
		return nil, fmt.Errorf("skill set source listing: %w", err)
	}
	if !listing.IsDirectory() {
		return nil, fmt.Errorf("skill set source must resolve to a directory")
	}

	selectedNames, err := selectNames(listing.ChildNames(), set.include, set.exclude)
	if err != nil {
		return nil, err
	}

	skills := make([]Skill, 0, len(selectedNames))
	for _, name := range selectedNames {
		childSource, err := set.source.ResolvedChild(name, listing.ResolvedRef())
		if err != nil {
			return nil, err
		}
		child, err := set.Child(name, childSource)
		if err != nil {
			return nil, err
		}
		skills = append(skills, child)
	}
	return skills, nil
}
