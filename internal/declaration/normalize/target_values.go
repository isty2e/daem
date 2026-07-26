package normalize

import (
	"fmt"
	"strings"

	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/target"
)

func normalizeTargets(values []string, context string) ([]target.Target, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%s: at least one target is required", context)
	}

	targets := make([]target.Target, 0, len(values))
	seen := make(map[target.Target]struct{}, len(values))

	for index, value := range values {
		parsedTarget, err := target.ParseTarget(value)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", context, index, err)
		}

		if _, exists := seen[parsedTarget]; exists {
			return nil, fmt.Errorf("%s[%d]: duplicate target %q", context, index, parsedTarget)
		}

		seen[parsedTarget] = struct{}{}
		targets = append(targets, parsedTarget)
	}

	return targets, nil
}

func targetsWithDefault(values []string, defaultTargets []target.Target, context string) ([]target.Target, error) {
	if len(values) == 0 {
		return append([]target.Target(nil), defaultTargets...), nil
	}

	return normalizeTargets(values, context)
}

func scopeWithDefault(value string, defaultScope target.Scope, context string) (target.Scope, error) {
	if value == "" {
		return defaultScope, nil
	}

	scope, err := target.ParseScope(value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", context, err)
	}

	return scope, nil
}

func installModeWithDefault(value string, defaultMode desiredskill.InstallMode, context string) (desiredskill.InstallMode, error) {
	if value == "" {
		return defaultMode, nil
	}

	installMode, err := desiredskill.ParseInstallMode(value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", context, err)
	}

	return installMode, nil
}

func requiredExactString(value string, context string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s: required", context)
	}
	if strings.TrimSpace(value) != value {
		return "", fmt.Errorf("%s: must not contain leading or trailing whitespace", context)
	}

	return value, nil
}

func targetList(targets []target.Target) string {
	values := make([]string, 0, len(targets))
	for _, selected := range targets {
		values = append(values, string(selected))
	}

	return strings.Join(values, " ")
}
