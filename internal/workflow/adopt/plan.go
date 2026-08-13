package adopt

import (
	"context"
	"fmt"
	"path/filepath"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	adoptextension "github.com/isty2e/daem/internal/adopt/extension"
	adoptmerge "github.com/isty2e/daem/internal/adopt/merge"
	adoptskill "github.com/isty2e/daem/internal/adopt/skill"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/desired"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/artifact/access"
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
	stagingStructureLimits := storagecommit.RootedTreeStagingStructureLimits()
	skillTreeLimit, err := access.NewTreeStructureLimit(
		stagingStructureLimits.MaximumEntries(),
		stagingStructureLimits.MaximumDepth(),
	)
	if err != nil {
		return adoptmodel.Plan{}, fmt.Errorf("derive skill import staging limit: %w", err)
	}
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
	var existingEnvironment desired.Environment
	var existingExtensions []desiredextension.Extension
	if merge {
		originalContent, err = declarationartifact.Read(ctx, output)
		if err != nil {
			return adoptmodel.Plan{}, fmt.Errorf("read merge output manifest: %w", err)
		}
		existingEnvironment, err = declarationmanifest.Decode(originalContent)
		if err != nil {
			return adoptmodel.Plan{}, fmt.Errorf("decode merge output manifest: %w", err)
		}
		existingExtensions = existingEnvironment.Extensions()
	}
	var sources []adoptmodel.Source
	var skills []adoptmodel.Skill
	var hooks []adoptmodel.Hook
	var mcpServers []adoptmodel.MCPServer
	var scans []adoptmodel.Scan
	var skipped []adoptmodel.Skipped
	extensionResult, err := adoptextension.Collect(adoptextension.Input{
		ManifestRoot: filepath.Dir(output),
		Targets:      request.Targets(),
		Scopes:       request.Scopes(),
		Existing:     existingExtensions,
	})
	if err != nil {
		return adoptmodel.Plan{}, fmt.Errorf("import extensions: %w", err)
	}
	for _, scan := range extensionResult.Scans() {
		scans = append(scans, adoptmodel.Scan{
			ResourceKind: "extension",
			ResourceName: "inventory",
			Target:       scan.Target,
			Scope:        scan.Scope,
			LivePath:     scan.LivePath,
			Status:       "observed",
			Entries:      scan.Entries,
			Imported:     scan.Imported,
			Skipped:      scan.Skipped,
		})
	}
	for _, skip := range extensionResult.Skipped() {
		skipped = append(skipped, adoptmodel.Skipped{
			LivePath: skip.LivePath,
			Reason:   skip.Reason,
		})
	}
	importedSkillDestinations := adoptskill.NewDestinationClaims()
	skillSourceIdentities := adoptskill.NewSourceIdentityCache(skillTreeLimit)
	for _, target := range request.Targets() {
		for _, scope := range request.Scopes() {
			if err := ctx.Err(); err != nil {
				return adoptmodel.Plan{}, err
			}
			importedSources, importedSkills, importedHooks, importedMCPServers, observedScans, observedSkipped, err := importCandidates(
				ctx,
				sourceDirectory,
				target,
				scope,
				importedSkillDestinations,
				skillSourceIdentities,
			)
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
	skills, err = adoptskill.Finalize(skills)
	if err != nil {
		return adoptmodel.Plan{}, err
	}
	skills, err = adoptskill.AssignGroupSources(sourceDirectory, skills)
	if err != nil {
		return adoptmodel.Plan{}, err
	}

	candidates, err := adoptmodel.NewCandidateSet(adoptmodel.CandidateSetInput{
		Sources:    sources,
		Skills:     skills,
		Hooks:      hooks,
		MCPServers: mcpServers,
		Extensions: extensionResult,
		Scans:      scans,
		Skipped:    skipped,
	})
	if err != nil {
		return adoptmodel.Plan{}, err
	}
	if candidates.ResourceCount() == 0 {
		return adoptmodel.Plan{}, fmt.Errorf("%w%s%s", adoptmodel.ErrNothingToImport, scanSummary(scans), skippedSummary(skipped))
	}

	var mergedPlan adoptmodel.Plan
	if merge {
		var selectorBackedSkills []desiredskill.Skill
		if len(skills) != 0 && len(existingEnvironment.SkillSets()) != 0 {
			paths, resolveErr := daempaths.Resolve(output)
			if resolveErr != nil {
				return adoptmodel.Plan{}, resolveErr
			}
			selectorBackedSkills, err = lockedSelectorBackedSkills(
				ctx,
				paths.LockfilePath,
				existingEnvironment,
			)
			if err != nil {
				return adoptmodel.Plan{}, err
			}
		}
		mergedPlan, err = adoptmerge.IntoManifest(
			request,
			originalContent,
			candidates,
			selectorBackedSkills,
		)
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
	manifestContent, err := adoptmodel.RenderManifestContent(
		sources,
		skills,
		hooks,
		mcpServers,
		candidates.Extensions(),
	)
	if err != nil {
		return adoptmodel.Plan{}, err
	}
	return adoptmodel.NewPlan(request, nil, manifestContent, candidates, nil)
}
