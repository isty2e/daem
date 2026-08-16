package listworkflow

import (
	"context"
	"fmt"
	"maps"

	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/desired"
	daempaths "github.com/isty2e/daem/internal/paths"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

type Input struct {
	ManifestPath string
	TargetValues []string
}

type Result struct {
	ManifestPath string
	Environment  desired.Environment
	Selection    targetselection.Selection
	skillGroups  map[string]string
}

// PathResult is the selected manifest path plus its immutable location inventory.
type PathResult struct {
	ManifestPath string
	Inventory    LocationInventory
}

// SkillGroups returns a defensive copy of syntax-only expanded skill
// membership labels used by the list presentation.
func (result Result) SkillGroups() map[string]string {
	groups := make(map[string]string, len(result.skillGroups))
	maps.Copy(groups, result.skillGroups)
	return groups
}

func Run(ctx context.Context, input Input) (Result, error) {
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return Result{}, err
	}
	result := Result{ManifestPath: paths.ManifestPath}
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		return result, err
	}

	content, err := declarationartifact.Read(ctx, paths.ManifestPath)
	if err != nil {
		return result, fmt.Errorf("read manifest: %w", err)
	}
	environment, err := declarationmanifest.Decode(content)
	if err != nil {
		return Result{}, fmt.Errorf("invalid manifest: %w", err)
	}

	groupMembership, err := declarationcodec.SkillGroupMembership(content)
	if err != nil {
		return Result{}, err
	}

	selection, err := targetselection.ForAvailableTargets(availableTargets(environment), input.TargetValues)
	if err != nil {
		return result, fmt.Errorf("%w: %w", targetselection.ErrInvalid, err)
	}

	result.Environment = environment
	result.Selection = selection
	result.skillGroups = groupMembership
	return result, nil
}

// RunPaths loads the same selected manifest as Run, then projects the complete
// static location catalog for its selected targets.
func RunPaths(ctx context.Context, input Input) (PathResult, error) {
	result, err := Run(ctx, input)
	if err != nil {
		return PathResult{ManifestPath: result.ManifestPath}, err
	}
	inventory, err := BuildLocationInventory(result.Environment, result.Selection)
	if err != nil {
		return PathResult{ManifestPath: result.ManifestPath}, fmt.Errorf("build location inventory: %w", err)
	}
	return PathResult{ManifestPath: result.ManifestPath, Inventory: inventory}, nil
}
