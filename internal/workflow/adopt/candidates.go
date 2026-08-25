package adopt

import (
	"context"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	adopthook "github.com/isty2e/daem/internal/adopt/hook"
	adoptinstructions "github.com/isty2e/daem/internal/adopt/instructions"
	adoptmcp "github.com/isty2e/daem/internal/adopt/mcp"
	adoptskill "github.com/isty2e/daem/internal/adopt/skill"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func importCandidates(
	ctx context.Context,
	sourceDirectory adoptmodel.SourceDirectory,
	target targetpkg.Target,
	scope targetpkg.Scope,
	importedSkillDestinations adoptskill.DestinationClaims,
	skillSourceIdentities *adoptskill.SourceIdentityCache,
	skippedCollector *adoptmodel.SkippedCollector,
) (
	[]adoptmodel.Source,
	[]adoptmodel.Skill,
	[]adoptmodel.Hook,
	[]adoptmodel.MCPServer,
	[]adoptmodel.MCPSourceAuthority,
	[]adoptmodel.Scan,
	error,
) {
	sources := make([]adoptmodel.Source, 0, 1)
	instructionSources, instructionSkipped, err := adoptinstructions.Candidates(ctx, sourceDirectory, target, scope)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	sources = append(sources, instructionSources...)
	if err := appendRouteSkipped(skippedCollector, instructionSkipped, target, scope); err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	skills, skillScans, skillSkipped, err := adoptskill.Candidates(
		ctx,
		sourceDirectory,
		target,
		scope,
		importedSkillDestinations,
		skillSourceIdentities,
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	if err := appendRouteSkipped(skippedCollector, skillSkipped, target, scope); err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	hooks, hookSkipped, err := adopthook.Candidates(ctx, target, scope)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	if err := appendRouteSkipped(skippedCollector, hookSkipped, target, scope); err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	mcpServers, mcpAuthorities, mcpSkipped, err := adoptmcp.Candidates(ctx, target, scope)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	if err := appendRouteSkipped(skippedCollector, mcpSkipped, target, scope); err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	return sources, skills, hooks, mcpServers, mcpAuthorities, skillScans, nil
}

func appendRouteSkipped(
	collector *adoptmodel.SkippedCollector,
	values []adoptmodel.Skipped,
	target targetpkg.Target,
	scope targetpkg.Scope,
) error {
	for _, value := range values {
		value.Target = target
		value.Scope = scope
		if err := collector.Add(value); err != nil {
			return err
		}
	}
	return nil
}
