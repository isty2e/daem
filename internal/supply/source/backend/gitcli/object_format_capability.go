package gitcli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (resolver Resolver) explicitObjectFormatSupported(ctx context.Context) (bool, error) {
	state, err := resolver.requireState()
	if err != nil {
		return false, err
	}
	state.objectFormatMu.Lock()
	defer state.objectFormatMu.Unlock()
	if state.objectFormatProbed {
		return state.explicitObjectFormat, nil
	}
	supported, err := probeExplicitObjectFormatSupport(ctx)
	if err != nil {
		return false, err
	}
	state.explicitObjectFormat = supported
	state.objectFormatProbed = true
	return supported, nil
}

func probeExplicitObjectFormatSupport(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	executable, err := exec.LookPath(gitExecutable)
	if err != nil {
		return false, err
	}
	probeRoot, err := os.MkdirTemp("", "daem-git-object-format-")
	if err != nil {
		return false, fmt.Errorf("create git object-format capability probe: %w", err)
	}
	defer os.RemoveAll(probeRoot)
	repoPath := filepath.Join(probeRoot, "repo")
	command := exec.CommandContext(
		ctx,
		executable,
		detachedGitCommandArgs([]string{"init", "--bare", "--quiet", "--object-format=sha1", "--", repoPath})...,
	)
	command.Env = repositoryGitCommandEnvironment(os.Environ())
	command.Dir = probeRoot
	output, err := command.CombinedOutput()
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if gitOutputIndicatesUnknownOption(string(output)) {
		return false, nil
	}
	diagnostic := strings.TrimSpace(string(output))
	if diagnostic == "" {
		return false, fmt.Errorf("inspect git object-format capability: %w", err)
	}
	return false, fmt.Errorf("inspect git object-format capability: %s", diagnostic)
}

func gitErrorIndicatesUnknownOption(err error) bool {
	return err != nil && gitOutputIndicatesUnknownOption(err.Error())
}

func gitOutputIndicatesUnknownOption(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "unknown option") ||
		strings.Contains(lower, "unrecognized option") ||
		strings.Contains(lower, "invalid option") ||
		strings.Contains(lower, "unknown switch")
}
