package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/supply/source"
)

type repositoryHandle struct {
	resolver   Resolver
	repository cachedRepository
	root       *rootedpath.CapturedRoot
}

const repositoryOriginFetchRefspec = "+refs/heads/*:refs/remotes/origin/*"

func (resolver Resolver) openRepository(
	ctx context.Context,
	repository cachedRepository,
) (*repositoryHandle, error) {
	root, err := rootedpath.CaptureRootNoFollow(repository.path)
	if err != nil {
		return nil, fmt.Errorf("capture git repository cache authority: %w", err)
	}
	if err := verifyRepositoryRootRecord(ctx, root, repository); err != nil {
		return nil, errors.Join(err, root.Close())
	}
	if err := validateRepositoryRootMode(root); err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return &repositoryHandle{
		resolver:   resolver,
		repository: repository,
		root:       root,
	}, nil
}

func (resolver Resolver) openVerifiedRepository(
	ctx context.Context,
	repository cachedRepository,
) (*repositoryHandle, error) {
	handle, err := resolver.openRepository(ctx, repository)
	if err != nil {
		return nil, err
	}
	if err := handle.verifyBare(ctx); err != nil {
		return nil, errors.Join(err, handle.Close())
	}
	if err := handle.verifyOrigin(ctx); err != nil {
		return nil, errors.Join(err, handle.Close())
	}
	if err := handle.verifyLocalConfiguration(ctx); err != nil {
		return nil, errors.Join(err, handle.Close())
	}
	return handle, nil
}

func (handle *repositoryHandle) verifyBare(ctx context.Context) error {
	output, err := handle.gitOutput(ctx, inspectBareRepositoryArgs()...)
	if err != nil {
		return fmt.Errorf("inspect git repository cache form: %w", err)
	}
	if output != "true\n" {
		return fmt.Errorf("git repository cache is not a daem bare repository")
	}
	return nil
}

func (handle *repositoryHandle) verifyOrigin(ctx context.Context) error {
	output, err := handle.gitOutput(ctx, inspectOriginArgs()...)
	if err != nil {
		return fmt.Errorf("read git repository cache origin: %w", err)
	}
	originValue, ok := trimSingleGitConfigValue(output)
	if !ok {
		return fmt.Errorf("git repository cache origin must contain exactly one canonical locator")
	}
	observed, err := source.ParseGitLocator(originValue)
	if err != nil || !handle.repository.locator.Equivalent(observed) {
		return fmt.Errorf("git repository cache origin does not match the declared locator")
	}
	return nil
}

