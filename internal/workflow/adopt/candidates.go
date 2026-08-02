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
) ([]adoptmodel.Source, []adoptmodel.Skill, []adoptmodel.Hook, []adoptmodel.MCPServer, []adoptmodel.Scan, []adoptmodel.Skipped, error) {
	sources := make([]adoptmodel.Source, 0, 1)
	skipped := make([]adoptmodel.Skipped, 0, 2)
	instructionSources, instructionSkipped, err := adoptinstructions.Candidates(ctx, sourceDirectory, target, scope)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	sources = append(sources, instructionSources...)
	skipped = append(skipped, instructionSkipped...)

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
	skipped = append(skipped, skillSkipped...)

	hooks, hookSkipped, err := adopthook.Candidates(ctx, target, scope)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	skipped = append(skipped, hookSkipped...)

	mcpServers, mcpSkipped, err := adoptmcp.Candidates(ctx, target, scope)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	skipped = append(skipped, mcpSkipped...)

	return sources, skills, hooks, mcpServers, skillScans, skipped, nil
}
