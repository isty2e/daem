package profile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/target"
)

func validateTargetSet(values []target.Target) error {
	if len(values) == 0 {
		return fmt.Errorf("at least one consumer target is required")
	}
	canonical, err := target.CanonicalSet(values)
	if err != nil {
		return fmt.Errorf("consumer targets: %w", err)
	}
	if len(values) != len(canonical) {
		return fmt.Errorf("consumer targets must be canonical and unique")
	}
	for index := range values {
		if values[index] != canonical[index] {
			return fmt.Errorf("consumer targets must be sorted")
		}
	}
	return nil
}

func canonicalStringSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		canonical := strings.TrimSpace(value)
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	sort.Strings(result)
	return result
}

func validateStringSet(values []string, label string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s set must not be empty", label)
	}
	for index, value := range values {
		if err := validateProfileToken(label, value); err != nil {
			return err
		}
		if index > 0 && values[index-1] >= value {
			return fmt.Errorf("%s set must be sorted and unique", label)
		}
	}
	return nil
}
