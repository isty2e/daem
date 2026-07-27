package diagnose

import (
	"context"

	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/findings"
	daempaths "github.com/isty2e/daem/internal/paths"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/backend/localfs"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

const (
	skillDiagnosticRepairable = "skill.compat.repairable"
	skillDiagnosticManual     = "skill.compat.manual"
)

// SkillSourceEpoch exposes raw local skill resolutions from one exact
// selection epoch. Implementations must reject source or selection drift.
type SkillSourceEpoch interface {
	SkillResolution(skill.Skill, targetselection.Selection) (acquisition.Resolution, bool)
}

func SkillRepairDiagnostics(ctx context.Context, paths daempaths.Paths, skills []skill.Skill, selection targetselection.Selection) []findings.Diagnostic {
	return SkillRepairDiagnosticsFromSourceEpoch(ctx, paths, skills, selection, nil)
}

// SkillRepairDiagnosticsFromSourceEpoch reuses matching raw source facts and
// falls back to a fresh local resolution when the epoch has no exact match.
func SkillRepairDiagnosticsFromSourceEpoch(
	ctx context.Context,
	paths daempaths.Paths,
	skills []skill.Skill,
	selection targetselection.Selection,
	epoch SkillSourceEpoch,
) []findings.Diagnostic {
	resolver, err := localfs.NewResolver(paths.ManifestRoot)
	if err != nil {
		return nil
	}
	diagnostics := make([]findings.Diagnostic, 0)
	for _, skillResource := range skills {
		targets := SelectedSkillTargets(skillResource, selection)
		if len(targets) == 0 {
			continue
		}
		resolution, local, err := resolveLocalSkill(
			ctx,
			resolver,
			skillResource,
			selection,
			epoch,
		)
		if !local || err != nil {
			continue
		}
		for _, target := range targets {
			classification, err := skillrepair.Classify(
				ctx,
				resolution.Identity(),
				resolution.View(),
				skillResource.InstallName(),
				[]targetpkg.Target{target},
			)
			if err != nil {
				continue
			}
			diagnostic, ok := SkillRepairDiagnosticForClassification(skillResource, target, classification)
			if ok {
				diagnostics = append(diagnostics, diagnostic)
			}
		}
	}
	return diagnostics
}

func resolveLocalSkill(
	ctx context.Context,
	resolver localfs.Resolver,
	resource skill.Skill,
	selection targetselection.Selection,
	epoch SkillSourceEpoch,
) (acquisition.Resolution, bool, error) {
	sourceSpec := resource.Source()
	if _, ok := sourceSpec.Local(); !ok {
		return acquisition.Resolution{}, false, nil
	}
	if epoch != nil {
		if resolution, ok := epoch.SkillResolution(resource, selection); ok {
			return resolution, true, nil
		}
	}
	resolution, err := resolver.Resolve(ctx, sourceSpec, acquisition.OperationOptions{})
	if err != nil {
		return acquisition.Resolution{}, true, err
	}
	return resolution, true, nil
}

func SkillRepairDiagnosticForClassification(
	skill skill.Skill,
	target targetpkg.Target,
	classification skillrepair.Classification,
) (findings.Diagnostic, bool) {
	return SkillRepairDiagnosticForClassificationWithOptions(skill, target, classification, true)
}

func SkillRepairDiagnosticForClassificationWithOptions(
	skill skill.Skill,
	target targetpkg.Target,
	classification skillrepair.Classification,
	suppressDeclaredMechanical bool,
) (findings.Diagnostic, bool) {
	switch classification.Repairability() {
	case skillrepair.RepairabilityMechanical:
		if suppressDeclaredMechanical && skill.CompatRepair() {
			return findings.Diagnostic{}, false
		}
		severity := findings.SeverityError
		nextStep := "set compat_repair = true on the manifest resource and run daem lock"
		if skill.CompatRepair() {
			severity = findings.SeverityWarn
			nextStep = "run daem lock to record the repair recipe"
		}
		return findings.Diagnostic{
			Severity:      severity,
			Code:          skillDiagnosticRepairable,
			EntityID:      skill.ID(),
			Target:        target,
			Scope:         skill.Scope(),
			Detail:        "skill compatibility failure is mechanically repairable by daem",
			Repairability: string(skillrepair.RepairabilityMechanical),
			RepairActions: classification.Actions(),
			NextStep:      nextStep,
		}, true
	case skillrepair.RepairabilityManual:
		reasons := classification.ManualReasons()
		if len(reasons) == 0 {
			reasons = []string{"manual skill source edit is required"}
		}
		return findings.Diagnostic{
			Severity:      findings.SeverityError,
			Code:          skillDiagnosticManual,
			EntityID:      skill.ID(),
			Target:        target,
			Scope:         skill.Scope(),
			Detail:        "skill compatibility failure requires manual source edits",
			Repairability: string(skillrepair.RepairabilityManual),
			RepairActions: classification.Actions(),
			ManualReasons: reasons,
			NextStep:      "edit the skill source and rerun daem lock",
		}, true
	default:
		return findings.Diagnostic{}, false
	}
}
