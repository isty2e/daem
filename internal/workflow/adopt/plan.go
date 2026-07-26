package adopt

import (
	"context"
	"fmt"
	"os"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	adoptmerge "github.com/isty2e/daem/internal/adopt/merge"
	adoptskill "github.com/isty2e/daem/internal/adopt/skill"
)

// BuildPlan scans selected live agent resources and produces a manifest import plan.
func BuildPlan(ctx context.Context, request adoptmodel.Request) (adoptmodel.Plan, error) {
	if ctx == nil {
		return adoptmodel.Plan{}, fmt.Errorf("import context is required")
	}
	if err := ctx.Err(); err != nil {
		return adoptmodel.Plan{}, err
	}
	if err := request.Validate(); err != nil {
		return adoptmodel.Plan{}, err
	}
	output := request.Output()
	sourceDirectory := request.SourceDirectory()
	merge := request.Merge()
	outputExists, err := pathExists(output)
	if err != nil {
		return adoptmodel.Plan{}, fmt.Errorf("inspect output manifest: %w", err)
	}
	if merge && !outputExists {
		return adoptmodel.Plan{}, fmt.Errorf("merge output manifest does not exist: %s", output)
	}
	if !merge && outputExists {
		return adoptmodel.Plan{}, fmt.Errorf("output manifest already exists: %s", output)
	}

	var originalContent []byte
	if merge {
		originalContent, err = os.ReadFile(output)
		if err != nil {
			return adoptmodel.Plan{}, fmt.Errorf("read merge output manifest: %w", err)
		}
	}
	var sources []adoptmodel.Source
	var skills []adoptmodel.Skill
	var hooks []adoptmodel.Hook
	var mcpServers []adoptmodel.MCPServer
	var scans []adoptmodel.Scan
	var skipped []adoptmodel.Skipped
	importedSkillDestinations := adoptskill.NewDestinationClaims()
	for _, target := range request.Targets() {
		for _, scope := range request.Scopes() {
			if err := ctx.Err(); err != nil {
				return adoptmodel.Plan{}, err
			}
			importedSources, importedSkills, importedHooks, importedMCPServers, observedScans, observedSkipped, err := importCandidates(ctx, sourceDirectory, target, scope, importedSkillDestinations)
			if err != nil {
				return adoptmodel.Plan{}, err
			}
			scans = append(scans, observedScans...)
			skipped = append(skipped, observedSkipped...)
			sources = append(sources, importedSources...)
			skills = append(skills, importedSkills...)
			hooks = append(hooks, importedHooks...)
			mcpServers = append(mcpServers, importedMCPServers...)
		}
	}
	skills = adoptskill.Finalize(skills)
	skills, err = adoptskill.AssignGroupSources(sourceDirectory, skills)
	if err != nil {
		return adoptmodel.Plan{}, err
	}

	candidates, err := adoptmodel.NewCandidateSet(sources, skills, hooks, mcpServers, scans, skipped)
	if err != nil {
		return adoptmodel.Plan{}, err
	}
	if candidates.ResourceCount() == 0 {
		return adoptmodel.Plan{}, fmt.Errorf("%w%s%s", adoptmodel.ErrNothingToImport, scanSummary(scans), skippedSummary(skipped))
	}

	var mergedPlan adoptmodel.Plan
	if merge {
		mergedPlan, err = adoptmerge.IntoManifest(request, originalContent, candidates)
		if err != nil {
			return adoptmodel.Plan{}, err
		}
		if mergedPlan.HasMergeConflicts() {
			return mergedPlan, nil
		}
		sources = mergedPlan.Sources()
		skills = mergedPlan.Skills()
	}

	checkedSources := make(map[string]struct{}, len(sources)+len(skills))
	for _, source := range sources {
		if _, checked := checkedSources[source.SourcePath]; checked {
			continue
		}
		checkedSources[source.SourcePath] = struct{}{}
		if exists, err := pathExists(source.SourcePath); err != nil {
			return adoptmodel.Plan{}, fmt.Errorf("inspect imported Source: %w", err)
		} else if exists {
			return adoptmodel.Plan{}, fmt.Errorf("imported source already exists: %s", source.SourcePath)
		}
	}
	checkedSkillGroupRoots := make(map[string]struct{}, len(skills))
	for _, skill := range skills {
		if skill.GroupRoot != "" {
			if _, checked := checkedSkillGroupRoots[skill.GroupRoot]; !checked {
				checkedSkillGroupRoots[skill.GroupRoot] = struct{}{}
				if exists, err := pathExists(skill.GroupRoot); err != nil {
					return adoptmodel.Plan{}, fmt.Errorf("inspect imported skill group Source: %w", err)
				} else if exists {
					return adoptmodel.Plan{}, fmt.Errorf("imported skill group source already exists: %s", skill.GroupRoot)
				}
			}
		}
		if _, checked := checkedSources[skill.SourcePath]; checked {
			continue
		}
		checkedSources[skill.SourcePath] = struct{}{}
		if exists, err := pathExists(skill.SourcePath); err != nil {
			return adoptmodel.Plan{}, fmt.Errorf("inspect imported skill Source: %w", err)
		} else if exists {
			return adoptmodel.Plan{}, fmt.Errorf("imported skill source already exists: %s", skill.SourcePath)
		}
	}

	if merge {
		return mergedPlan, nil
	}
	manifestContent, err := adoptmodel.RenderManifestContent(sources, skills, hooks, mcpServers)
	if err != nil {
		return adoptmodel.Plan{}, err
	}
	return adoptmodel.NewPlan(request, nil, manifestContent, candidates, nil)
}
