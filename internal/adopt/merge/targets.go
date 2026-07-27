package merge

import (
	"slices"
	"strings"

	targetpkg "github.com/isty2e/daem/internal/target"
)

func missingImportTargets(existingTargets []string, importedTargets []targetpkg.Target) []targetpkg.Target {
	missing := make(map[targetpkg.Target]struct{}, len(importedTargets))
	for _, target := range importedTargets {
		if !containsStringTarget(existingTargets, target) {
			missing[target] = struct{}{}
		}
	}
	ordered := make([]targetpkg.Target, 0, len(missing))
	for _, selectedTarget := range targetpkg.SupportedTargets() {
		if _, ok := missing[selectedTarget]; ok {
			ordered = append(ordered, selectedTarget)
		}
	}
	return ordered
}

func containsStringTarget(targets []string, target targetpkg.Target) bool {
	return slices.Contains(targets, string(target))
}

func containsTarget(targets []targetpkg.Target, target targetpkg.Target) bool {
	return slices.Contains(targets, target)
}

func mergeImportStringTargets(existing []string, additions []string) []string {
	merged := append([]string{}, existing...)
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		merged = append(merged, value)
	}
	return merged
}

func sameImportStringTargets(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func importDomainTargetsText(targets []targetpkg.Target) string {
	values := make([]string, 0, len(targets))
	for _, target := range targets {
		values = append(values, string(target))
	}
	return strings.Join(values, ",")
}
