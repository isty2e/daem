package listworkflow

import (
	"context"
	"fmt"
	"os"

	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/journal"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	daempaths "github.com/isty2e/daem/internal/paths"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

// OutputResult is the selected manifest plus its immutable live output
// inventory.
type OutputResult struct {
	ManifestPath string
	Inventory    OutputInventory
}

// RunOutputs builds the output inventory without invoking full status
// readiness, source freshness, provider, relation, order, or delegate probes.
func RunOutputs(ctx context.Context, input Input) (OutputResult, error) {
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return OutputResult{}, err
	}
	result := OutputResult{ManifestPath: paths.ManifestPath}
	if err := journal.RequireNoInterruptedApply(ctx, paths.RecoveryDir); err != nil {
		return result, err
	}
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		return result, err
	}

	environment, err := declarationmanifest.LoadSelected(paths)
	if err != nil {
		return result, fmt.Errorf("invalid manifest: %w", err)
	}
	locked, missing, err := loadOutputLockfile(paths.LockfilePath)
	if err != nil {
		return result, fmt.Errorf("read lockfile: %w", err)
	}
	if !missing {
		generatedSkills, err := locked.Locked.SkillSetChildren(
			environment.Skills(),
			environment.SkillSets(),
		)
		if err != nil {
			return result, fmt.Errorf("expand skill groups from lockfile: %w", err)
		}
		environment, err = environment.WithGeneratedSkills(generatedSkills)
		if err != nil {
			return result, fmt.Errorf("build runtime desired environment: %w", err)
		}
	}

	resolver := managedhostpath.Resolver(paths)
	assessment, err := readiness.AssessOutputInventory(ctx, readiness.OutputInventoryInput{
		Paths:                   paths,
		Resolver:                liveobserve.DestinationResolver(resolver.Resolve),
		Environment:             environment,
		Lockfile:                locked,
		TargetValues:            input.TargetValues,
		Codecs:                  aggregatecodec.Catalog(),
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
	})
	if err != nil {
		return result, err
	}
	result.Inventory, err = buildOutputInventory(
		assessment.CurrentState,
		assessment.ManagedPaths,
		assessment.Aggregates,
		assessment.Selection,
	)
	if err != nil {
		return result, fmt.Errorf("build output inventory: %w", err)
	}
	return result, nil
}

func loadOutputLockfile(path string) (lock.File, bool, error) {
	file, err := lockfile.Load(path)
	if err == nil {
		return file, false, nil
	}
	if os.IsNotExist(err) {
		return lock.File{}, true, nil
	}
	return lock.File{}, false, err
}
