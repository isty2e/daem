package adopt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	adoptskill "github.com/isty2e/daem/internal/adopt/skill"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/recoverygate"
	targetpkg "github.com/isty2e/daem/internal/target"
)

type CommandInput struct {
	TargetValues   []string
	ScopeValues    []string
	ManifestPath   string
	SourceDir      string
	Merge          bool
	ProgressEvents ProgressEventSink
}

type CommandPlan struct {
	request          adoptmodel.Request
	plan             adoptmodel.Plan
	skillSearchRoots *adoptskill.SearchRootCache
	revisions        mutation.RevisionSet
	stableRevisions  mutation.RevisionSet
	barrier          recoverygate.EffectAuthority
}

func BuildCommandPlan(ctx context.Context, input CommandInput) (CommandPlan, error) {
	if ctx == nil {
		return CommandPlan{}, fmt.Errorf("import context is required")
	}
	if err := ctx.Err(); err != nil {
		return CommandPlan{}, err
	}
	targets, err := commandTargets(input.TargetValues)
	if err != nil {
		return CommandPlan{}, err
	}
	scopes, err := commandScopes(input.ScopeValues)
	if err != nil {
		return CommandPlan{}, err
	}
	request, err := NewRequest(RequestInput{
		Targets:      targets,
		Scopes:       scopes,
		ManifestPath: input.ManifestPath,
		SourceDir:    input.SourceDir,
		Merge:        input.Merge,
	})
	if err != nil {
		return CommandPlan{}, err
	}
	paths, err := daempaths.Resolve(request.Output())
	if err != nil {
		return CommandPlan{}, err
	}
	barrier, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return CommandPlan{request: request}, err
	}
	if err := barrier.Validate(ctx); err != nil {
		return CommandPlan{request: request, barrier: barrier}, err
	}

	result := CommandPlan{request: request, barrier: barrier}
	progressTotal := importProgressTotal(request.Targets(), request.Scopes())
	input.ProgressEvents.emit(ProgressEvent{
		Kind:  ProgressEventPhaseStarted,
		Phase: ProgressPhaseDiscovery,
		Total: progressTotal,
	})
	observed, err := buildPlan(ctx, request, ProgressPhaseDiscovery, input.ProgressEvents)
	if err != nil {
		return result, err
	}
	result.plan = observed.plan
	result.skillSearchRoots = observed.skillSearchRoots
	if err := validateMCPSourceAuthoritiesCurrent(ctx, observed.plan); err != nil {
		return result, err
	}
	revisions, stableRevisions, err := captureImportRevisionEvidence(ctx, observed.plan, barrier)
	if err != nil {
		return result, err
	}
	if err := barrier.Validate(ctx); err != nil {
		return result, err
	}
	if err := observed.validateSkillSearchRoots(ctx); err != nil {
		return result, err
	}
	result.revisions = revisions
	result.stableRevisions = stableRevisions
	input.ProgressEvents.emit(ProgressEvent{
		Kind:      ProgressEventPhaseCompleted,
		Phase:     ProgressPhaseDiscovery,
		Completed: progressTotal,
		Total:     progressTotal,
	})
	return result, nil
}

func IsNothingToImport(err error) bool {
	return errors.Is(err, adoptmodel.ErrNothingToImport)
}

func (result CommandPlan) AdoptionPlan() adoptmodel.Plan {
	return result.plan
}

func (result CommandPlan) OutputPath() string {
	if result.plan.Output() != "" {
		return result.plan.Output()
	}
	return result.request.Output()
}

func (result CommandPlan) Merge() bool {
	if result.plan.Output() != "" {
		return result.plan.Merge()
	}
	return result.request.Merge()
}

func (result CommandPlan) HasMergeConflicts() bool {
	return result.plan.HasMergeConflicts()
}

func (result CommandPlan) ManifestDiff() (string, []byte, string, []byte) {
	if result.plan.Merge() {
		return result.plan.Output(), result.plan.OriginalContent(), result.plan.Output(), result.plan.ManifestContent()
	}
	return "/dev/null", nil, result.plan.Output(), result.plan.ManifestContent()
}

func commandTargets(values []string) ([]targetpkg.Target, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("--target is required")
	}

	targets := make([]targetpkg.Target, 0, len(values))
	seen := make(map[targetpkg.Target]struct{}, len(values))
	for _, value := range values {
		target, err := targetpkg.ParseTarget(value)
		if err != nil {
			return nil, err
		}
		if !profile.Profile(target).HasImportableDiscovery() {
			return nil, fmt.Errorf("target %q is not supported by import (accepted import targets: %s)", target, targetValues(profile.ImportableTargets()))
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}

	return targets, nil
}

func commandScopes(values []string) ([]targetpkg.Scope, error) {
	if len(values) == 0 {
		return []targetpkg.Scope{targetpkg.ScopeProject}, nil
	}

	scopes := make([]targetpkg.Scope, 0, len(values))
	seen := make(map[targetpkg.Scope]struct{}, len(values))
	for _, value := range values {
		scope, err := targetpkg.ParseScope(value)
		if err != nil {
			return nil, err
		}
		if scope != targetpkg.ScopeProject && scope != targetpkg.ScopeGlobal {
			return nil, fmt.Errorf("scope %q is not supported by import", scope)
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		scopes = append(scopes, scope)
	}

	return scopes, nil
}

func targetValues(targets []targetpkg.Target) string {
	values := make([]string, 0, len(targets))
	for _, target := range targets {
		values = append(values, string(target))
	}

	return strings.Join(values, ", ")
}
