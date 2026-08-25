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
	Reason     adoptmodel.SkipReason
	Detail     string
	Category   adoptmodel.SkipCategory
	ActionHint adoptmodel.SkipActionHint
}

// PrintImportSkippedReport renders bounded typed skip evidence in the selected
// human mode and marks operation-wide diagnostic-budget exhaustion explicitly.
func PrintImportSkippedReport(
	output io.Writer,
	skipped []adoptmodel.Skipped,
	options HumanOptions,
	overflow bool,
) {
	printImportSkippedReport(output, importSkippedFromAdoption(skipped), options, overflow)
}

func printImportSkippedReport(
	output io.Writer,
	skipped []ImportSkipped,
	options HumanOptions,
	overflow bool,
) {
	if options.Verbose {
		printImportSkippedVerbose(output, skipped)
	} else {
		printImportSkippedDefault(output, skipped)
	}
	if overflow {
		fmt.Fprintln(output, "skipped detail omitted: the operation-wide skip diagnostic budget was exhausted")
		fmt.Fprintln(output, "next: reduce the observed source set or its rejected rows, then retry import")
	}
}

func printImportSkippedDefault(output io.Writer, skipped []ImportSkipped) {
	counts := importSkipCounts(skipped)
	if counts.ActionRequired != 0 {
		printImportActionRequired(output, skipped)
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

func printImportActionRequired(output io.Writer, skipped []ImportSkipped) {
	printedHeader := false
	for _, item := range skipped {
		if item.Category != adoptmodel.SkipCategoryActionRequired {
			continue
		}
		if !printedHeader {
			fmt.Fprintln(output, "action required:")
			printedHeader = true
		}
		fmt.Fprintf(
			output,
			"  skip live=%q reason=%s target=%s scope=%s",
			item.LivePath,
			item.Reason,
			item.Target,
			item.Scope,
		)
		if item.Detail != "" {
			fmt.Fprintf(output, " detail=%q", item.Detail)
		}
		fmt.Fprintln(output)
		fmt.Fprintf(output, "    next: %s\n", importSkipActionText(item.ActionHint))
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
		if item.Detail != "" {
			fmt.Fprintf(output, " detail=%q", item.Detail)
		}
		if item.ActionHint != "" {
			fmt.Fprintf(output, " action_hint=%s", item.ActionHint)
		}
		fmt.Fprintln(output)
	}
}

type importSkipGroup struct {
	target string
	reason adoptmodel.SkipReason
	count  int
}

func importSkipGroups(skipped []ImportSkipped, category adoptmodel.SkipCategory) []importSkipGroup {
	type key struct {
		target string
		reason adoptmodel.SkipReason
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
	case adoptmodel.SkipActionResolveConflict:
		return "keep one skill definition, make the definitions identical, or rename one skill"
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
			Detail:     item.Detail,
			Category:   item.Category(),
			ActionHint: item.ActionHint(),
		})
	}
	return result
}
