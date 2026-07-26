package codexplugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
)

// HostPaths is the resolved Codex-owned config and plugin-cache boundary.
type HostPaths struct {
	home string
}

// ResolveHostPaths mirrors Codex's CODEX_HOME resolution contract.
func ResolveHostPaths() (HostPaths, error) {
	if configured := os.Getenv("CODEX_HOME"); configured != "" {
		info, err := os.Stat(configured)
		if err != nil {
			return HostPaths{}, fmt.Errorf("resolve CODEX_HOME %q: %w", configured, err)
		}
		if !info.IsDir() {
			return HostPaths{}, fmt.Errorf("CODEX_HOME %q is not a directory", configured)
		}
		resolved, err := filepath.EvalSymlinks(configured)
		if err != nil {
			return HostPaths{}, fmt.Errorf("canonicalize CODEX_HOME %q: %w", configured, err)
		}
		absolute, err := filepath.Abs(resolved)
		if err != nil {
			return HostPaths{}, fmt.Errorf("make CODEX_HOME absolute: %w", err)
		}
		return HostPaths{home: filepath.Clean(absolute)}, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return HostPaths{}, fmt.Errorf("resolve user home for Codex: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return HostPaths{}, fmt.Errorf("resolved user home for Codex is empty")
	}
	return HostPaths{home: filepath.Join(home, ".codex")}, nil
}

// ConfigPath returns the selected Codex config file.
func (paths HostPaths) ConfigPath() string {
	if paths.home == "" {
		return ""
	}
	return filepath.Join(paths.home, "config.toml")
}

// PluginCachePath returns the selected versioned plugin bundle cache root.
func (paths HostPaths) PluginCachePath(key desiredextension.CarrierKey) (string, error) {
	if paths.home == "" {
		return "", fmt.Errorf("Codex host paths are unresolved")
	}
	if err := key.Validate(); err != nil {
		return "", fmt.Errorf("Codex plugin cache identity: %w", err)
	}
	if key.Carrier() != desiredextension.CarrierCodexPlugin {
		return "", fmt.Errorf("Codex plugin cache does not support carrier %q", key.Carrier())
	}
	selector, ok := key.Source().MarketplaceSelector()
	if !ok {
		return "", fmt.Errorf("Codex plugin cache requires PLUGIN@MARKETPLACE source")
	}
	return filepath.Join(
		paths.home,
		"plugins",
		"cache",
		selector.Marketplace(),
		selector.Plugin(),
	), nil
}
