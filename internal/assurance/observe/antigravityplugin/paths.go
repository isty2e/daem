package antigravityplugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HostPaths is the Antigravity CLI shared plugin-config boundary.
type HostPaths struct {
	configRoot string
}

// ResolveHostPaths resolves the shared config root used by Antigravity CLI.
func ResolveHostPaths() (HostPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return HostPaths{}, fmt.Errorf("resolve user home for Antigravity CLI: %w", err)
	}
	if strings.TrimSpace(home) == "" || strings.TrimSpace(home) != home {
		return HostPaths{}, fmt.Errorf("resolved user home for Antigravity CLI is empty or untrimmed")
	}
	if strings.ContainsRune(home, '\x00') {
		return HostPaths{}, fmt.Errorf("resolved user home for Antigravity CLI contains a NUL byte")
	}
	absolute, err := filepath.Abs(home)
	if err != nil {
		return HostPaths{}, fmt.Errorf("make Antigravity CLI home absolute: %w", err)
	}
	return HostPaths{
		configRoot: filepath.Join(filepath.Clean(absolute), ".gemini", "config"),
	}, nil
}

// ImportManifestPath returns the host-owned plugin import manifest.
func (paths HostPaths) ImportManifestPath() string {
	if paths.configRoot == "" {
		return ""
	}
	return filepath.Join(paths.configRoot, "import_manifest.json")
}

// PluginManifestPath returns the exact installed plugin bundle manifest.
func (paths HostPaths) PluginManifestPath(plugin string) (string, error) {
	directory, err := paths.PluginDirectoryPath(plugin)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "plugin.json"), nil
}

// PluginDirectoryPath returns the exact installed plugin bundle root.
func (paths HostPaths) PluginDirectoryPath(plugin string) (string, error) {
	if paths.configRoot == "" {
		return "", fmt.Errorf("Antigravity CLI host paths are unresolved")
	}
	if !stablePluginToken(plugin) {
		return "", fmt.Errorf("Antigravity CLI plugin name %q is not path-safe", plugin)
	}
	return filepath.Join(paths.configRoot, "plugins", plugin), nil
}

func stablePluginToken(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		asciiAlnum := (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9')
		if index == 0 && !asciiAlnum {
			return false
		}
		if !asciiAlnum && character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}
