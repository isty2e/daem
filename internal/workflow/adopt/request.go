package adopt

import (
	"fmt"
	"path/filepath"
	"strings"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	daempaths "github.com/isty2e/daem/internal/paths"
	targetpkg "github.com/isty2e/daem/internal/target"
)

// RequestInput is the normalized import request after CLI flag parsing.
type RequestInput struct {
	Targets      []targetpkg.Target
	Scopes       []targetpkg.Scope
	ManifestPath string
	SourceDir    string
	Merge        bool
}

// NewRequest resolves paths and validates normalized import inputs.
func NewRequest(input RequestInput) (adoptmodel.Request, error) {
	if len(input.Targets) == 0 {
		return adoptmodel.Request{}, fmt.Errorf("--target is required")
	}
	if len(input.Scopes) == 0 {
		input.Scopes = []targetpkg.Scope{targetpkg.ScopeProject}
	}
	for _, target := range input.Targets {
		if !adoptmodel.SupportsTarget(target) {
			return adoptmodel.Request{}, fmt.Errorf("target %q is not supported by import", target)
		}
	}
	for _, scope := range input.Scopes {
		if scope != targetpkg.ScopeProject && scope != targetpkg.ScopeGlobal {
			return adoptmodel.Request{}, fmt.Errorf("scope %q is not supported by import", scope)
		}
	}

	output, resolvedSourceDir, err := resolveImportPaths(input.ManifestPath, input.SourceDir, input.Merge)
	if err != nil {
		return adoptmodel.Request{}, err
	}
	sourceDirectory, err := adoptmodel.NewSourceDirectory(output, resolvedSourceDir)
	if err != nil {
		return adoptmodel.Request{}, err
	}
	return adoptmodel.NewRequest(input.Targets, input.Scopes, output, sourceDirectory, input.Merge)
}

func resolveImportPaths(manifestPath string, sourceDir string, merge bool) (string, string, error) {
	var output string
	if strings.TrimSpace(manifestPath) == "" {
		var resolvedPaths daempaths.Paths
		var err error
		if merge {
			resolvedPaths, err = daempaths.Resolve("")
		} else {
			resolvedPaths, err = daempaths.ResolveCreation("")
		}
		if err != nil {
			return "", "", err
		}
		output = resolvedPaths.ManifestPath
	} else {
		absoluteOutput, err := filepath.Abs(manifestPath)
		if err != nil {
			return "", "", fmt.Errorf("resolve output path: %w", err)
		}
		output = absoluteOutput
	}
	output = filepath.Clean(output)
	outputDir := filepath.Dir(output)

	resolvedSourceDir := sourceDir
	if strings.TrimSpace(resolvedSourceDir) == "" {
		resolvedSourceDir = DefaultSourceDir(filepath.Base(output))
	}
	if filepath.IsAbs(resolvedSourceDir) {
		resolvedSourceDir = filepath.Clean(resolvedSourceDir)
	} else {
		resolvedSourceDir = filepath.Clean(filepath.Join(outputDir, resolvedSourceDir))
	}

	return output, resolvedSourceDir, nil
}

// DefaultSourceDir returns the default local source directory for an output manifest basename.
func DefaultSourceDir(outputBase string) string {
	stem := strings.TrimSuffix(outputBase, filepath.Ext(outputBase))
	if stem == "" {
		stem = outputBase
	}
	return stem + ".d"
}
