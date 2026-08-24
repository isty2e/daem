package clipresent

import (
	"fmt"
	"io"
	"sort"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
)

type importSkipCountsValue struct {
	ActionRequired int
	Unsupported    int
	Informational  int
}

type ImportSkipped struct {
	Target     string
	Scope      string
	LivePath   string
	Reason     string
	Category   adoptmodel.SkipCategory
	ActionHint adoptmodel.SkipActionHint
}

func printImportSkippedDefault(output io.Writer, skipped []ImportSkipped) {
	counts := importSkipCounts(skipped)
	if counts.ActionRequired != 0 {
		fmt.Fprintln(output, "action required:")
		for _, item := range skipped {
			if item.Category != adoptmodel.SkipCategoryActionRequired {
				continue
			}
			fmt.Fprintf(
				output,
				"  skip live=%q reason=%s target=%s scope=%s\n",
				item.LivePath,
				item.Reason,
				item.Target,
				item.Scope,
			)
			fmt.Fprintf(output, "    next: %s\n", importSkipActionText(item.ActionHint))
		}
	}

	compacted := false
	for _, category := range []adoptmodel.SkipCategory{
		adoptmodel.SkipCategoryUnsupported,
		adoptmodel.SkipCategoryInformational,
	} {
		groups := importSkipGroups(skipped, category)
		if len(groups) == 0 {
			continue
		}
		compacted = true
		fmt.Fprintf(output, "%s:\n", category)
		for _, group := range groups {
			fmt.Fprintf(
				output,
				"  target=%s reason=%s count=%d\n",
				group.target,
				group.reason,
				group.count,
			)
		}
	}
	if compacted {
		fmt.Fprintln(output, "skipped detail: rerun with --verbose to inspect every skipped path")
	}
}

func printImportSkippedVerbose(output io.Writer, skipped []ImportSkipped) {
	for _, item := range skipped {
		fmt.Fprintf(
			output,
			"skip live=%q reason=%s target=%s scope=%s category=%s",
			item.LivePath,
			item.Reason,
			item.Target,
			item.Scope,
			item.Category,
		)
		if item.ActionHint != "" {
			fmt.Fprintf(output, " action_hint=%s", item.ActionHint)
		}
		fmt.Fprintln(output)
	}
}

type importSkipGroup struct {
	target string
	reason string
	count  int
}

func importSkipGroups(skipped []ImportSkipped, category adoptmodel.SkipCategory) []importSkipGroup {
	type key struct {
		target string
		reason string
	}
	counts := make(map[key]int)
	for _, item := range skipped {
		if item.Category != category {
			continue
		}
		counts[key{target: item.Target, reason: item.Reason}]++
	}
	groups := make([]importSkipGroup, 0, len(counts))
	for key, count := range counts {
		groups = append(groups, importSkipGroup{
			target: key.target,
			reason: key.reason,
			count:  count,
		})
	}
	sort.Slice(groups, func(left int, right int) bool {
		if groups[left].target != groups[right].target {
			return groups[left].target < groups[right].target
		}
		return groups[left].reason < groups[right].reason
	})
	return groups
}

func importSkipActionText(actionHint adoptmodel.SkipActionHint) string {
	switch actionHint {
	case adoptmodel.SkipActionUseSymbolicEnvironment:
		return "replace literal secrets with symbolic environment references or leave this row unmanaged"
	case adoptmodel.SkipActionAuthorExplicitSource:
		return "author the exact source explicitly or leave this row unmanaged"
	case adoptmodel.SkipActionRetryWhenStable:
		return "retry after the live source stops changing"
	case adoptmodel.SkipActionReduceSource:
		return "reduce the live source to the supported limits or leave it unmanaged"
	case adoptmodel.SkipActionReplaceUnsupportedEntry:
		return "replace the live entry with a supported file or directory, or leave it unmanaged"
	case adoptmodel.SkipActionRepairSource:
		return "repair the live source to a supported form or leave it unmanaged"
	default:
		return "review the live source and author it explicitly, or leave it unmanaged"
	}
}

func importSkipCounts(skipped []ImportSkipped) importSkipCountsValue {
	var counts importSkipCountsValue
	for _, item := range skipped {
		switch item.Category {
		case adoptmodel.SkipCategoryActionRequired:
			counts.ActionRequired++
		case adoptmodel.SkipCategoryUnsupported:
			counts.Unsupported++
		case adoptmodel.SkipCategoryInformational:
			counts.Informational++
		}
	}
	return counts
}

func importSkippedFromAdoption(skipped []adoptmodel.Skipped) []ImportSkipped {
	result := make([]ImportSkipped, 0, len(skipped))
	for _, item := range skipped {
		result = append(result, ImportSkipped{
			Target:     string(item.Target),
			Scope:      string(item.Scope),
			LivePath:   item.LivePath,
			Reason:     item.Reason,
			Category:   item.Category(),
			ActionHint: item.ActionHint(),
		})
	}
	return result
}
