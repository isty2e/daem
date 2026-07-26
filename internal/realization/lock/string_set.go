package lock

import (
	"fmt"
	"sort"
	"strings"
)

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		normalized = append(normalized, strings.TrimSpace(value))
	}
	sort.Strings(normalized)
	return normalized
}

func validateStringSet(values []string, context string) error {
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return fmt.Errorf("%s[%d] is required", context, index)
		}
		if _, exists := seen[trimmed]; exists {
			return fmt.Errorf("duplicate %s %q", context, trimmed)
		}
		seen[trimmed] = struct{}{}
	}
	return nil
}
