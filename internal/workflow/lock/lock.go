package lock

import (
	"context"
	"fmt"
	"path/filepath"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired"
	daempaths "github.com/isty2e/daem/internal/paths"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	lockgenerate "github.com/isty2e/daem/internal/workflow/lock/generate"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

const defaultCommandMaxParallelSourceOps = 4

// LockInput describes one daem lock workflow invocation.
type LockInput struct {
	ManifestPath         string
	LockfilePath         string
	DryRun               bool
	MaxParallelSourceOps int
	SourceEvents         acquisition.EventSink
	LockEvents           ProgressEventSink
	// PrepareResult prepares the built response before publication; it must not mutate workflow inputs or outputs.
	PrepareResult func(Result) error
}

// OutdatedInput describes one daem outdated workflow invocation.
type OutdatedInput struct {
	ManifestPath         string
	LockfilePath         string
	MaxParallelSourceOps int
	SourceEvents         acquisition.EventSink
	LockEvents           ProgressEventSink
}

// Result carries lock workflow facts for CLI and presentation layers.
type Result struct {
	ManifestPath string
	LockfilePath string
	Lockfile     lock.File

	previous PreviousLockStatus
	delta    lock.Delta
}

func (result Result) PreviousStatus() PreviousLockStatus { return result.previous }

// Comparison returns a delta when the previous lock was readable or absent.
func (result Result) Comparison() *lock.Delta {
	if !result.previous.comparisonAvailable() {
		return nil
	}
	delta := result.delta
	return &delta
}

// CommandError wraps workflow failures with resolved command path facts for CLI diagnostics.
type CommandError struct {
	ManifestPath     string
	LockfilePath     string
	ExplicitLockfile bool
	Err              error
}

func (err CommandError) Error() string {
	if err.Err == nil {
		return ""
	}
	return err.Err.Error()
}

func (err CommandError) Unwrap() error {
	return err.Err
}

type commandResult struct {
	Result  Result
	Content []byte
}

// RunLock resolves sources and reports the prospective lock. Without dry-run it also publishes the lockfile.
func RunLock(ctx context.Context, input LockInput) (Result, error) {
	if input.DryRun {
		result, err := previewLock(
			ctx,
			input.ManifestPath,
			input.LockfilePath,
			true,
			lockgenerate.Input{
				MaxParallelSourceOps: input.MaxParallelSourceOps,
				SourceEvents:         input.SourceEvents,
				Events:               lockBuildProgressSink(input.LockEvents),
			},
		)
		if err != nil {
			return Result{}, err
		}
		if input.PrepareResult != nil {
			if err := input.PrepareResult(result.Result); err != nil {
				return Result{}, err
			}
		}
		return result.Result, nil
	}
	return runLockMutation(ctx, input)
}

// RunOutdated resolves sources through a temporary cache and reports the delta without writing the lockfile.
func RunOutdated(ctx context.Context, input OutdatedInput) (Result, error) {
	result, err := previewLock(
		ctx,
		input.ManifestPath,
		input.LockfilePath,
		false,
		lockgenerate.Input{
			MaxParallelSourceOps: input.MaxParallelSourceOps,
			SourceEvents:         input.SourceEvents,
			Events:               lockBuildProgressSink(input.LockEvents),
		},
	)
	if err != nil {
		return Result{}, err
	}

	return result.Result, nil
}

func previewLock(
	ctx context.Context,
	manifestPath string,
	lockfilePath string,
	standalone bool,
	input lockgenerate.Input,
) (commandResult, error) {
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		return commandResult{}, err
	}
	outputPath := outputLockfilePath(lockfilePath, paths)
	errorContext := CommandError{
		ManifestPath:     paths.ManifestPath,
		LockfilePath:     outputPath,
		ExplicitLockfile: lockfilePath != "",
	}
	if err := recoverygate.RequireFileSetClear(ctx, paths); err != nil {
		errorContext.Err = err
		return commandResult{}, errorContext
	}

	environment, err := loadLockEnvironment(ctx, paths)
	if err != nil {
		errorContext.Err = err
		return commandResult{}, errorContext
	}

	readPath := outputPath
	if standalone {
		readPath, err = filepath.Abs(outputPath)
		if err != nil {
			errorContext.Err = fmt.Errorf("resolve lockfile: %w", err)
			return commandResult{}, errorContext
		}
	}
	previous, err := readPreviousLock(ctx, readPath, standalone)
	if err != nil {
		errorContext.Err = fmt.Errorf("read lockfile: %w", err)
		return commandResult{}, errorContext
	}

	input.Paths = paths
	input.Environment = environment
	if previous.status == PreviousDamaged {
		if err := checkPreviewRebuildInputs(ctx, readPath, input); err != nil {
			errorContext.Err = err
			return commandResult{}, errorContext
		}
	}
	return buildCommandResult(ctx, errorContext, input, previous)
}

func buildCommandResult(ctx context.Context, errorContext CommandError, input lockgenerate.Input, previous previousLock) (commandResult, error) {
	input.MaxParallelSourceOps = commandMaxParallelSourceOps(input.MaxParallelSourceOps)
	input.HookEncoder = hookcodec.CanonicalHookContribution
	input.MCPEncoder = mcpcodec.CanonicalMCPBindingContribution
	input.ExtensionOrderIdentity = aggregatecodec.ExtensionOrderIdentityResolver(input.Paths)
	input.NativeDocumentLimits = nativeconfig.DocumentLimits()
	snapshot, err := lockgenerate.Build(ctx, input)
	if err != nil {
		errorContext.Err = err
		return commandResult{}, errorContext
	}
	generatedSnapshot := snapshot.Snapshot()

	result := Result{
		ManifestPath: errorContext.ManifestPath,
		LockfilePath: errorContext.LockfilePath,
		Lockfile:     generatedSnapshot,
		previous:     previous.status,
	}
	if previous.status.comparisonAvailable() {
		result.delta = lock.BuildDelta(previous.snapshot, generatedSnapshot)
	}
	return commandResult{Result: result, Content: snapshot.Content()}, nil
}

func loadLockEnvironment(ctx context.Context, paths daempaths.Paths) (desired.Environment, error) {
	environment, err := declarationmanifest.LoadSelected(ctx, paths)
	if err != nil {
		return desired.Environment{}, fmt.Errorf("invalid manifest: %w", err)
	}
	return environment, nil
}

func commandMaxParallelSourceOps(value int) int {
	if value <= 0 {
		return defaultCommandMaxParallelSourceOps
	}

	return value
}

func outputLockfilePath(inputPath string, paths daempaths.Paths) string {
	if inputPath != "" {
		return inputPath
	}

	return paths.LockfilePath
}