func (handle *repositoryHandle) verifyLocalConfiguration(ctx context.Context) error {
	namesOutput, err := handle.gitOutput(ctx, inspectLocalConfigNamesArgs()...)
	if err != nil {
		return fmt.Errorf("inspect git repository cache configuration: %w", err)
	}
	counts := make(map[string]int)
	for _, name := range strings.Fields(namesOutput) {
		if !isAdmittedRepositoryConfigName(name) {
			return fmt.Errorf("git repository cache contains an unsupported local configuration key")
		}
		counts[name]++
		if counts[name] > 1 {
			return fmt.Errorf("git repository cache contains a duplicate local configuration key")
		}
	}
	for _, required := range []string{"core.bare", "remote.origin.url", "remote.origin.fetch"} {
		if counts[required] != 1 {
			return fmt.Errorf("git repository cache is missing required local configuration")
		}
	}

	fetchOutput, err := handle.gitOutput(ctx, inspectOriginFetchArgs()...)
	if err != nil {
		return fmt.Errorf("inspect git repository cache fetch mapping: %w", err)
	}
	fetchRefspec, ok := trimSingleGitConfigValue(fetchOutput)
	if !ok || fetchRefspec != repositoryOriginFetchRefspec {
		return fmt.Errorf("git repository cache fetch mapping does not match the daem contract")
	}

	effectiveOutput, err := handle.gitOutput(ctx, inspectEffectiveOriginArgs()...)
	if err != nil {
		return fmt.Errorf("inspect effective git repository cache origin: %w", err)
	}
	effectiveValue, ok := trimSingleGitConfigValue(effectiveOutput)
	if !ok {
		return fmt.Errorf("effective git repository cache origin must contain exactly one canonical locator")
	}
	effective, err := source.ParseGitLocator(effectiveValue)
	if err != nil || !handle.repository.locator.Equivalent(effective) {
		return fmt.Errorf("effective git repository cache origin does not match the declared locator")
	}

	explicit, err := handle.resolver.explicitObjectFormatSupported(ctx)
	if err != nil {
		return err
	}
	if !explicit {
		if handle.repository.format != gitObjectFormatSHA1 {
			return fmt.Errorf("git source object format sha256 requires a git binary that supports --object-format")
		}
		if counts["extensions.objectformat"] != 1 {
			return nil
		}
		formatOutput, err := handle.gitOutput(ctx, inspectObjectFormatConfigArgs()...)
		if err != nil {
			return fmt.Errorf("inspect git repository cache object format: %w", err)
		}
		format, err := parseGitObjectFormat(formatOutput)
		if err != nil {
			return err
		}
		if format != gitObjectFormatSHA1 {
			return fmt.Errorf("git repository cache object format does not match the observed source")
		}
		return nil
	}
	formatOutput, err := handle.gitOutput(ctx, inspectObjectFormatArgs()...)
	if err != nil {
		return fmt.Errorf("inspect git repository cache object format: %w", err)
	}
	format, err := parseGitObjectFormat(formatOutput)
	if err != nil {
		return err
	}
	if format != handle.repository.format {
		return fmt.Errorf("git repository cache object format does not match the observed source")
	}
	return nil
}

func isAdmittedRepositoryConfigName(name string) bool {
	switch name {
	case "core.repositoryformatversion",
		"core.filemode",
		"core.bare",
		"core.ignorecase",
		"core.precomposeunicode",
		"extensions.objectformat",
		"remote.origin.url",
		"remote.origin.fetch":
		return true
	default:
		return false
	}
}

func (handle *repositoryHandle) initialize(ctx context.Context) error {
	if err := handle.repository.format.validate(); err != nil {
		return err
	}
	explicit, err := handle.resolver.explicitObjectFormatSupported(ctx)
	if err != nil {
		return err
	}
	if handle.repository.format == gitObjectFormatSHA256 && !explicit {
		return fmt.Errorf("git source object format sha256 requires a git binary that supports --object-format")
	}
	if err := handle.runGit(ctx, initializeBareRepositoryArgs(handle.repository.format, explicit)...); err != nil {
		return fmt.Errorf("initialize bare git repository cache: %w", err)
	}
	if err := handle.runGit(ctx, addOriginArgs(handle.repository.locator.String())...); err != nil {
		return fmt.Errorf("declare git repository cache origin: %w", err)
	}
	if err := handle.verifyBare(ctx); err != nil {
		return err
	}
	if err := handle.verifyOrigin(ctx); err != nil {
		return err
	}
	return handle.verifyLocalConfiguration(ctx)
}

func (handle *repositoryHandle) gitBytes(ctx context.Context, args ...string) ([]byte, error) {
	command, finish, err := handle.prepareCommand(ctx, args)
	if err != nil {
		return nil, err
	}
	output, runErr := runGitOutput(ctx, command)
	return output, errors.Join(runErr, finish())
}

