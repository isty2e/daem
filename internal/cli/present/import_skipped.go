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
	for _, guidance := range importSkippedGuidance(skipped, options, overflow) {
		fmt.Fprintln(output, guidance.HumanText())
	}
}

func importSkippedGuidance(skipped []ImportSkipped, options HumanOptions, overflow bool) []Guidance {
	var guidance []Guidance
	if options.Verbose {
		guidance = importSkippedVerboseGuidance(skipped)
	} else {
		guidance = importSkippedDefaultGuidance(skipped)
	}
	if overflow {
		guidance = append(guidance, importSkipOverflowGuidance()...)
	}
	return guidance
}

// ImportFailureSkippedGuidance shares the selected human-mode projection.
// Common JSON failures request the basic view, not verbose path inventory.
func ImportFailureSkippedGuidance(skipped []adoptmodel.Skipped, options HumanOptions, overflow bool) []Guidance {
	return importSkippedGuidance(importSkippedFromAdoption(skipped), options, overflow)
}

func importSkippedDefaultGuidance(skipped []ImportSkipped) []Guidance {
	var guidance []Guidance
	for _, item := range skipped {
		if item.Category != adoptmodel.SkipCategoryActionRequired {
			continue
		}
		if len(guidance) == 0 {
			guidance = append(guidance, textGuidance(GuidanceNote, "", "action required:"))
		}
		message := fmt.Sprintf("skip live=%q reason=%s target=%s scope=%s", item.LivePath, item.Reason, item.Target, item.Scope)
		if item.Detail != "" {
			message += fmt.Sprintf(" detail=%q", item.Detail)
		}
		// Typed skip admission owns the budgets; quoted path/detail fields are
		// already display-safe and must not be escaped or truncated a second time.
		guidance = append(
			guidance,
			Guidance{kind: GuidanceNote, message: message, humanPrefix: "  "},
			textGuidance(GuidanceInstruction, "    next: ", importSkipActionText(item.ActionHint)),
		)
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
		guidance = append(guidance, textGuidance(GuidanceNote, "", string(category)+":"))
		for _, group := range groups {
			guidance = append(guidance, textGuidance(GuidanceNote, "  ", fmt.Sprintf("target=%s reason=%s count=%d", group.target, group.reason, group.count)))
		}
	}
	if compacted {
		guidance = append(guidance, textGuidance(GuidanceInstruction, "skipped detail: ", "rerun with --verbose to inspect every skipped path"))
	}
	return guidance
}

func importSkipOverflowGuidance() []Guidance {
	return []Guidance{
		textGuidance(GuidanceNote, "", "skipped detail omitted: the operation-wide skip diagnostic budget was exhausted"),
		InstructionGuidance("reduce the observed source set or its rejected rows, then retry import"),
	}
}

func importSkippedVerboseGuidance(skipped []ImportSkipped) []Guidance {
	guidance := make([]Guidance, 0, len(skipped))
	for _, item := range skipped {
		message := fmt.Sprintf(
			"skip live=%q reason=%s target=%s scope=%s category=%s",
			item.LivePath,
			item.Reason,
			item.Target,
			item.Scope,
			item.Category,
		)
		if item.Detail != "" {
			message += fmt.Sprintf(" detail=%q", item.Detail)
		}
		if item.ActionHint != "" {
			message += fmt.Sprintf(" action_hint=%s", item.ActionHint)
		}
		guidance = append(guidance, Guidance{kind: GuidanceNote, message: message})
	}
	return guidance
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
