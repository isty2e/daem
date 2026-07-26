package gitcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

func (resolver Resolver) gitBytes(ctx context.Context, repoPath string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, gitExecutable, args...)
	if repoPath != "" {
		command.Dir = repoPath
	}

	return runGitOutput(ctx, command)
}

func (resolver Resolver) gitOutput(ctx context.Context, repoPath string, args ...string) (string, error) {
	output, err := resolver.gitBytes(ctx, repoPath, args...)
	if err != nil {
		return "", err
	}

	return string(output), nil
}

func (resolver Resolver) runGit(ctx context.Context, repoPath string, args ...string) error {
	_, err := resolver.gitBytes(ctx, repoPath, args...)
	return err
}

func runGitOutput(ctx context.Context, command *exec.Cmd) ([]byte, error) {
	process, err := startGitProcess(command)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}

	output, readErr := io.ReadAll(process.Stdout())
	if readErr != nil {
		_, _ = process.Terminate()
	}
	result := process.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		if result.terminationErr != nil {
			return nil, errors.Join(ctxErr, result.terminationErr)
		}
		return nil, ctxErr
	}

	runErr := readErr
	if result.waitErr != nil {
		runErr = errors.Join(
			runErr,
			gitCommandErrorWithCapture(result.waitErr, result.stderr, result.stderrTruncated),
		)
	}
	if result.stderrReadErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("read git stderr: %w", result.stderrReadErr))
	}
	if result.terminationErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("terminate git process tree: %w", result.terminationErr))
	}
	if result.termination.ProcessesFound() {
		runErr = errors.Join(runErr, errors.New("git exited while descendant processes remained; terminated residual process tree"))
	}
	if runErr != nil {
		return nil, runErr
	}
	return output, nil
}
