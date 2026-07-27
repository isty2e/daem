package skill

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/target"
)

// TargetPlacement is one immutable target-specific skill-root request.
// Host admission remains a realization concern.
type TargetPlacement struct {
	installTo string
}

// NewTargetPlacement constructs one canonical scope-relative root request.
func NewTargetPlacement(scope target.Scope, installTo string) (TargetPlacement, error) {
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return TargetPlacement{}, err
	}
	if err := validateInstallTo(parsedScope, installTo); err != nil {
		return TargetPlacement{}, err
	}
	return TargetPlacement{installTo: installTo}, nil
}

// InstallTo returns the requested portable skill-root spelling.
func (placement TargetPlacement) InstallTo() string { return placement.installTo }

func validateTargetPlacements(
	owner string,
	targets target.Set,
	scope target.Scope,
	values map[target.Target]TargetPlacement,
) (map[target.Target]TargetPlacement, error) {
	result := make(map[target.Target]TargetPlacement, len(values))
	keys := make([]target.Target, 0, len(values))
	for selectedTarget := range values {
		keys = append(keys, selectedTarget)
	}
	slices.Sort(keys)

	for _, selectedTarget := range keys {
		parsedTarget, err := target.ParseTarget(string(selectedTarget))
		if err != nil {
			return nil, fmt.Errorf("%s target %q: %w", owner, selectedTarget, err)
		}
		if !targets.Contains(parsedTarget) {
			return nil, fmt.Errorf("%s target %q is not declared for the skill", owner, selectedTarget)
		}
		canonical, err := NewTargetPlacement(scope, values[selectedTarget].installTo)
		if err != nil {
			return nil, fmt.Errorf("%s target %q: %w", owner, selectedTarget, err)
		}
		result[parsedTarget] = canonical
	}
	return result, nil
}

func cloneTargetPlacements(values map[target.Target]TargetPlacement) map[target.Target]TargetPlacement {
	result := make(map[target.Target]TargetPlacement, len(values))
	maps.Copy(result, values)
	return result
}

func validateInstallTo(scope target.Scope, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("install_to must be valid UTF-8")
	}
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("install_to must be non-empty and trimmed")
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r)
	}) >= 0 {
		return fmt.Errorf("install_to must not contain control characters")
	}
	if strings.Contains(value, "\\") {
		return fmt.Errorf("install_to %q must use slash separators", value)
	}
	if path.Clean(value) != value || value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return fmt.Errorf("install_to %q must be a canonical directory path without parent traversal", value)
	}

	switch scope {
	case target.ScopeProject:
		if path.IsAbs(value) || strings.HasPrefix(value, "~") {
			return fmt.Errorf("project install_to %q must be project-relative", value)
		}
	case target.ScopeGlobal:
		relative := strings.TrimPrefix(value, "~/")
		if relative == value || relative == "" || path.IsAbs(relative) ||
			relative == ".." || strings.HasPrefix(relative, "../") {
			return fmt.Errorf("global install_to %q must start with ~/ and remain inside the home directory", value)
		}
	default:
		return fmt.Errorf("unknown scope %q", scope)
	}
	return nil
}
