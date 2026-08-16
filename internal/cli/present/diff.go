package clipresent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

type DryRunDiff struct {
	ResourceID     string
	Targets        []string
	Scope          string
	Destination    string
	CurrentLabel   string
	CurrentContent []byte
	DesiredLabel   string
	DesiredContent []byte
	OmissionReason applyworkflow.DryRunDiffOmissionReason
}

// DryRunDiffReport is the bounded human projection of one optional diff request.
type DryRunDiffReport struct {
	Diffs                       []DryRunDiff
	UninspectedManagedPathCount int
}

// DryRunDiffReportFrom projects canonical apply diffs into the stable diff contract.
func DryRunDiffReportFrom(collection applyworkflow.DryRunDiffCollection) DryRunDiffReport {
	result := make([]DryRunDiff, 0, len(collection.Diffs))
	for _, diff := range collection.Diffs {
		targets := make([]string, 0, len(diff.Targets))
		for _, selectedTarget := range diff.Targets {
			targets = append(targets, string(selectedTarget))
		}
		result = append(result, DryRunDiff{
			ResourceID:     string(diff.EntityID.Kind()) + "/" + diff.EntityID.Name(),
			Targets:        targets,
			Scope:          string(diff.Scope),
			Destination:    diff.Destination,
			CurrentLabel:   diff.CurrentLabel,
			CurrentContent: append([]byte(nil), diff.CurrentContent...),
			DesiredLabel:   diff.DesiredLabel,
			DesiredContent: append([]byte(nil), diff.DesiredContent...),
			OmissionReason: diff.OmissionReason,
		})
	}
	return DryRunDiffReport{
		Diffs:                       result,
		UninspectedManagedPathCount: collection.UninspectedManagedPathCount,
	}
}

type lineDiffOp struct {
	prefix byte
	line   string
}

const (
	maxInlineDiffCells           = 250_000
	maximumDryRunDiffReportCells = 16_000_000
	maxInlineDiffLabelBytes      = 32 * 1024
)

type dryRunDiffCellBudget struct {
	remaining int
}

func (budget *dryRunDiffCellBudget) admit(cells int) bool {
	if cells > budget.remaining {
		return false
	}
	budget.remaining -= cells
	return true
}

func PrintDryRunDiffs(
	ctx context.Context,
	output io.Writer,
	report DryRunDiffReport,
) error {
	return printDryRunDiffs(ctx, output, report, maximumDryRunDiffReportCells)
}

func printDryRunDiffs(
	ctx context.Context,
	output io.Writer,
	report DryRunDiffReport,
	maximumCells int,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	diffs := report.Diffs
	cellBudget := dryRunDiffCellBudget{remaining: maximumCells}
	workOmitted := 0
	fmt.Fprintf(output, "diff: %d files\n", len(diffs))
	for _, diff := range diffs {
		if err := ctx.Err(); err != nil {
			return err
		}
		targetLabel := "targets=" + Escape(strings.Join(diff.Targets, ","))
		if len(diff.Targets) == 1 {
			targetLabel = "target=" + Escape(diff.Targets[0])
		}
		fmt.Fprintf(
			output,
			"diff resource=%s %s scope=%s destination=%s\n",
			Quote(diff.ResourceID),
			targetLabel,
			Escape(diff.Scope),
			Quote(diff.Destination),
		)
		if diff.OmissionReason != "" {
			fmt.Fprintln(output, dryRunDiffOmissionMessage(diff.OmissionReason))
			continue
		}
		if !isTextDiffContent(diff.CurrentContent) || !isTextDiffContent(diff.DesiredContent) {
			fmt.Fprintln(output, "binary content differs; textual diff omitted")
			continue
		}
		lines, omittedForWork := formatTextDiff(
			diff.CurrentLabel,
			diff.CurrentContent,
			diff.DesiredLabel,
			diff.DesiredContent,
			&cellBudget,
		)
		if omittedForWork {
			workOmitted++
		}
		for _, line := range lines {
			fmt.Fprintln(output, line)
		}
	}
	if report.UninspectedManagedPathCount != 0 {
		fmt.Fprintf(
			output,
			"diff collection omitted %d managed-path decisions after the operation item budget was exhausted\n",
			report.UninspectedManagedPathCount,
		)
	}
	if workOmitted != 0 {
		fmt.Fprintf(
			output,
			"diff rendering omitted %d textual diffs after the operation work budget was exhausted\n",
			workOmitted,
		)
	}
	return nil
}

func isTextDiffContent(content []byte) bool {
	return !bytes.Contains(content, []byte{0}) && utf8.Valid(content)
}

func FormatTextDiff(currentLabel string, currentContent []byte, desiredLabel string, desiredContent []byte) []string {
	lines, _ := formatTextDiff(currentLabel, currentContent, desiredLabel, desiredContent, nil)
	return lines
}

