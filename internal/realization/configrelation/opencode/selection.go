package opencode

import (
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/target"
)

// ConfigKind identifies one OpenCode plugin contribution document.
type ConfigKind string

const (
	ConfigServer ConfigKind = "server"
	ConfigTUI    ConfigKind = "tui"
)

// CandidateNames returns the host-selected config filenames in precedence
// order. The first candidate is also the creation/default-absence path.
func CandidateNames(kind ConfigKind) ([]string, error) {
	switch kind {
	case ConfigServer:
		return []string{"opencode.json", "opencode.jsonc"}, nil
	case ConfigTUI:
		return []string{"tui.json", "tui.jsonc"}, nil
	default:
		return nil, fmt.Errorf("unsupported OpenCode config kind %q", kind)
	}
}

// ConfigDirectory returns the selected OpenCode config directory without
// consulting process state. globalRoot is the complete OpenCode config root.
func ConfigDirectory(
	manifestRoot string,
	globalRoot string,
	scope target.Scope,
) (string, error) {
	switch scope {
	case target.ScopeProject:
		if err := validateAbsoluteCleanPath(
			"OpenCode project manifest root",
			manifestRoot,
		); err != nil {
			return "", err
		}
		return filepath.Join(manifestRoot, ".opencode"), nil
	case target.ScopeGlobal:
		if err := validateAbsoluteCleanPath(
			"OpenCode global config root",
			globalRoot,
		); err != nil {
			return "", err
		}
		return globalRoot, nil
	default:
		return "", fmt.Errorf(
			"OpenCode plugin relation scope %q has no config directory",
			scope,
		)
	}
}

// DefaultGlobalConfigRoot derives OpenCode's global config root from already
// observed process roots.
func DefaultGlobalConfigRoot(xdgConfigHome string, homeRoot string) (string, error) {
	base := xdgConfigHome
	if base == "" {
		if err := validateAbsoluteCleanPath("user home", homeRoot); err != nil {
			return "", err
		}
		base = filepath.Join(homeRoot, ".config")
	}
	if err := validateAbsoluteCleanPath("XDG config home", base); err != nil {
		return "", err
	}
	return filepath.Join(base, "opencode"), nil
}

func validateAbsoluteCleanPath(label string, value string) error {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return fmt.Errorf("%s %q must be absolute and clean", label, value)
	}
	return nil
}
