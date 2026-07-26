package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

// Options controls lock/build source execution policy.
type Options struct {
	MaxParallelSourceOps    int
	Events                  EventSink
	SourceEvents            acquisition.EventSink
	HookContributionEncoder commandhook.ContributionEncoder
	MCPContributionEncoder  refine.MCPContributionEncoder
}

// BuildWithOptions resolves lockable resources into a canonical lock file.
func BuildWithOptions(ctx context.Context, environment desired.Environment, resolver acquisition.Resolver, options Options) (lock.File, error) {
	if err := ctx.Err(); err != nil {
		return lock.File{}, err
	}
	if err := environment.Validate(); err != nil {
		return lock.File{}, fmt.Errorf("desired environment: %w", err)
	}
	if sessionProvider, ok := resolver.(acquisition.ResolutionSessionProvider); ok {
		sessionResolver, err := sessionProvider.NewResolutionSession()
		if err != nil {
			return lock.File{}, fmt.Errorf("begin source resolution session: %w", err)
		}
		if sessionResolver == nil {
			return lock.File{}, fmt.Errorf("begin source resolution session: provider returned nil resolver")
		}
		resolver = sessionResolver
	}

	skills, err := lockableSkills(ctx, environment.Skills(), environment.SkillSets(), resolver, options)
	if err != nil {
		return lock.File{}, err
	}
	hookAssets := environment.HookAssets()
	instructionResources := environment.Instructions()
	hookTopology, err := topologyhook.Lower(hookAssets, environment.Hooks())
	if err != nil {
		return lock.File{}, err
	}

	concreteSources, err := resolveConcreteSources(ctx, skills, hookAssets, instructionResources, resolver, options)
	if err != nil {
		return lock.File{}, err
	}

	skillInputs, err := newSkillLockAssemblyInputs(skills, concreteSources.skills)
	if err != nil {
		return lock.File{}, err
	}
	assemblyInput := lockAssemblyInput{Skills: skillInputs}
	if err := assemblyInput.validate(); err != nil {
		return lock.File{}, err
	}
	lockedSkills, err := lockResolvedSkills(ctx, assemblyInput.Skills, options)
	if err != nil {
		return lock.File{}, err
	}

	instructionInputs, err := newInstructionLockAssemblyInputs(instructionResources, concreteSources.instructions)
	if err != nil {
		return lock.File{}, err
	}
	assemblyInput.Instructions = instructionInputs
	if err := assemblyInput.validate(); err != nil {
		return lock.File{}, err
	}
	lockedInstructions, err := lockResolvedInstructions(ctx, assemblyInput.Instructions, options)
	if err != nil {
		return lock.File{}, err
	}

	hookAssetInputs, err := newHookAssetLockAssemblyInputs(hookAssets, concreteSources.hookAssets)
	if err != nil {
		return lock.File{}, err
	}
	assemblyInput.HookAssets = hookAssetInputs
	if err := assemblyInput.validate(); err != nil {
		return lock.File{}, err
	}
	lockedHookAssets, err := lockResolvedHookAssets(ctx, assemblyInput.HookAssets, options)
	if err != nil {
		return lock.File{}, err
	}
	lockedHookAssetPaths, err := refine.HookAssetPathProjections(hookAssets, hookTopology, lockedHookAssets)
	if err != nil {
		return lock.File{}, err
	}
	lockedHooks, err := refine.HookContributions(
		environment.Hooks(),
		hookTopology,
		options.HookContributionEncoder,
	)
	if err != nil {
		return lock.File{}, err
	}

	lockedMCPSubjects, err := refine.MCPSubjects(
		environment.MCPServers(),
		options.MCPContributionEncoder,
	)
	if err != nil {
		emitSnapshotValidationFailed(options.Events, err)
		return lock.File{}, err
	}
	lockedExtensionSubjects, err := refine.Extensions(environment.Extensions())
	if err != nil {
		emitSnapshotValidationFailed(options.Events, err)
		return lock.File{}, err
	}
	lockedSubjects := make(
		[]lock.LockedSubjectContract, 0,
		len(lockedSkills)+len(lockedInstructions)+len(lockedHookAssets)+len(lockedHookAssetPaths)+len(lockedHooks)+
			len(lockedMCPSubjects)+len(lockedExtensionSubjects),
	)
	lockedSubjects = append(lockedSubjects, lockedSkills...)
	lockedSubjects = append(lockedSubjects, lockedInstructions...)
	lockedSubjects = append(lockedSubjects, lockedHookAssets...)
	lockedSubjects = append(lockedSubjects, lockedHookAssetPaths...)
	lockedSubjects = append(lockedSubjects, lockedHooks...)
	lockedSubjects = append(lockedSubjects, lockedMCPSubjects...)
	lockedSubjects = append(lockedSubjects, lockedExtensionSubjects...)
	lockedSection, err := lock.NewLockedSection(lockedSubjects)
	if err != nil {
		emitSnapshotValidationFailed(options.Events, err)
		return lock.File{}, err
	}

	file := lock.File{
		Version: lock.CurrentVersion,
		Locked:  lockedSection,
	}
	if err := lock.Validate(file); err != nil {
		emitSnapshotValidationFailed(options.Events, err)
		return lock.File{}, err
	}

	options.Events.Emit(Event{
		Kind:  EventSnapshotValidated,
		Stage: EventStageSnapshot,
		Count: lockedSection.Len(),
	})

	return file, nil
}

func emitSnapshotValidationFailed(events EventSink, err error) {
	events.Emit(Event{
		Kind:  EventSnapshotValidationFailed,
		Stage: EventStageSnapshot,
		Err:   err,
	})
}
