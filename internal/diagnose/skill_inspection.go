package diagnose

import (
	"context"
	"errors"
	"fmt"

	skillresource "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/findings"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/backend/localfs"
)

const skillGroupExpansionCheckName = "skill_group_expansion"

// InspectManifestSkills constructs one bounded skill view shared by every
// skill-related doctor check in the current diagnostic phase.
func InspectManifestSkills(
	ctx context.Context,
	paths daempaths.Paths,
	directSkills []skillresource.Skill,
	sets []skillresource.SkillSet,
) ([]skillresource.Skill, []findings.Check) {
	resolver, err := localfs.NewResolver(paths.ManifestRoot)
	if err != nil {
		return append([]skillresource.Skill(nil), directSkills...), []findings.Check{
			skillGroupInspectionErrorCheck(err),
		}
	}

	return inspectManifestSkills(
		ctx,
		resolver,
		directSkills,
		sets,
		source.NewRootListingBudget(),
		skillresource.NewExpansionBudget(),
	)
}

type skillRootListingObservation struct {
	listing source.RootListing
	err     error
}

func inspectManifestSkills(
	ctx context.Context,
	lister acquisition.RootLister,
	directSkills []skillresource.Skill,
	sets []skillresource.SkillSet,
	listingBudget *source.RootListingBudget,
	expansionBudget *skillresource.ExpansionBudget,
) ([]skillresource.Skill, []findings.Check) {
	direct := append([]skillresource.Skill(nil), directSkills...)
	if len(sets) == 0 {
		return direct, nil
	}
	if ctx == nil {
		return direct, []findings.Check{skillGroupInspectionErrorCheck(
			fmt.Errorf("skill-group diagnostic context is required"),
		)}
	}
	if err := ctx.Err(); err != nil {
		return direct, nil
	}
	if err := expansionBudget.CheckGroupCount(len(sets)); err != nil {
		return direct, []findings.Check{skillGroupBudgetCheck(err)}
	}

	localSourceIDs := make(map[artifact.SourceID]struct{})
	for _, set := range sets {
		if _, ok := set.Source().Local(); !ok {
			continue
		}
		sourceID, err := source.SourceIDFor(set.Source())
		if err != nil {
			return direct, []findings.Check{skillGroupInspectionErrorCheck(err)}
		}
		localSourceIDs[sourceID] = struct{}{}
	}
	if len(localSourceIDs) == 0 {
		return direct, nil
	}
	if err := listingBudget.CheckRootCount(len(localSourceIDs)); err != nil {
		return direct, []findings.Check{skillGroupBudgetCheck(err)}
	}
	if lister == nil {
		return direct, []findings.Check{skillGroupInspectionErrorCheck(
			fmt.Errorf("local skill-group root lister is required"),
		)}
	}

	operationOptions, err := (acquisition.OperationOptions{}).WithRootListingBudget(listingBudget)
	if err != nil {
		return direct, []findings.Check{skillGroupInspectionErrorCheck(err)}
	}
	observations := make(map[artifact.SourceID]skillRootListingObservation, len(localSourceIDs))
	expanded := append([]skillresource.Skill(nil), direct...)
	for _, set := range sets {
		if err := ctx.Err(); err != nil {
			return direct, nil
		}
		if _, ok := set.Source().Local(); !ok {
			continue
		}

		sourceID, err := source.SourceIDFor(set.Source())
		if err != nil {
			return direct, []findings.Check{skillGroupInspectionErrorCheck(err)}
		}
		observation, observed := observations[sourceID]
		if !observed {
			observation.listing, observation.err = lister.ListSourceRoot(ctx, set.Source(), operationOptions)
			observations[sourceID] = observation
		}
		if observation.err != nil {
			if ctx.Err() != nil {
				return direct, nil
			}
			if isSkillGroupBudgetError(observation.err) {
				return direct, []findings.Check{skillGroupBudgetCheck(observation.err)}
			}
			continue
		}

		groupSkills, err := set.ExpandWithBudget(ctx, observation.listing, expansionBudget)
		if err != nil {
			if ctx.Err() != nil {
				return direct, nil
			}
			if isSkillGroupBudgetError(err) {
				return direct, []findings.Check{skillGroupBudgetCheck(err)}
			}
			continue
		}
		expanded = append(expanded, groupSkills...)
	}

	return expanded, nil
}

func isSkillGroupBudgetError(err error) bool {
	return errors.Is(err, source.ErrRootListingLimitExceeded) ||
		errors.Is(err, skillresource.ErrExpansionLimitExceeded)
}

func skillGroupBudgetCheck(err error) findings.Check {
	check := skillGroupInspectionErrorCheck(err)
	check.NextStep = "reduce skill-group declarations, source-root breadth, or selector breadth, then rerun daem doctor"
	return check
}

func skillGroupInspectionErrorCheck(err error) findings.Check {
	return errorCheck(
		skillGroupExpansionCheckName,
		fmt.Sprintf("inspect selector-backed skill groups: %v", err),
	)
}