func formatTextDiff(
	currentLabel string,
	currentContent []byte,
	desiredLabel string,
	desiredContent []byte,
	cellBudget *dryRunDiffCellBudget,
) ([]string, bool) {
	lines := []string{
		"--- " + displayDiffLabel(currentLabel),
		"+++ " + displayDiffLabel(desiredLabel),
		"@@",
	}
	if int64(len(currentContent)) > applyworkflow.MaximumDryRunDiffInputBytes ||
		int64(len(desiredContent)) > applyworkflow.MaximumDryRunDiffInputBytes-int64(len(currentContent)) {
		return append(lines, "text content differs; inline diff omitted because the files are too large"), false
	}
	currentLineCount := diffLineCount(currentContent)
	desiredLineCount := diffLineCount(desiredContent)
	if !inlineDiffMatrixWithinBudget(currentLineCount, desiredLineCount) {
		return append(lines, "text content differs; inline diff omitted because the files are too large"), false
	}
	if cellBudget != nil && !cellBudget.admit((currentLineCount+1)*(desiredLineCount+1)) {
		return append(
			lines,
			"text content differs; inline diff omitted because the report work budget was exhausted",
		), true
	}
	currentLines := splitDiffLines(currentContent)
	desiredLines := splitDiffLines(desiredContent)
	currentIDs, desiredIDs := canonicalDiffLineIDs(currentLines, desiredLines)
	for _, op := range lineDiff(currentLines, desiredLines, currentIDs, desiredIDs) {
		lines = append(lines, formatLineDiffOp(op)...)
	}

	return lines, false
}

func displayDiffLabel(label string) string {
	if len(label) > maxInlineDiffLabelBytes {
		return "[label omitted because it is too large]"
	}
	return Escape(label)
}

func inlineDiffMatrixWithinBudget(currentLines int, desiredLines int) bool {
	rows := currentLines + 1
	columns := desiredLines + 1
	return columns <= maxInlineDiffCells/rows
}

func PrintManifestDiff(output io.Writer, currentLabel string, currentContent []byte, desiredLabel string, desiredContent []byte) {
	fmt.Fprintln(output, "manifest diff:")
	for _, line := range FormatTextDiff(currentLabel, currentContent, desiredLabel, desiredContent) {
		fmt.Fprintln(output, line)
	}
}

func splitDiffLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}

	parts := strings.SplitAfter(string(content), "\n")
	if parts[len(parts)-1] == "" {
		return parts[:len(parts)-1]
	}

	return parts
}

func diffLineCount(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := 1
	for _, value := range content {
		if value == '\n' {
			count++
		}
	}
	if content[len(content)-1] == '\n' {
		count--
	}
	return count
}

func canonicalDiffLineIDs(currentLines []string, desiredLines []string) ([]int, []int) {
	byLine := make(map[string]int, len(currentLines)+len(desiredLines))
	nextID := 1
	index := func(lines []string) []int {
		ids := make([]int, len(lines))
		for lineIndex, line := range lines {
			id, present := byLine[line]
			if !present {
				id = nextID
				nextID++
				byLine[line] = id
			}
			ids[lineIndex] = id
		}
		return ids
	}
	return index(currentLines), index(desiredLines)
}

func lineDiff(
	currentLines []string,
	desiredLines []string,
	currentIDs []int,
	desiredIDs []int,
) []lineDiffOp {
	lcs := make([][]int, len(currentLines)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(desiredLines)+1)
	}
	for currentIndex := len(currentLines) - 1; currentIndex >= 0; currentIndex-- {
		for desiredIndex := len(desiredLines) - 1; desiredIndex >= 0; desiredIndex-- {
			if currentIDs[currentIndex] == desiredIDs[desiredIndex] {
				lcs[currentIndex][desiredIndex] = lcs[currentIndex+1][desiredIndex+1] + 1
				continue
			}
			lcs[currentIndex][desiredIndex] = max(lcs[currentIndex+1][desiredIndex], lcs[currentIndex][desiredIndex+1])
		}
	}

	ops := make([]lineDiffOp, 0, len(currentLines)+len(desiredLines))
	currentIndex := 0
	desiredIndex := 0
	for currentIndex < len(currentLines) && desiredIndex < len(desiredLines) {
		if currentIDs[currentIndex] == desiredIDs[desiredIndex] {
			ops = append(ops, lineDiffOp{prefix: ' ', line: currentLines[currentIndex]})
			currentIndex++
			desiredIndex++
			continue
		}
		if lcs[currentIndex+1][desiredIndex] >= lcs[currentIndex][desiredIndex+1] {
			ops = append(ops, lineDiffOp{prefix: '-', line: currentLines[currentIndex]})
			currentIndex++
			continue
		}
		ops = append(ops, lineDiffOp{prefix: '+', line: desiredLines[desiredIndex]})
		desiredIndex++
	}
	for currentIndex < len(currentLines) {
		ops = append(ops, lineDiffOp{prefix: '-', line: currentLines[currentIndex]})
		currentIndex++
	}
	for desiredIndex < len(desiredLines) {
		ops = append(ops, lineDiffOp{prefix: '+', line: desiredLines[desiredIndex]})
		desiredIndex++
	}

	return ops
}

func dryRunDiffOmissionMessage(reason applyworkflow.DryRunDiffOmissionReason) string {
	switch reason {
	case applyworkflow.DryRunDiffOmittedOperationLimit:
		return "text content differs; inline diff omitted because the operation input budget was exhausted"
	default:
		return "text content differs; inline diff omitted because the files are too large"
	}
}

func formatLineDiffOp(op lineDiffOp) []string {
	line := strings.TrimSuffix(op.line, "\n")
	lines := []string{string(op.prefix) + Escape(line)}
	if !strings.HasSuffix(op.line, "\n") {
		lines = append(lines, `\ No newline at end of file`)
	}

	return lines
}
