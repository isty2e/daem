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
	skillSearchRoots *adoptskill.SearchRootCache,
	skipped adoptmodel.SkipEmitter,
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
	instructionSources, err := adoptinstructions.Candidates(ctx, sourceDirectory, target, scope, skipped)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	sources = append(sources, instructionSources...)

	skills, skillScans, err := adoptskill.Candidates(
		ctx,
		sourceDirectory,
		target,
		scope,
		importedSkillDestinations,
		skillSourceIdentities,
		skillSearchRoots,
		skipped,
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	hooks, err := adopthook.Candidates(ctx, target, scope, skipped)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	mcpServers, mcpAuthorities, err := adoptmcp.Candidates(ctx, target, scope, skipped)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	return sources, skills, hooks, mcpServers, mcpAuthorities, skillScans, nil
}
