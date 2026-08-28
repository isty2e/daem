package adopt

import (
	"context"
	"errors"
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

type observedImportPlan struct {
	plan             adoptmodel.Plan
	skillSearchRoots *adoptskill.SearchRootCache
}

func (observed observedImportPlan) validateSkillSearchRoots(ctx context.Context) error {
	if observed.skillSearchRoots == nil {
		return nil
	}
	if err := observed.skillSearchRoots.Validate(ctx); err != nil {
		return fmt.Errorf("revalidate Skill search roots: %w", err)
	}
	return nil
}

func buildPlan(
	ctx context.Context,
	request adoptmodel.Request,
	progressPhase ProgressPhase,
	progressEvents ProgressEventSink,
) (observedImportPlan, error) {
	if ctx == nil {
		return observedImportPlan{}, fmt.Errorf("import context is required")
	}
	if err := ctx.Err(); err != nil {
		return observedImportPlan{}, err
	}
	if err := request.Validate(); err != nil {
		return observedImportPlan{}, err
	}
	requestTargets := request.Targets()
	requestScopes := request.Scopes()
	progressTotal := importProgressTotal(requestTargets, requestScopes)
	output := request.Output()
	sourceDirectory := request.SourceDirectory()
	merge := request.Merge()
	outputExists, err := pathExists(output)
	if err != nil {
		return observedImportPlan{}, fmt.Errorf("inspect output manifest: %w", err)
	}
	if merge && !outputExists {
		return observedImportPlan{}, fmt.Errorf("merge output manifest does not exist: %s", output)
	}
	if !merge && outputExists {
		return observedImportPlan{}, fmt.Errorf("output manifest already exists: %s", output)
	}

	var originalContent []byte
	var existingEnvironment desired.Environment
	var existingExtensions []desiredextension.Extension
	if merge {
		originalContent, err = declarationartifact.Read(ctx, output)
		if err != nil {
			return observedImportPlan{}, fmt.Errorf("read merge output manifest: %w", err)
		}
		existingEnvironment, err = declarationmanifest.Decode(originalContent)
		if err != nil {
			return observedImportPlan{}, fmt.Errorf("decode merge output manifest: %w", err)
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
	var extensionResult adoptextension.Result
	err = skippedCollector.Collect(func(skipped adoptmodel.SkipEmitter) error {
		var collectErr error
		extensionResult, collectErr = adoptextension.Collect(ctx, adoptextension.Input{
			ManifestRoot: filepath.Dir(output),
			Targets:      requestTargets,
			Scopes:       requestScopes,
			Existing:     existingExtensions,
		}, func(value adoptextension.Skip) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return skipped.Add(adoptmodel.Skipped{
				Target:   value.Target,
				Scope:    value.Scope,
				LivePath: value.LivePath,
				Reason:   adoptmodel.SkipReason(value.Reason),
			})
		})
		if collectErr != nil {
			return collectErr
		}
		return ctx.Err()
	})
	if err != nil {
		if errors.Is(err, adoptmodel.ErrSkipObservationLimitExceeded) {
			return observedImportPlan{}, wrapSkippedObservationError(err, skippedCollector)
		}
		return observedImportPlan{}, fmt.Errorf("import extensions: %w", err)
	}
	for _, scan := range extensionResult.Scans() {
		evidence, err := adoptmodel.NewBoundedFileScanEvidence(scan.MaximumBytes)
		if err != nil {
			return observedImportPlan{}, fmt.Errorf("import extension scan evidence: %w", err)
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
	importedSkillDestinations := adoptskill.NewDestinationClaims()
	skillSourceIdentities := adoptskill.NewSourceIdentityCache(mutationfs.DefaultTreeTraversalLimits())
	skillSearchRoots := adoptskill.NewSearchRootCache()
	completedTargetScopes := 0
	for _, target := range requestTargets {
		for _, scope := range requestScopes {
			if err := ctx.Err(); err != nil {
				return observedImportPlan{}, err
			}
			progressEvents.emit(ProgressEvent{
				Kind:      ProgressEventTargetScopeStarted,
				Phase:     progressPhase,
				Target:    target,
				Scope:     scope,
				Completed: completedTargetScopes,
				Total:     progressTotal,
			})
			var importedSources []adoptmodel.Source
			var importedSkills []adoptmodel.Skill
			var importedHooks []adoptmodel.Hook
			var importedMCPServers []adoptmodel.MCPServer
			var observedMCPAuthorities []adoptmodel.MCPSourceAuthority
			var observedScans []adoptmodel.Scan
			err := skippedCollector.Collect(func(skipped adoptmodel.SkipEmitter) error {
				var importErr error
				importedSources, importedSkills, importedHooks, importedMCPServers, observedMCPAuthorities, observedScans, importErr = importCandidates(
					ctx,
					sourceDirectory,
					target,
					scope,
					importedSkillDestinations,
					skillSourceIdentities,
					skillSearchRoots,
					skipped.WithRoute(target, scope),
				)
				return importErr
			})
			if err != nil {
				return observedImportPlan{}, wrapSkippedObservationError(err, skippedCollector)
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
		return observedImportPlan{}, err
	}
	skills, err = adoptskill.AssignGroupSources(sourceDirectory, skills)
	if err != nil {
		return observedImportPlan{}, err
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
		return observedImportPlan{}, err
	}
	observed := observedImportPlan{
		skillSearchRoots: skillSearchRoots,
	}
	if candidates.ResourceCount() == 0 {
		if err := observed.validateSkillSearchRoots(ctx); err != nil {
			return observedImportPlan{}, err
		}
		return observedImportPlan{}, newNothingToImportError(scans, skipped)
	}

	var mergedPlan adoptmodel.Plan
	if merge {
		var selectorBackedSkills []desiredskill.Skill
		if len(skills) != 0 && len(existingEnvironment.SkillSets()) != 0 {
			paths, resolveErr := daempaths.Resolve(output)
			if resolveErr != nil {
				return observedImportPlan{}, resolveErr
			}
			selectorBackedSkills, err = lockedSelectorBackedSkills(
				ctx,
				paths.LockfilePath,
				existingEnvironment,
			)
			if err != nil {
				return observedImportPlan{}, err
			}
		}
		mergedPlan, err = adoptmerge.IntoManifest(
			request,
			originalContent,
			candidates,
			selectorBackedSkills,
		)
		if err != nil {
			return observedImportPlan{}, err
		}
		if mergedPlan.HasMergeConflicts() {
			if err := observed.validateSkillSearchRoots(ctx); err != nil {
				return observedImportPlan{}, err
			}
			observed.plan = mergedPlan
			return observed, nil
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
			return observedImportPlan{}, fmt.Errorf("inspect imported Source: %w", err)
		} else if exists {
			return observedImportPlan{}, fmt.Errorf("imported source already exists: %s", source.SourcePath)
		}
	}
	checkedSkillGroupRoots := make(map[string]struct{}, len(skills))
	for _, skill := range skills {
		if skill.GroupRoot != "" {
			if _, checked := checkedSkillGroupRoots[skill.GroupRoot]; !checked {
				checkedSkillGroupRoots[skill.GroupRoot] = struct{}{}
				if exists, err := pathExists(skill.GroupRoot); err != nil {
					return observedImportPlan{}, fmt.Errorf("inspect imported skill group Source: %w", err)
				} else if exists {
					return observedImportPlan{}, fmt.Errorf("imported skill group source already exists: %s", skill.GroupRoot)
				}
			}
		}
		if _, checked := checkedSources[skill.SourcePath]; checked {
			continue
		}
		checkedSources[skill.SourcePath] = struct{}{}
		if exists, err := pathExists(skill.SourcePath); err != nil {
			return observedImportPlan{}, fmt.Errorf("inspect imported skill Source: %w", err)
		} else if exists {
			return observedImportPlan{}, fmt.Errorf("imported skill source already exists: %s", skill.SourcePath)
		}
	}

	if merge {
		if err := observed.validateSkillSearchRoots(ctx); err != nil {
			return observedImportPlan{}, err
		}
		observed.plan = mergedPlan
		return observed, nil
	}
	manifestContent, err := adoptmodel.RenderManifestContent(
		sources,
		skills,
		hooks,
		mcpServers,
		candidates.Extensions(),
	)
	if err != nil {
		return observedImportPlan{}, err
	}
	plan, err := adoptmodel.NewPlan(request, nil, manifestContent, candidates, nil)
	if err != nil {
		return observedImportPlan{}, err
	}
	if err := observed.validateSkillSearchRoots(ctx); err != nil {
		return observedImportPlan{}, err
	}
	observed.plan = plan
	return observed, nil
}
