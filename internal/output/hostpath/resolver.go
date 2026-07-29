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
	projectRoot         string
	dataRoot            string
	destinationOverride DestinationOverrideResolver
}

// DestinationOverrideResolver maps an exact canonical logical destination to a
// host-selected physical path. The bool is false when normal root expansion
// should continue.
type DestinationOverrideResolver func(output.Destination) (string, bool, error)

// NewResolver constructs a resolver with project and home roots.
func NewResolver(projectRoot string) Resolver {
	return Resolver{projectRoot: projectRoot}
}

// NewResolverWithManagedDataRoot constructs a resolver with project, home, and
// daem-data roots.
func NewResolverWithManagedDataRoot(projectRoot string, dataRoot string) Resolver {
	return Resolver{projectRoot: projectRoot, dataRoot: dataRoot}
}

// WithDestinationOverride returns a copy that consults one target-owned
// physical-path policy before generic root expansion.
func (resolver Resolver) WithDestinationOverride(override DestinationOverrideResolver) Resolver {
	resolver.destinationOverride = override
	return resolver
}

// Resolve expands a canonical portable destination without changing its identity.
func (resolver Resolver) Resolve(destination output.Destination) (string, error) {
	if err := destination.Validate(); err != nil {
		return "", err
	}
	if resolver.destinationOverride != nil {
		path, matched, err := resolver.destinationOverride(destination)
		if err != nil {
			return "", err
		}
		if matched {
			if strings.TrimSpace(path) == "" || strings.TrimSpace(path) != path || !filepath.IsAbs(path) {
				return "", fmt.Errorf("destination override for %q must return a trimmed absolute path", destination)
			}
			return filepath.Clean(path), nil
		}
	}
	switch destination.RootRole() {
	case output.RootHome:
		homeDirectory, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for %q: %w", destination, err)
		}
		if strings.TrimSpace(homeDirectory) != homeDirectory || !filepath.IsAbs(homeDirectory) {
			return "", fmt.Errorf("resolve home directory for %q: home root must be a trimmed absolute path", destination)
		}
		return filepath.Join(homeDirectory, filepath.FromSlash(destination.RelativePath())), nil
	case output.RootData:
		dataRoot, err := cleanManagedDataRoot(resolver.dataRoot)
		if err != nil {
			return "", fmt.Errorf("resolve data-root destination %q: %w", destination, err)
		}
		return filepath.Join(dataRoot, filepath.FromSlash(destination.RelativePath())), nil
	case output.RootProject:
		projectRoot, err := cleanProjectRoot(resolver.projectRoot)
		if err != nil {
			return "", fmt.Errorf("resolve project destination %q: %w", destination, err)
		}
		return filepath.Join(projectRoot, filepath.FromSlash(destination.RelativePath())), nil
	default:
		return "", fmt.Errorf("destination %q has unsupported root role %q", destination, destination.RootRole())
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
