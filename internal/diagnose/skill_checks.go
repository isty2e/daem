package diagnose

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	skillresource "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/findings"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/backend/localfs"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// ManifestSkillChecks reports compatibility checks for direct and selector-expanded manifest skills.
func ManifestSkillChecks(
	ctx context.Context,
	paths daempaths.Paths,
	skills []skillresource.Skill,
	sets []skillresource.SkillSet,
	selection targetselection.Selection,
) []findings.Check {
	resolver, err := localfs.NewResolver(paths.ManifestRoot)
	if err != nil {
		return nil
	}
	checks := make([]findings.Check, 0)
	for _, skillResource := range manifestSkillsForChecks(ctx, resolver, skills, sets) {
		checks = append(checks, skillChecks(ctx, resolver, skillResource, selection)...)
	}

	return checks
}

func manifestSkillsForChecks(
	ctx context.Context,
	resolver localfs.Resolver,
	skills []skillresource.Skill,
	sets []skillresource.SkillSet,
) []skillresource.Skill {
	result := append([]skillresource.Skill(nil), skills...)
	for _, set := range sets {
		if ctx.Err() != nil {
			return result
		}
		if _, ok := set.Source().Local(); !ok {
			continue
		}
		listing, err := resolver.ListSourceRoot(ctx, set.Source(), acquisition.OperationOptions{})
		if err != nil {
			continue
		}
		groupSkills, err := set.Expand(listing)
		if err != nil {
			continue
		}
		result = append(result, groupSkills...)
	}
	return result
}

func skillChecks(
	ctx context.Context,
	resolver localfs.Resolver,
	skill skillresource.Skill,
	selection targetselection.Selection,
) []findings.Check {
	targets := SelectedSkillTargets(skill, selection)
	if len(targets) == 0 {
		return nil
	}

	localSource, ok := skill.Source().Local()
	if !ok {
		return nil
	}

	sourcePath := localSource.Path()
	resolution, err := resolver.Resolve(ctx, skill.Source(), acquisition.OperationOptions{})
	if err != nil {
		if localfs.IsSourceUnavailable(err) {
			return skillSourceUnavailableChecks(targets, skill, sourcePath)
		}

		return skillSourceErrorChecks(targets, skill, fmt.Sprintf("resolve %s: %v", sourcePath, err))
	}
	identity := resolution.Identity()
	view := resolution.View()

	if err := skillcompat.ValidateSkillArtifact(ctx, view, identity.SourceID()); err != nil {
		return skillRepairabilityChecks(ctx, targets, skill, identity, view, err.Error())
	}

	checks := make([]findings.Check, 0, len(targets))
	for _, target := range targets {
		checks = append(checks, skillTargetChecks(ctx, identity, view, skill, target)...)
	}

	return checks
}

func SelectedSkillTargets(skill skillresource.Skill, selection targetselection.Selection) []targetpkg.Target {
	skillTargets := skill.Targets()
	targets := make([]targetpkg.Target, 0, len(skillTargets))
	for _, target := range skillTargets {
		if !selection.Includes(target) {
			continue
		}
		if !profile.Profile(target).Supports(entity.KindSkill) {
			continue
		}
		targets = append(targets, target)
	}

	return targets
}

func skillSourceUnavailableChecks(targets []targetpkg.Target, skill skillresource.Skill, sourcePath string) []findings.Check {
	checks := make([]findings.Check, 0, len(targets))
	for _, target := range targets {
		checks = append(checks, warnCheck(
			skillCompatibilityCheckName(target, skill),
			fmt.Sprintf("local skill source %s is missing; lock will validate resolved content before apply", sourcePath),
		))
	}

	return checks
}

func skillSourceErrorChecks(targets []targetpkg.Target, skill skillresource.Skill, detail string) []findings.Check {
	checks := make([]findings.Check, 0, len(targets))
	for _, target := range targets {
		checks = append(checks, errorCheck(skillCompatibilityCheckName(target, skill), detail))
	}

	return checks
}

func skillTargetChecks(
	ctx context.Context,
	identity artifact.ExactIdentity,
	view access.View,
	skill skillresource.Skill,
	target targetpkg.Target,
) []findings.Check {
	if !profile.Profile(target).Supports(entity.KindSkill) {
		return nil
	}

	diagnostics := skillcompat.Diagnostics(ctx, view, identity.SourceID(), skill.InstallName(), target)
	if len(diagnostics) == 0 {
		return []findings.Check{
			okCheck(
				skillCompatibilityCheckName(target, skill),
				"local skill source is compatible with selected target policy",
			),
		}
	}

	checks := make([]findings.Check, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Blocking() {
			checks = append(checks, skillRepairabilityChecks(ctx, []targetpkg.Target{target}, skill, identity, view, diagnostic.Message)...)
			continue
		}

		checks = append(checks, warnCheck(skillCompatibilityCheckName(target, skill), diagnostic.Message))
	}

	return checks
}

func skillRepairabilityChecks(
	ctx context.Context,
	targets []targetpkg.Target,
	skill skillresource.Skill,
	identity artifact.ExactIdentity,
	view access.View,
	detail string,
) []findings.Check {
	checks := make([]findings.Check, 0, len(targets))
	for _, target := range targets {
		classification, err := skillrepair.Classify(ctx, identity, view, skill.InstallName(), []targetpkg.Target{target})
		if err != nil {
			checks = append(checks, errorCheck(skillCompatibilityCheckName(target, skill), detail))
			continue
		}
		diagnostic, ok := SkillRepairDiagnosticForClassificationWithOptions(skill, target, classification, false)
		if !ok {
			checks = append(checks, errorCheck(skillCompatibilityCheckName(target, skill), detail))
			continue
		}
		severity := findings.SeverityError
		if diagnostic.Severity == findings.SeverityWarn {
			severity = findings.SeverityWarn
		}
		checks = append(checks, findings.Check{
			Severity:      severity,
			Name:          skillCompatibilityCheckName(target, skill),
			Detail:        detail + "; " + diagnostic.Detail,
			Repairability: diagnostic.Repairability,
			RepairActions: append([]string(nil), diagnostic.RepairActions...),
			ManualReasons: append([]string(nil), diagnostic.ManualReasons...),
			NextStep:      diagnostic.NextStep,
		})
	}
	return checks
}

func skillCompatibilityCheckName(target targetpkg.Target, skill skillresource.Skill) string {
	return fmt.Sprintf("target=%s skill=%s compatibility", target, skill.ID().Name())
}
