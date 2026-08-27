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
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func buildPlan(
	ctx context.Context,
	request adoptmodel.Request,
	progressPhase ProgressPhase,
	progressEvents ProgressEventSink,
) (adoptmodel.Plan, error) {
	if ctx == nil {
		return adoptmodel.Plan{}, fmt.Errorf("import context is required")
	}
	if err := ctx.Err(); err != nil {
		return adoptmodel.Plan{}, err
	}
	if err := request.Validate(); err != nil {
		return adoptmodel.Plan{}, err
	}
	requestTargets := request.Targets()
	requestScopes := request.Scopes()
	progressTotal := importProgressTotal(requestTargets, requestScopes)
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
	var mcpAuthorities []adoptmodel.MCPSourceAuthority
	var scans []adoptmodel.Scan
	skippedCollector := adoptmodel.NewSkippedCollector()
	extensionResult, err := adoptextension.Collect(adoptextension.Input{
		ManifestRoot: filepath.Dir(output),
		Targets:      requestTargets,
		Scopes:       requestScopes,
		Existing:     existingExtensions,
	})
	if err != nil {
		return adoptmodel.Plan{}, fmt.Errorf("import extensions: %w", err)
	}
	for _, scan := range extensionResult.Scans() {
		evidence, err := adoptmodel.NewBoundedFileScanEvidence(scan.MaximumBytes)
		if err != nil {
			return adoptmodel.Plan{}, fmt.Errorf("import extension scan evidence: %w", err)
		}
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
			Evidence:     evidence,
		})
	}
	for _, skip := range extensionResult.Skipped() {
		if err := skippedCollector.Add(adoptmodel.Skipped{
			Target:   skip.Target,
			Scope:    skip.Scope,
			LivePath: skip.LivePath,
			Reason:   adoptmodel.SkipReason(skip.Reason),
		}); err != nil {
			return adoptmodel.Plan{}, wrapSkippedObservationError(err, skippedCollector)
		}
	}
	importedSkillDestinations := adoptskill.NewDestinationClaims()
	skillSourceIdentities := adoptskill.NewSourceIdentityCache(mutationfs.DefaultTreeTraversalLimits())
	skillSearchRoots := adoptskill.NewSearchRootCache()
	completedTargetScopes := 0
	for _, target := range requestTargets {
		for _, scope := range requestScopes {
			if err := ctx.Err(); err != nil {
				return adoptmodel.Plan{}, err
			}
			progressEvents.emit(ProgressEvent{
				Kind:      ProgressEventTargetScopeStarted,
				Phase:     progressPhase,
				Target:    target,
				Scope:     scope,
				Completed: completedTargetScopes,
				Total:     progressTotal,
			})
			importedSources, importedSkills, importedHooks, importedMCPServers, observedMCPAuthorities, observedScans, err := importCandidates(
				ctx,
				sourceDirectory,
				target,
				scope,
				importedSkillDestinations,
				skillSourceIdentities,
				skillSearchRoots,
				skippedCollector,
			)
			if err != nil {
				return adoptmodel.Plan{}, wrapSkippedObservationError(err, skippedCollector)
			}
			scans = append(scans, observedScans...)
			sources = append(sources, importedSources...)
			skills = append(skills, importedSkills...)
			hooks = append(hooks, importedHooks...)
			mcpServers = append(mcpServers, importedMCPServers...)
			mcpAuthorities = append(mcpAuthorities, observedMCPAuthorities...)
			completedTargetScopes++
			progressEvents.emit(ProgressEvent{
				Kind:      ProgressEventTargetScopeCompleted,
				Phase:     progressPhase,
				Target:    target,
				Scope:     scope,
				Completed: completedTargetScopes,
				Total:     progressTotal,
			})
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

	skipped := skippedCollector.Skipped()
	candidates, err := adoptmodel.NewCandidateSet(adoptmodel.CandidateSetInput{
		Sources:              sources,
		Skills:               skills,
		Hooks:                hooks,
		MCPServers:           mcpServers,
		MCPSourceAuthorities: mcpAuthorities,
		Extensions:           extensionResult,
		Scans:                scans,
		Skipped:              skipped,
	})
	if err != nil {
		return adoptmodel.Plan{}, err
	}
	validateSkillSearchRoots := func() error {
		if err := skillSearchRoots.Validate(ctx); err != nil {
			return fmt.Errorf("revalidate Skill search roots: %w", err)
		}
		return nil
	}
	if candidates.ResourceCount() == 0 {
		if err := validateSkillSearchRoots(); err != nil {
			return adoptmodel.Plan{}, err
		}
		return adoptmodel.Plan{}, newNothingToImportError(scans, skipped)
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
			if err := validateSkillSearchRoots(); err != nil {
				return adoptmodel.Plan{}, err
			}
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
		if err := validateSkillSearchRoots(); err != nil {
			return adoptmodel.Plan{}, err
		}
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
	plan, err := adoptmodel.NewPlan(request, nil, manifestContent, candidates, nil)
	if err != nil {
		return adoptmodel.Plan{}, err
	}
	if err := validateSkillSearchRoots(); err != nil {
		return adoptmodel.Plan{}, err
	}
	return plan, nil
}
