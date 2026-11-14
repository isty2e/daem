// Package codexhostpath resolves Codex's host-selected physical output root.
package codexhostpath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	nativehostpath "github.com/isty2e/daem/internal/output/hostpath/native"
)

// HomeEnvironmentVariable selects a configured Codex home locator.
const HomeEnvironmentVariable = "CODEX_HOME"

// ResolveHomeRoot returns Codex's configured home locator, or the default
// user's .codex directory, without checking path existence or resolving symlinks.
func ResolveHomeRoot(workDir string) (string, error) {
	if workDir != "" {
		if err := validateWorkDir(workDir); err != nil {
			return "", err
		}
	}

	configuredRoot := os.Getenv(HomeEnvironmentVariable)
	if configuredRoot != "" {
		if workDir == "" && !filepath.IsAbs(configuredRoot) {
			var err error
			workDir, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("resolve Codex config working directory: %w", err)
			}
			if err := validateWorkDir(workDir); err != nil {
				return "", err
			}
		}
		return nativehostpath.CodexHomeDirectory(configuredRoot, workDir, "")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve Codex config home: %w", err)
	}
	return nativehostpath.CodexHomeDirectory("", workDir, home)
}

// DestinationOverride maps only Codex-home destinations to Codex's selected
// physical root; other root roles continue through their owning policies.
func DestinationOverride(workDir string, resolveRoot func(string) (string, error)) hostpath.DestinationOverrideResolver {
	return func(destination output.Destination) (string, bool, error) {
		if destination.RootRole() != output.RootCodexHome {
			return "", false, nil
		}
		root, err := ResolveHomeRoot(workDir)
		if err != nil {
			return "", true, err
		}
		for _, component := range strings.Split(filepath.ToSlash(root), "/") {
			if component != ".." {
				continue
			}
			if resolveRoot == nil {
				return "", true, fmt.Errorf("Codex home with parent components requires physical root resolution")
			}
			root, err = resolveRoot(root)
			if err != nil {
				return "", true, fmt.Errorf("resolve Codex home before expanding the destination: %w", err)
			}
			break
		}
		return filepath.Join(root, filepath.FromSlash(destination.RelativePath())), true, nil
	}
}

func validateWorkDir(workDir string) error {
	if strings.ContainsRune(workDir, '\x00') {
		return fmt.Errorf("Codex config working directory must not contain a NUL byte")
	}
	if !filepath.IsAbs(workDir) {
		return fmt.Errorf("Codex config working directory must be an absolute path")
	}
	return nil
}
