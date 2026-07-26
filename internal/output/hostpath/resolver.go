// Package hostpath resolves canonical output destinations at the host boundary.
package hostpath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/output"
)

// Resolver expands canonical portable destinations using operation-selected roots.
type Resolver struct {
	projectRoot string
	dataRoot    string
}

// NewResolver constructs a resolver with project and home roots.
func NewResolver(projectRoot string) Resolver {
	return Resolver{projectRoot: projectRoot}
}

// NewResolverWithManagedDataRoot constructs a resolver with project, home, and
// daem-data roots.
func NewResolverWithManagedDataRoot(projectRoot string, dataRoot string) Resolver {
	return Resolver{projectRoot: projectRoot, dataRoot: dataRoot}
}

// Resolve expands a canonical portable destination without changing its identity.
func (resolver Resolver) Resolve(destination output.Destination) (string, error) {
	value := string(destination)
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("destination is required")
	}

	portable, err := output.Parse(value)
	if err != nil {
		return "", err
	}
	switch portable.RootRole() {
	case output.RootHome:
		homeDirectory, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for %q: %w", destination, err)
		}
		if strings.TrimSpace(homeDirectory) != homeDirectory || !filepath.IsAbs(homeDirectory) {
			return "", fmt.Errorf("resolve home directory for %q: home root must be a trimmed absolute path", destination)
		}
		return filepath.Join(homeDirectory, filepath.FromSlash(portable.RelativePath())), nil
	case output.RootData:
		dataRoot, err := cleanManagedDataRoot(resolver.dataRoot)
		if err != nil {
			return "", fmt.Errorf("resolve data-root destination %q: %w", destination, err)
		}
		return filepath.Join(dataRoot, filepath.FromSlash(portable.RelativePath())), nil
	case output.RootProject:
		projectRoot, err := cleanProjectRoot(resolver.projectRoot)
		if err != nil {
			return "", fmt.Errorf("resolve project destination %q: %w", destination, err)
		}
		return filepath.Join(projectRoot, filepath.FromSlash(portable.RelativePath())), nil
	default:
		return "", fmt.Errorf("destination %q has unsupported root role %q", destination, portable.RootRole())
	}
}

func cleanManagedDataRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("managed data root is required")
	}
	if strings.TrimSpace(root) != root {
		return "", fmt.Errorf("managed data root must be trimmed")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("managed data root must be absolute")
	}
	cleaned := filepath.Clean(root)
	if filepath.Dir(cleaned) == cleaned {
		return "", fmt.Errorf("managed data root must not be a filesystem root")
	}
	return cleaned, nil
}

func cleanProjectRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("project root is required")
	}
	if strings.TrimSpace(root) != root {
		return "", fmt.Errorf("project root must be trimmed")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("project root must be absolute")
	}
	return filepath.Clean(root), nil
}
