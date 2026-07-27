package diagnoseworkflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/declaration/transaction"
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

func Run(ctx context.Context, input Input, admission platformsupport.Admission) (Result, error) {
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
	platformCheck := diagnose.PlatformCheck(admission)

	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		result.Checks = []findings.Check{platformCheck, findings.ErrorCheck("paths", err.Error())}
		result.HasErrors = true
		return result, nil
	}
	result.ManifestPath = paths.ManifestPath
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		return result, err
	}
	if err := journal.EnsureNoActive(paths.RecoveryDir); err != nil {
		return result, err
	}

	loadedManifest := doctorManifestLoader.load(paths.ManifestPath)
	if !input.TargetExplicit && !input.AllTargets && loadedManifest.ready() {
		selection = loadedManifest.facts.ContextSelection()
		result.Selection = selection
	}

	resourceKinds := allResourceKinds()
	if !input.TargetExplicit && !input.AllTargets && loadedManifest.ready() {
		resourceKinds = loadedManifest.facts.ResourceKinds()
	}

	checks := []findings.Check{platformCheck, manifestCheck(paths.ManifestPath, input.ManifestExplicit, loadedManifest)}
	if loadedManifest.ready() {
		checks = append(checks, diagnose.HookCommandChecks(loadedManifest.facts.Hooks(), selection)...)
		checks = append(checks, diagnose.ManifestSkillChecks(
			ctx,
			paths,
			loadedManifest.facts.Skills(),
			loadedManifest.facts.SkillSets(),
			selection,
		)...)
		checks = append(checks, diagnose.RetainedSkillDiscoveryChecks(
			ctx,
			paths,
			loadedManifest.facts.Skills(),
			loadedManifest.facts.SkillSets(),
			selection,
		)...)
		checks = append(checks, diagnose.MCPExecutableRequirementChecks(loadedManifest.facts.MCPServers(), selection)...)
	}
	checks = append(checks, diagnose.EnvironmentChecks(
		ctx,
		paths,
		paths.ProjectPlacementAllowed(),
		selection,
		resourceKinds,
		input.TargetExplicit || input.AllTargets,
	)...)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Checks = checks
	result.HasErrors = findings.HasCheckErrors(checks)

	return result, nil
}
