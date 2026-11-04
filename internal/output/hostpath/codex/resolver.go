// Package codexhostpath resolves Codex's host-selected physical output root.
package codexhostpath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
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
		if strings.ContainsRune(configuredRoot, '\x00') {
			return "", fmt.Errorf("Codex home root must not contain a NUL byte")
		}
		if filepath.IsAbs(configuredRoot) {
			return configuredRoot, nil
		}
		if workDir == "" {
			var err error
			workDir, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("resolve Codex config working directory: %w", err)
			}
			if err := validateWorkDir(workDir); err != nil {
				return "", err
			}
		}
		return filepath.Join(workDir, configuredRoot), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve Codex config home: %w", err)
	}
	if strings.TrimSpace(home) == "" || strings.TrimSpace(home) != home ||
		strings.ContainsRune(home, '\x00') || !filepath.IsAbs(home) {
		return "", fmt.Errorf("Codex default home must be a trimmed absolute path")
	}
	return filepath.Join(home, ".codex"), nil
}

// DestinationOverride maps only Codex-home destinations to Codex's selected
// physical root; other root roles continue through their owning policies.
func DestinationOverride(workDir string) hostpath.DestinationOverrideResolver {
	return func(destination output.Destination) (string, bool, error) {
		if destination.RootRole() != output.RootCodexHome {
			return "", false, nil
		}
		root, err := ResolveHomeRoot(workDir)
		if err != nil {
			return "", true, err
		}
		return filepath.Join(root, filepath.FromSlash(destination.RelativePath())), true, nil
	}
}

func validateWorkDir(workDir string) error {
	if strings.ContainsRune(workDir, '\x00') {
		return fmt.Errorf("Codex config working directory must not contain a NUL byte")
	}
	if strings.TrimSpace(workDir) == "" || strings.TrimSpace(workDir) != workDir || !filepath.IsAbs(workDir) {
		return fmt.Errorf("Codex config working directory must be a trimmed absolute path")
	}
	return nil
}
