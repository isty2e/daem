package adopt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/journal"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/profile"
	targetpkg "github.com/isty2e/daem/internal/target"
)

type CommandInput struct {
	TargetValues []string
	ScopeValues  []string
	ManifestPath string
	SourceDir    string
	Merge        bool
}

type CommandPlan struct {
	request adoptmodel.Request
	plan    adoptmodel.Plan
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
	if err := journal.EnsureNoActive(paths.RecoveryDir); err != nil {
		return CommandPlan{request: request}, err
	}
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		return CommandPlan{request: request}, err
	}

	result := CommandPlan{request: request}
	plan, err := BuildPlan(ctx, request)
	if err != nil {
		return result, err
	}
	result.plan = plan
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
