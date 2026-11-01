package relationhost

import (
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/observe/antigravityplugin"
	"github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	"github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
)

// OpaquePluginNamespaces supplies private write-exclusion envelopes, not rewrite
// permission. Unhandled hosts retain their separate operation-specific profiles.
func OpaquePluginNamespaces(paths daempaths.Paths, selected target.Target, scope target.Scope) ([]relationobserve.AuthorityPath, bool, error) {
	if _, err := target.ParseScope(string(scope)); err != nil {
		return nil, false, err
	}
	var result []relationobserve.AuthorityPath
	add := func(path string, scope target.Scope) error {
		value, err := relationobserve.NewAuthorityPath(path, selected, scope)
		if err != nil {
			return err
		}
		result = append(result, value)
		return nil
	}
	switch selected {
	case target.TargetOpenCode:
		candidates, err := opencodeplugin.OrderAuthorityPaths(opencodeplugin.InventoryInput{ManifestRoot: paths.ManifestRoot, Scope: scope})
		if err != nil {
			return nil, true, err
		}
		for _, path := range candidates {
			if err := add(path, scope); err != nil {
				return nil, true, err
			}
		}
	case target.TargetClaudeCode:
		inventory, err := claudeplugin.InstalledInventoryPath(claudeplugin.InstalledInventoryInput{WorkDir: paths.ManifestRoot, ProjectRoot: paths.ManifestRoot})
		if err != nil {
			return nil, true, err
		}
		if err := add(filepath.Dir(filepath.Dir(inventory)), target.ScopeGlobal); err != nil {
			return nil, true, err
		}
		if scope == target.ScopeProject {
			if err := add(filepath.Join(paths.ManifestRoot, ".claude"), scope); err != nil {
				return nil, true, err
			}
		}
	case target.TargetAntigravityCLI:
		if scope != target.ScopeGlobal {
			return nil, true, fmt.Errorf("Antigravity plugin namespace requires global scope")
		}
		config, err := antigravityplugin.ResolveHostPaths()
		if err != nil {
			return nil, true, err
		}
		if err := add(filepath.Dir(config.ImportManifestPath()), scope); err != nil {
			return nil, true, err
		}
		if err := add(config.PluginStorePath(), scope); err != nil {
			return nil, true, err
		}
	default:
		return nil, false, nil
	}
	return result, true, nil
}
