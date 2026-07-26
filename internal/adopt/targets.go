package adopt

import (
	"strings"

	targetpkg "github.com/isty2e/daem/internal/target"
)

// SupportedTargets returns targets with live import discovery surfaces.
func SupportedTargets() []targetpkg.Target {
	return []targetpkg.Target{
		targetpkg.TargetCodex,
		targetpkg.TargetClaudeCode,
		targetpkg.TargetOpenCode,
		targetpkg.TargetPi,
		targetpkg.TargetAntigravityCLI,
	}
}

// SupportsTarget reports whether target has a live import discovery realization.
func SupportsTarget(target targetpkg.Target) bool {
	for _, supported := range SupportedTargets() {
		if target == supported {
			return true
		}
	}
	return false
}

func TargetStrings(targets []targetpkg.Target) []string {
	values := make([]string, 0, len(targets))
	for _, target := range targets {
		values = append(values, string(target))
	}
	return values
}

func MergeTargetStrings(existing []string, additions []targetpkg.Target) []string {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	merged := make([]string, 0, len(existing)+len(additions))
	for _, target := range existing {
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		merged = append(merged, target)
	}
	for _, target := range additions {
		targetValue := string(target)
		if _, ok := seen[targetValue]; ok {
			continue
		}
		seen[targetValue] = struct{}{}
		merged = append(merged, targetValue)
	}
	return merged
}

func OrderedTargets(targets []targetpkg.Target) []targetpkg.Target {
	seen := make(map[targetpkg.Target]struct{}, len(targets))
	for _, target := range targets {
		seen[target] = struct{}{}
	}

	ordered := make([]targetpkg.Target, 0, len(seen))
	for _, target := range targetpkg.SupportedTargets() {
		if _, ok := seen[target]; ok {
			ordered = append(ordered, target)
		}
	}
	return ordered
}

func UniqueTargets(targets []targetpkg.Target) []targetpkg.Target {
	result := make([]targetpkg.Target, 0, len(targets))
	seen := make(map[targetpkg.Target]struct{}, len(targets))
	for _, target := range targets {
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		result = append(result, target)
	}
	return result
}

func TargetsKey(targets []targetpkg.Target) string {
	ordered := OrderedTargets(targets)
	values := make([]string, 0, len(ordered))
	for _, target := range ordered {
		values = append(values, string(target))
	}
	return strings.Join(values, "\x00")
}
