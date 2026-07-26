package target

import (
	"fmt"
	"slices"
	"strings"
)

// Target is an agent host that can receive rendered resources.
type Target string

const (
	TargetCodex          Target = "codex"
	TargetClaudeCode     Target = "claude-code"
	TargetOpenCode       Target = "opencode"
	TargetPi             Target = "pi"
	TargetAntigravityCLI Target = "antigravity-cli"
)

var supportedTargets = []Target{
	TargetCodex,
	TargetClaudeCode,
	TargetOpenCode,
	TargetPi,
	TargetAntigravityCLI,
}

// Scope identifies whether a resource is installed globally or for a project.
type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
)

// SupportedTargets returns every target accepted by ParseTarget in stable order.
func SupportedTargets() []Target {
	return append([]Target(nil), supportedTargets...)
}

// ParseTarget validates a target string.
func ParseTarget(value string) (Target, error) {
	parsed := Target(value)
	if slices.Contains(supportedTargets, parsed) {
		return parsed, nil
	}

	return "", fmt.Errorf("unknown target %q (accepted targets: %s)", value, targetValues(supportedTargets))
}

func targetValues(targets []Target) string {
	values := make([]string, 0, len(targets))
	for _, target := range targets {
		values = append(values, string(target))
	}

	return strings.Join(values, ", ")
}

// ParseScope validates a scope string.
func ParseScope(value string) (Scope, error) {
	switch Scope(value) {
	case ScopeGlobal, ScopeProject:
		return Scope(value), nil
	default:
		return "", fmt.Errorf("unknown scope %q", value)
	}
}
