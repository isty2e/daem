package clipresent

import (
	"bytes"
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
}

// DryRunDiffsFrom projects canonical apply diffs into the stable diff contract.
func DryRunDiffsFrom(diffs []applyworkflow.DryRunDiff) []DryRunDiff {
	result := make([]DryRunDiff, 0, len(diffs))
	for _, diff := range diffs {
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
		})
	}
	return result
}

type lineDiffOp struct {
	prefix byte
	line   string
}

const maxInlineDiffCells = 250_000

func PrintDryRunDiffs(
	output io.Writer,
	diffs []DryRunDiff,
) {
	fmt.Fprintf(output, "diff: %d files\n", len(diffs))
	for _, diff := range diffs {
		targetLabel := "targets=" + strings.Join(diff.Targets, ",")
		if len(diff.Targets) == 1 {
			targetLabel = "target=" + diff.Targets[0]
		}
		fmt.Fprintf(
			output,
			"diff resource=%q %s scope=%s destination=%q\n",
			diff.ResourceID,
			targetLabel,
			diff.Scope,
			diff.Destination,
		)
		if !isTextDiffContent(diff.CurrentContent) || !isTextDiffContent(diff.DesiredContent) {
			fmt.Fprintln(output, "binary content differs; textual diff omitted")
			continue
		}
		for _, line := range FormatTextDiff(diff.CurrentLabel, diff.CurrentContent, diff.DesiredLabel, diff.DesiredContent) {
			fmt.Fprintln(output, line)
		}
	}
}

func isTextDiffContent(content []byte) bool {
	return !bytes.Contains(content, []byte{0}) && utf8.Valid(content)
}

func FormatTextDiff(currentLabel string, currentContent []byte, desiredLabel string, desiredContent []byte) []string {
	lines := []string{
		"--- " + currentLabel,
		"+++ " + desiredLabel,
		"@@",
	}
	currentLines := splitDiffLines(currentContent)
	desiredLines := splitDiffLines(desiredContent)
	if len(currentLines) > 0 && len(desiredLines) > maxInlineDiffCells/len(currentLines) {
		return append(lines, "text content differs; inline diff omitted because the files are too large")
	}
	for _, op := range lineDiff(currentLines, desiredLines) {
		lines = append(lines, formatLineDiffOp(op)...)
	}

	return lines
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

func lineDiff(currentLines []string, desiredLines []string) []lineDiffOp {
	lcs := make([][]int, len(currentLines)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(desiredLines)+1)
	}
	for currentIndex := len(currentLines) - 1; currentIndex >= 0; currentIndex-- {
		for desiredIndex := len(desiredLines) - 1; desiredIndex >= 0; desiredIndex-- {
			if currentLines[currentIndex] == desiredLines[desiredIndex] {
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
		if currentLines[currentIndex] == desiredLines[desiredIndex] {
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

func formatLineDiffOp(op lineDiffOp) []string {
	line := strings.TrimSuffix(op.line, "\n")
	line = strings.ReplaceAll(line, "\r", "\\r")
	lines := []string{string(op.prefix) + line}
	if !strings.HasSuffix(op.line, "\n") {
		lines = append(lines, `\ No newline at end of file`)
	}

	return lines
}