func (handle *repositoryHandle) gitOutput(ctx context.Context, args ...string) (string, error) {
	output, err := handle.gitBytes(ctx, args...)
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func (handle *repositoryHandle) consumeGitOutput(
	ctx context.Context,
	consume func(io.Reader) error,
	args ...string,
) error {
	command, finish, err := handle.prepareCommand(ctx, args)
	if err != nil {
		return err
	}
	return errors.Join(runGitReader(ctx, command, consume), finish())
}

func (handle *repositoryHandle) runGit(ctx context.Context, args ...string) error {
	_, err := handle.gitBytes(ctx, args...)
	return err
}

func (handle *repositoryHandle) prepareCommand(
	ctx context.Context,
	args []string,
) (*exec.Cmd, func() error, error) {
	if handle == nil || handle.root == nil {
		return nil, nil, fmt.Errorf("git repository cache handle is required")
	}
	if handle.resolver.state != nil && handle.resolver.state.testBeforeRepositoryCommand != nil {
		handle.resolver.state.testBeforeRepositoryCommand()
	}
	return prepareCapturedRepositoryCommand(ctx, handle.root, handle.repository.path, args)
}

func prepareCapturedRepositoryCommand(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	selected string,
	args []string,
) (*exec.Cmd, func() error, error) {
	if root == nil {
		return nil, nil, fmt.Errorf("git repository cache handle is required")
	}
	capability, err := root.AcquireSelectedWorkingDirectory(selected)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire git repository cache working directory: %w", err)
	}
	directory, err := capability.OpenDirectory()
	if err != nil {
		_ = capability.Close()
		return nil, nil, fmt.Errorf("open git repository cache working directory: %w", err)
	}
	cleanup := func() error {
		validationErr := capability.Validate()
		return errors.Join(validationErr, directory.Close(), capability.Close())
	}
	return finishPreparedWorkingDirectoryCommand(
		ctx,
		capability,
		directory,
		cleanup,
		repositoryGitCommandArgs(args),
	)
}

func finishPreparedWorkingDirectoryCommand(
	ctx context.Context,
	capability rootedpath.WorkingDirectoryCapability,
	directory *os.File,
	cleanup func() error,
	commandArgs []string,
) (*exec.Cmd, func() error, error) {
	if err := capability.Validate(); err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("git repository cache changed before command launch: %w", err),
			directory.Close(),
			capability.Close(),
		)
	}
	executable, err := exec.LookPath(gitExecutable)
	if err != nil {
		return nil, nil, errors.Join(err, directory.Close(), capability.Close())
	}
	command, err := subprocess.PrepareCommandInWorkingDirectory(
		ctx,
		executable,
		commandArgs,
		repositoryGitCommandEnvironment(os.Environ()),
		directory,
	)
	if err != nil {
		return nil, nil, errors.Join(err, directory.Close(), capability.Close())
	}
	return command, cleanup, nil
}

func gitOutputAtCapturedRepository(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	selected string,
	args ...string,
) (string, error) {
	command, finish, err := prepareCapturedRepositoryCommand(ctx, root, selected, args)
	if err != nil {
		return "", err
	}
	output, runErr := runGitOutput(ctx, command)
	return string(output), errors.Join(runErr, finish())
}

func runGitAtCapturedRepository(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	selected string,
	args ...string,
) error {
	_, err := gitOutputAtCapturedRepository(ctx, root, selected, args...)
	return err
}

func consumeGitOutputAtCapturedRepository(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	selected string,
	consume func(io.Reader) error,
	args ...string,
) error {
	command, finish, err := prepareCapturedRepositoryCommand(ctx, root, selected, args)
	if err != nil {
		return err
	}
	return errors.Join(runGitReader(ctx, command, consume), finish())
}

func repositoryGitCommandArgs(args []string) []string {
	commandArgs := make([]string, 0, len(args)+4)
	commandArgs = append(
		commandArgs,
		"--no-replace-objects",
		"--git-dir=.",
		"-c",
		"core.hooksPath="+os.DevNull,
	)
	return append(commandArgs, args...)
}

func detachedGitCommandArgs(args []string) []string {
	commandArgs := make([]string, 0, len(args)+3)
	commandArgs = append(
		commandArgs,
		"--no-replace-objects",
		"-c",
		"core.hooksPath="+os.DevNull,
	)
	return append(commandArgs, args...)
}

func (resolver Resolver) gitOutputInCacheRoot(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	args ...string,
) (string, error) {
	command, finish, err := resolver.prepareCacheRootCommand(ctx, cacheRoot, args)
	if err != nil {
		return "", err
	}
	output, runErr := runGitOutput(ctx, command)
	return string(output), errors.Join(runErr, finish())
}

