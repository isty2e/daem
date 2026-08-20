package diagnoseworkflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/diagnose"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/findings"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/platformsupport"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

var ErrInvalidTargetSelection = errors.New("invalid diagnostic target selection")

type Input struct {
	ManifestPath     string
	ManifestExplicit bool
	TargetExplicit   bool
	AllTargets       bool
	TargetValues     []string
}

type Result struct {
	ManifestPath     string
	ManifestExplicit bool
	Selection        targetselection.Selection
	Checks           []findings.Check
	HasErrors        bool
}

func Run(ctx context.Context, input Input, assessment platformsupport.PlatformAssessment) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("diagnose context is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	selection, err := targetselection.ForDiagnostics(input.TargetValues)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidTargetSelection, err)
	}

	result := Result{
		ManifestPath:     input.ManifestPath,
		ManifestExplicit: input.ManifestExplicit,
		Selection:        selection,
	}
	platformCheck := diagnose.PlatformCheck(assessment)

	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		result.Checks = []findings.Check{platformCheck, findings.ErrorCheck("paths", err.Error())}
		result.HasErrors = true
		return result, nil
	}
	result.ManifestPath = paths.ManifestPath
	if !assessment.IsAdmitted() {
		selection, checks := remainingChecks(ctx, input, paths, platformCheck, result.Selection, remainingCheckIndependent)
		return finishDiagnose(ctx, result, selection, checks)
	}
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		return result, err
	}
	if err := journal.RequireNoInterruptedApply(ctx, paths.RecoveryDir); err != nil {
		return result, err
	}

	selection, checks := remainingChecks(ctx, input, paths, platformCheck, result.Selection, remainingCheckFull)
	return finishDiagnose(ctx, result, selection, checks)
}

type remainingCheckMode uint8

const (
	remainingCheckFull remainingCheckMode = iota
	remainingCheckIndependent
)

func remainingChecks(
	ctx context.Context,
	input Input,
	paths daempaths.Paths,
	platformCheck findings.Check,
	selection targetselection.Selection,
	mode remainingCheckMode,
) (targetselection.Selection, []findings.Check) {
	loadedManifest := doctorManifestLoader.load(ctx, paths.ManifestPath)
	selection, resourceKinds := applyManifestDiagnosticContext(input, loadedManifest, selection)
	checks := []findings.Check{platformCheck}
	if mode == remainingCheckIndependent {
		checks = append(
			checks,
			findings.UnsupportedCheck("file_set", "durable file-set inventory cannot be honored on this platform"),
			findings.UnsupportedCheck("recovery", "interrupted-apply recovery inventory cannot be honored on this platform"),
		)
	}
	checks = append(checks, manifestCheck(paths.ManifestPath, input.ManifestExplicit, loadedManifest))
	if loadedManifest.ready() {
		if mode == remainingCheckIndependent {
			checks = append(checks, diagnose.HookCommandChecks(loadedManifest.facts.Hooks(), selection)...)
			if len(loadedManifest.facts.Skills()) > 0 || len(loadedManifest.facts.SkillSets()) > 0 {
				checks = append(checks, findings.SkippedCheck(
					"skill_observation",
					"skill-group expansion, compatibility, and retained discovery were not attempted; this platform cannot honor those host-observation contracts",
				))
			}
			checks = append(checks, diagnose.IndependentMCPExecutableRequirementChecks(loadedManifest.facts.MCPServers(), selection)...)
		} else {
			manifestSkills, skillInspectionChecks := diagnose.InspectManifestSkills(
				ctx,
				paths,
				loadedManifest.facts.Skills(),
				loadedManifest.facts.SkillSets(),
			)
			checks = append(checks, diagnose.HookCommandChecks(loadedManifest.facts.Hooks(), selection)...)
			checks = append(checks, skillInspectionChecks...)
			checks = append(checks, diagnose.ManifestSkillChecks(
				ctx,
				paths,
				manifestSkills,
				selection,
			)...)
			checks = append(checks, diagnose.RetainedSkillDiscoveryChecks(
				ctx,
				paths,
				manifestSkills,
				selection,
			)...)
			checks = append(checks, diagnose.MCPExecutableRequirementChecks(loadedManifest.facts.MCPServers(), selection)...)
		}
	}
	environment := diagnose.EnvironmentChecks
	if mode == remainingCheckIndependent {
		environment = diagnose.IndependentEnvironmentChecks
	}
	checks = append(checks, environment(
		ctx,
		paths,
		paths.ProjectPlacementAllowed(),
		selection,
		resourceKinds,
		input.TargetExplicit || input.AllTargets,
	)...)
	return selection, checks
}

func applyManifestDiagnosticContext(input Input, loadedManifest manifestLoad, selection targetselection.Selection) (targetselection.Selection, map[entity.Kind]struct{}) {
	if !input.TargetExplicit && !input.AllTargets && loadedManifest.ready() {
		selection = loadedManifest.facts.ContextSelection()
	}
	resourceKinds := allResourceKinds()
	if !input.TargetExplicit && !input.AllTargets && loadedManifest.ready() {
		resourceKinds = loadedManifest.facts.ResourceKinds()
	}
	return selection, resourceKinds
}

func finishDiagnose(ctx context.Context, result Result, selection targetselection.Selection, checks []findings.Check) (Result, error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Selection = selection
	result.Checks = checks
	result.HasErrors = findings.HasCheckErrors(checks)
	return result, nil
}
