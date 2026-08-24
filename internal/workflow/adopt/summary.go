package adopt

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
)

type nothingToImportError struct {
	message string
	skipped []adoptmodel.Skipped
}

func newNothingToImportError(scans []adoptmodel.Scan, skipped []adoptmodel.Skipped) error {
	return &nothingToImportError{
		message: adoptmodel.ErrNothingToImport.Error() + scanSummary(scans) + skippedSummary(skipped),
		skipped: append([]adoptmodel.Skipped(nil), skipped...),
	}
}

func (err *nothingToImportError) Error() string {
	return err.message
}

func (err *nothingToImportError) Unwrap() error {
	return adoptmodel.ErrNothingToImport
}

// NothingToImportSkipped returns an immutable copy of typed skip evidence.
func NothingToImportSkipped(err error) []adoptmodel.Skipped {
	var detail *nothingToImportError
	if !errors.As(err, &detail) {
		return nil
	}
	return append([]adoptmodel.Skipped(nil), detail.skipped...)
}

func skippedSummary(skipped []adoptmodel.Skipped) string {
	if len(skipped) == 0 {
		return ""
	}

	var actionRequired int
	var unsupported int
	var informational int
	type groupKey struct {
		category adoptmodel.SkipCategory
		target   string
		reason   adoptmodel.SkipReason
	}
	groups := make(map[groupKey]int)
	actionable := make([]string, 0)
	for _, value := range skipped {
		category := value.Category()
		switch category {
		case adoptmodel.SkipCategoryActionRequired:
			actionRequired++
		case adoptmodel.SkipCategoryUnsupported:
			unsupported++
		case adoptmodel.SkipCategoryInformational:
			informational++
		}
		if category == adoptmodel.SkipCategoryActionRequired {
			actionable = append(actionable, fmt.Sprintf("%s: %s", value.LivePath, value.Reason))
			continue
		}
		groups[groupKey{
			category: category,
			target:   string(value.Target),
			reason:   value.Reason,
		}]++
	}

	parts := []string{fmt.Sprintf(
		"action_required=%d unsupported=%d informational=%d",
		actionRequired,
		unsupported,
		informational,
	)}
	if len(actionable) != 0 {
		parts = append(parts, "action_required "+strings.Join(actionable, ", "))
	}
	orderedGroups := make([]groupKey, 0, len(groups))
	for key := range groups {
		orderedGroups = append(orderedGroups, key)
	}
	sort.Slice(orderedGroups, func(left int, right int) bool {
		if orderedGroups[left].category != orderedGroups[right].category {
			return orderedGroups[left].category < orderedGroups[right].category
		}
		if orderedGroups[left].target != orderedGroups[right].target {
			return orderedGroups[left].target < orderedGroups[right].target
		}
		return orderedGroups[left].reason < orderedGroups[right].reason
	})
	for _, key := range orderedGroups {
		parts = append(parts, fmt.Sprintf(
			"%s target=%s reason=%s count=%d",
			key.category,
			key.target,
			key.reason,
			groups[key],
		))
	}

	return " (skipped " + strings.Join(parts, "; ") + ")"
}

func scanSummary(scans []adoptmodel.Scan) string {
	if len(scans) == 0 {
		return ""
	}

	parts := make([]string, 0, len(scans))
	for _, scan := range scans {
		parts = append(
			parts,
			fmt.Sprintf(
				"%s: %s entries=%d imported=%d skipped=%d",
				scan.LivePath,
				scan.Status,
				scan.Entries,
				scan.Imported,
				scan.Skipped,
			),
		)
	}

	return " (scanned " + strings.Join(parts, ", ") + ")"
}
