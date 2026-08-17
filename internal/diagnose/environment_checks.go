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
	check := gitCheckWithTimeout(ctx, 5*time.Second, defaultGitVersion)
	if check.Severity != findings.SeverityOK {
		return check
	}
	label, err := inspectGitObjectFormatCapability(ctx)
	if err != nil {
		check.Detail = check.Detail + "; object-format capability could not be inspected"
		return check
	}
	check.Detail = check.Detail + "; " + label
	return check
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

func defaultGitVersion(ctx context.Context) (string, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("locate git executable: %w", err)
	}

	command := exec.CommandContext(ctx, gitPath, "--version")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("run git --version: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func inspectGitObjectFormatCapability(ctx context.Context) (string, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, gitPath, "init", "-h")
	output, err := command.CombinedOutput()
	if len(output) == 0 && err != nil {
		return "", err
	}
	return gitObjectFormatCapabilityLabel(string(output)), nil
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
