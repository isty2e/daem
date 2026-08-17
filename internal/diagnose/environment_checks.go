package diagnose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/findings"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func EnvironmentChecks(
	ctx context.Context,
	paths daempaths.Paths,
	projectPlacementAllowed bool,
	selection targetselection.Selection,
	resourceKinds map[entity.Kind]struct{},
	includePassivePluginDiagnostics bool,
) []findings.Check {
	checks := []findings.Check{
		gitCheck(ctx),
		cacheCheck(paths.CacheDir),
		symlinkCheck(),
	}

	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		checks = append(checks, errorCheck("home", fmt.Sprintf("resolve user home directory: %v", err)))
		return checks
	}

	for _, target := range selection.Targets() {
		checks = append(checks, targetChecks(homeDirectory, paths.ManifestRoot, projectPlacementAllowed, target, resourceKinds)...)
	}
	if includePassivePluginDiagnostics {
		checks = append(checks, CodexPluginChecks(homeDirectory, selection)...)
	}

	return checks
}

func gitCheck(ctx context.Context) findings.Check {
	return gitEnvironmentCheck(ctx, 5*time.Second)
}

func gitEnvironmentCheck(ctx context.Context, timeout time.Duration) findings.Check {
	if ctx == nil {
		ctx = context.Background()
	}
	checkContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Timeout:     timeout,
		OutputLimit: subprocess.DefaultCommandOutputLimit,
	})

	version := executor.Execute(checkContext, subprocess.CommandAttemptRequest{
		Command: "git",
		Args:    []string{"--version"},
	})
	check := classifyGitVersionAttempt(ctx, checkContext, timeout, version)
	if check.Severity != findings.SeverityOK {
		return check
	}

	help := executor.Execute(checkContext, subprocess.CommandAttemptRequest{
		Command: "git",
		Args:    []string{"init", "-h"},
	})
	switch {
	case help.TimedOut(), errors.Is(checkContext.Err(), context.DeadlineExceeded) && help.Failed():
		return errorCheck("git", fmt.Sprintf("git check timed out after %s", timeout))
	case help.Canceled() && ctx.Err() != nil:
		return errorCheck("git", fmt.Sprintf("git check stopped by caller context: %v", ctx.Err()))
	}
	if !gitInitHelpAdmitted(help) {
		detail := strings.TrimSpace(help.ErrorDetail())
		if detail == "" {
			detail = "git init -h failed"
		}
		return errorCheck("git", fmt.Sprintf("git init -h failed: %s", detail))
	}
	output := strings.TrimSpace(strings.TrimSpace(help.Stdout()) + "\n" + strings.TrimSpace(help.Stderr()))
	if output == "" {
		return errorCheck("git", "git init -h returned empty output")
	}
	check.Detail = check.Detail + "; " + gitObjectFormatCapabilityLabel(output)
	return check
}

const gitHelpUsageExitCode = 129

func gitInitHelpAdmitted(result subprocess.CommandAttemptResult) bool {
	if result.Succeeded() {
		return true
	}
	exitCode, ok := result.ExitCode()
	return ok && exitCode == gitHelpUsageExitCode
}

func classifyGitVersionAttempt(
	caller context.Context,
	checkContext context.Context,
	timeout time.Duration,
	result subprocess.CommandAttemptResult,
) findings.Check {
	switch {
	case result.Canceled() && caller.Err() != nil:
		return errorCheck("git", fmt.Sprintf("git check stopped by caller context: %v", caller.Err()))
	case result.TimedOut(), errors.Is(checkContext.Err(), context.DeadlineExceeded) && result.Failed():
		return errorCheck("git", fmt.Sprintf("git check timed out after %s", timeout))
	case result.Reason() == subprocess.CommandReasonMissingRunner:
		return errorCheck("git", "git executable was not found in PATH")
	case result.Failed():
		detail := strings.TrimSpace(result.ErrorDetail())
		if detail == "" {
			detail = "command failed"
		}
		return errorCheck("git", fmt.Sprintf("git --version failed: %s", detail))
	}

	value := strings.TrimSpace(result.Stdout())
	if value == "" {
		return errorCheck("git", "git --version returned empty output")
	}
	return okCheck("git", value)
}

func gitCheckWithTimeout(
	ctx context.Context,
	timeout time.Duration,
	version func(context.Context) (string, error),
) findings.Check {
	checkContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	value, err := version(checkContext)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return errorCheck("git", fmt.Sprintf("git check stopped by caller context: %v", ctx.Err()))
		case errors.Is(checkContext.Err(), context.DeadlineExceeded):
			return errorCheck("git", fmt.Sprintf("git version check timed out after %s", timeout))
		case errors.Is(err, exec.ErrNotFound):
			return errorCheck("git", "git executable was not found in PATH")
		default:
			return errorCheck("git", fmt.Sprintf("git --version failed: %v", err))
		}
	}

	if value == "" {
		return errorCheck("git", "git --version returned empty output")
	}
	return okCheck("git", value)
}

func gitObjectFormatCapabilityLabel(help string) string {
	if strings.Contains(help, "--object-format") {
		return "object-format sha1,sha256"
	}
	return "object-format sha1"
}

func cacheCheck(cachePath string) findings.Check {
	return directoryCheck("cache", cachePath)
}

func symlinkCheck() findings.Check {
	tempDirectory, err := os.MkdirTemp("", "daem-doctor-symlink-*")
	if err != nil {
		return warnCheck("symlink", fmt.Sprintf("create temporary probe directory: %v", err))
	}
	defer os.RemoveAll(tempDirectory)

	targetPath := filepath.Join(tempDirectory, "target")
	if err := os.WriteFile(targetPath, []byte("doctor\n"), 0o600); err != nil {
		return warnCheck("symlink", fmt.Sprintf("write temporary probe target: %v", err))
	}

	linkPath := filepath.Join(tempDirectory, "link")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		return warnCheck("symlink", fmt.Sprintf("symlink placement unavailable: %v", err))
	}

	return okCheck("symlink", "symlink placement is available")
}
