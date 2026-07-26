package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/desired/hookasset"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

type concreteSourceResults struct {
	skills       []sourceTaskResult
	hookAssets   []sourceTaskResult
	instructions []sourceTaskResult
}

func resolveConcreteSources(
	ctx context.Context,
	skills []lockableSkill,
	hookAssets []hookasset.HookAsset,
	instructionResources []instructions.Instructions,
	resolver acquisition.Resolver,
	options Options,
) (concreteSourceResults, error) {
	tasks := make([]sourceTask, 0, len(skills)+len(hookAssets)+len(instructionResources))
	for ordinal, lockable := range skills {
		tasks = append(tasks, newSkillResolveTask(ordinal, lockable))
	}
	for ordinal, asset := range hookAssets {
		tasks = append(tasks, newHookAssetResolveTask(ordinal, asset))
	}
	for ordinal, instruction := range instructionResources {
		tasks = append(tasks, newInstructionsResolveTask(ordinal, instruction))
	}

	results, err := sourceTaskResults(ctx, resolver, tasks, options)
	if err != nil {
		return concreteSourceResults{}, err
	}
	if len(results) != len(tasks) {
		if err := firstSourceTaskError(ctx, results); err != nil {
			return concreteSourceResults{}, err
		}
		return concreteSourceResults{}, fmt.Errorf("concrete source resolution returned %d results for %d tasks", len(results), len(tasks))
	}

	skillEnd := len(skills)
	hookAssetEnd := skillEnd + len(hookAssets)
	return concreteSourceResults{
		skills:       results[:skillEnd],
		hookAssets:   results[skillEnd:hookAssetEnd],
		instructions: results[hookAssetEnd:],
	}, nil
}
