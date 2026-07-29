package lock

import (
	"context"
	"errors"
	"fmt"
	"os"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	lockgenerate "github.com/isty2e/daem/internal/workflow/lock/generate"
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
	ManifestPath  string
	LockfilePath  string
	PreviousFound bool
	Lockfile      lock.File
	Delta         lock.Delta
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

// RunLock resolves sources, computes a lock delta, and writes the lockfile for non-dry-run invocations.
func RunLock(ctx context.Context, input LockInput) (Result, error) {
	if input.DryRun {
		result, err := buildCommandResult(
			ctx,
			input.ManifestPath,
			input.LockfilePath,
			false,
			true,
			commandMaxParallelSourceOps(input.MaxParallelSourceOps),
			input.SourceEvents,
			input.LockEvents,
		)
		if err != nil {
			return Result{}, err
		}
		return result.Result, nil
	}
	return runLockMutation(ctx, input)
}

// RunOutdated resolves sources through a temporary cache and reports the delta without writing the lockfile.
func RunOutdated(ctx context.Context, input OutdatedInput) (Result, error) {
	result, err := buildCommandResult(
		ctx,
		input.ManifestPath,
		input.LockfilePath,
		false,
		false,
		commandMaxParallelSourceOps(input.MaxParallelSourceOps),
		input.SourceEvents,
		input.LockEvents,
	)
	if err != nil {
		return Result{}, err
	}

	return result.Result, nil
}

func buildCommandResult(
	ctx context.Context,
	manifestPath string,
	lockfilePath string,
	usePersistentCache bool,
	allowPriorSchemaReplacement bool,
	maxParallelSourceOps int,
	sourceEvents acquisition.EventSink,
	lockEvents ProgressEventSink,
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
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		errorContext.Err = err
		return commandResult{}, errorContext
	}

	environment, err := declarationmanifest.LoadSelected(paths)
	if err != nil {
		errorContext.Err = fmt.Errorf("invalid manifest: %w", err)
		return commandResult{}, errorContext
	}

	previousLockfile, previousMissing, err := loadOptionalLockfile(
		outputPath,
		allowPriorSchemaReplacement,
	)
	if err != nil {
		errorContext.Err = fmt.Errorf("read lockfile: %w", err)
		return commandResult{}, errorContext
	}

	snapshot, err := lockgenerate.Build(ctx, lockgenerate.Input{
		Paths:                  paths,
		Environment:            environment,
		UsePersistentCache:     usePersistentCache,
		MaxParallelSourceOps:   maxParallelSourceOps,
		SourceEvents:           sourceEvents,
		Events:                 lockBuildProgressSink(lockEvents),
		HookEncoder:            hookcodec.CanonicalHookContribution,
		MCPEncoder:             mcpcodec.CanonicalMCPBindingContribution,
		ExtensionOrderIdentity: aggregatecodec.ExtensionOrderIdentityResolver(paths),
	})
	if err != nil {
		errorContext.Err = err
		return commandResult{}, errorContext
	}
	generatedSnapshot := snapshot.Snapshot()

	return commandResult{
		Result: Result{
			ManifestPath:  paths.ManifestPath,
			LockfilePath:  outputPath,
			PreviousFound: !previousMissing,
			Lockfile:      generatedSnapshot,
			Delta:         lock.BuildDelta(previousLockfile, generatedSnapshot),
		},
		Content: snapshot.Content(),
	}, nil
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

func loadOptionalLockfile(
	path string,
	allowPriorSchemaReplacement bool,
) (lock.File, bool, error) {
	file, err := lockfile.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return lock.File{Version: lock.CurrentVersion}, true, nil
		}
		var versionErr lockfile.UnsupportedVersionError
		if allowPriorSchemaReplacement &&
			errors.As(err, &versionErr) &&
			versionErr.RelockSupported() {
			return lock.File{Version: lock.CurrentVersion}, false, nil
		}
		return lock.File{}, false, err
	}

	return file, false, nil
}

func commitLockfile(ctx context.Context, path string, content []byte, fileMode os.FileMode) error {
	commitPath, err := mutation.CanonicalDirectoryEntryPath(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		request, requestErr := storagecommit.NewFileCreate(commitPath, content, fileMode)
		if requestErr != nil {
			return requestErr
		}
		return storagecommit.CommitFile(ctx, request)
	}
	if err != nil {
		return err
	}
	expected, err := storagecommit.CaptureEntryIdentity(ctx, commitPath)
	if err != nil {
		return err
	}
	request, err := storagecommit.NewFileReplacement(commitPath, content, info.Mode().Perm(), expected)
	if err != nil {
		return err
	}
	return storagecommit.CommitFile(ctx, request)
}
