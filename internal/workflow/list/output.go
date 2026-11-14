package listworkflow

import (
	"context"
	"errors"
	"fmt"
	"os"

	physicalhostpath "github.com/isty2e/daem/internal/assurance/observe/hostpath"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	daempaths "github.com/isty2e/daem/internal/paths"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
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
func RunOutputs(ctx context.Context, input Input) (result OutputResult, resultErr error) {
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return OutputResult{}, err
	}
	result = OutputResult{ManifestPath: paths.ManifestPath}
	if err := recoverygate.RequireClear(ctx, paths); err != nil {
		return result, err
	}

	environment, err := declarationmanifest.LoadSelected(ctx, paths)
	if err != nil {
		return result, fmt.Errorf("invalid manifest: %w", err)
	}
	locked, missing, err := loadOutputLockfile(ctx, paths.LockfilePath)
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

	epoch, err := nativeconfig.LoadPersistenceEpoch(ctx, paths)
	if err != nil {
		return result, err
	}
	before, err := epoch.Snapshot()
	if err != nil {
		return result, err
	}
	selection, err := readiness.SelectOutputInventoryTargets(environment, locked, before, input.TargetValues)
	if err != nil {
		return result, err
	}
	documents, err := nativeconfig.ObserveDocuments(ctx, nativeconfig.DocumentObservationInput{
		Paths: paths, Environment: environment, Lockfile: locked, Selection: selection, PersistenceEpoch: &epoch,
	})
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, documents.Close()) }()
	var native *readiness.NativeAssessmentInput
	if observed, present := documents.NativeInput(); present {
		native = &observed
	}
	budget, err := epoch.PhysicalBudget()
	if err != nil {
		return result, err
	}
	resolver := physicalhostpath.NewResolver(paths).ForBudget(budget)
	codecs, err := nativeconfig.Codecs(ctx)
	if err != nil {
		return result, err
	}
	assessment, err := readiness.AssessOutputInventory(ctx, readiness.OutputInventoryInput{
		Paths:                   paths,
		Resolver:                liveobserve.DestinationResolver(resolver.Resolve),
		Environment:             environment,
		Lockfile:                locked,
		TargetValues:            input.TargetValues,
		Codecs:                  codecs,
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
		PersistenceEpoch:        &epoch,
		Native:                  native,
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

func loadOutputLockfile(ctx context.Context, path string) (lock.File, bool, error) {
	file, err := lockfile.Load(ctx, path)
	if err == nil {
		return file, false, nil
	}
	if os.IsNotExist(err) {
		return lock.File{}, true, nil
	}
	return lock.File{}, false, err
}