func (resolver Resolver) runGitInCacheRoot(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	args ...string,
) error {
	_, err := resolver.gitOutputInCacheRoot(ctx, cacheRoot, args...)
	return err
}

func (resolver Resolver) consumeGitOutputInCacheRoot(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	consume func(io.Reader) error,
	args ...string,
) error {
	command, finish, err := resolver.prepareCacheRootCommand(ctx, cacheRoot, args)
	if err != nil {
		return err
	}
	return errors.Join(runGitReader(ctx, command, consume), finish())
}

func (resolver Resolver) prepareCacheRootCommand(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	args []string,
) (*exec.Cmd, func() error, error) {
	if cacheRoot == nil {
		return nil, nil, fmt.Errorf("git source cache root authority is required")
	}
	capability, err := cacheRoot.AcquireWorkingDirectory()
	if err != nil {
		return nil, nil, fmt.Errorf("acquire git source cache working directory: %w", err)
	}
	directory, err := capability.OpenDirectory()
	if err != nil {
		_ = capability.Close()
		return nil, nil, fmt.Errorf("open git source cache working directory: %w", err)
	}
	cleanup := func() error {
		validationErr := capability.Validate()
		return errors.Join(validationErr, directory.Close(), capability.Close())
	}
	if err := capability.Validate(); err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("git source cache changed before command launch: %w", err),
			directory.Close(),
			capability.Close(),
		)
	}
	executable, err := exec.LookPath(gitExecutable)
	if err != nil {
		return nil, nil, errors.Join(err, directory.Close(), capability.Close())
	}
	command, err := subprocess.PrepareCommandInWorkingDirectory(
		ctx,
		executable,
		detachedGitCommandArgs(args),
		repositoryGitCommandEnvironment(os.Environ()),
		directory,
	)
	if err != nil {
		return nil, nil, errors.Join(err, directory.Close(), capability.Close())
	}
	return command, cleanup, nil
}

func repositoryGitCommandEnvironment(entries []string) []string {
	filtered := make([]string, 0, len(entries))
	for _, entry := range entries {
		name, _, ok := strings.Cut(entry, "=")
		if ok && isFilteredGitCommandEnvironment(name) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func isFilteredGitCommandEnvironment(name string) bool {
	return isRepositorySelectingGitEnvironment(name) || name == "GIT_DEFAULT_HASH"
}

func isRepositorySelectingGitEnvironment(name string) bool {
	switch name {
	case "GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR",
		"GIT_DIR",
		"GIT_IMPLICIT_WORK_TREE",
		"GIT_INDEX_FILE",
		"GIT_NAMESPACE",
		"GIT_NO_REPLACE_OBJECTS",
		"GIT_OBJECT_DIRECTORY",
		"GIT_QUARANTINE_PATH",
		"GIT_REPLACE_REF_BASE",
		"GIT_SHALLOW_FILE",
		"GIT_WORK_TREE":
		return true
	default:
		return false
	}
}

func (handle *repositoryHandle) Close() error {
	if handle == nil || handle.root == nil {
		return nil
	}
	err := handle.root.Close()
	handle.root = nil
	return err
}

func runGitOutput(ctx context.Context, command *exec.Cmd) ([]byte, error) {
	var output bytes.Buffer
	err := runGitReader(ctx, command, func(reader io.Reader) error {
		_, copyErr := io.Copy(&output, reader)
		return copyErr
	})
	if err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func runGitReader(ctx context.Context, command *exec.Cmd, consume func(io.Reader) error) error {
	if consume == nil {
		return fmt.Errorf("git stdout consumer is required")
	}
	process, err := startGitProcess(command)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}

	readErr, result := completeGitProcess(ctx, process, consume)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return joinGitProcessGroupTerminateErr(ctxErr, "git", result)
	}

	runErr := readErr
	if result.commandErr != nil {
		runErr = errors.Join(runErr, result.commandErr)
	}
	if result.stderrReadErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("read git stderr: %w", result.stderrReadErr))
	}
	runErr = joinGitProcessGroupResidual(runErr, "git", result)
	if runErr != nil {
		return runErr
	}
	return nil
}
