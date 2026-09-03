package archguard

import (
	"fmt"
	"sort"
	"strings"
)

// FormatAnalysisReport renders a deterministic blocking topology report.
// Shadow findings are omitted so CI failure logs cannot be mistaken for
// report-only compiler-shadow evidence.
func FormatAnalysisReport(report Report) string {
	return FormatReport(report.Violations)
}

// FormatShadowReport renders deterministic report-only compiler-shadow findings.
func FormatShadowReport(report Report) string {
	findings := sortedFindings(report.Shadow)
	if len(findings) == 0 {
		return "archguard: no compiler-shadow findings reported\n"
	}
	var builder strings.Builder
	writeFindings(&builder, "compiler-shadow finding", findings)
	return builder.String()
}

// FormatReport renders a deterministic text report for test logs and tickets.
func FormatReport(violations []GuardrailFinding) string {
	violations = sortedViolations(violations)
	if len(violations) == 0 {
		return "archguard: no topology violations reported\n"
	}

	var builder strings.Builder
	writeFindings(&builder, "topology violation", violations)
	return builder.String()
}

func writeFindings(builder *strings.Builder, label string, findings []GuardrailFinding) {
	fmt.Fprintf(builder, "archguard: %d %s(s)\n", len(findings), label)
	for _, finding := range sortedFindings(findings) {
		switch {
		case finding.ImportPath != "":
			fmt.Fprintf(builder, "- %s: %s -> %s", finding.Rule, finding.PackagePath, finding.ImportPath)
		case finding.Path != "":
			fmt.Fprintf(builder, "- %s: %s", finding.Rule, finding.Path)
		default:
			fmt.Fprintf(builder, "- %s: %s", finding.Rule, finding.PackagePath)
		}
		if finding.Reason != "" {
			fmt.Fprintf(builder, " reason=%q", finding.Reason)
		}
		if finding.Detail != "" {
			fmt.Fprintf(builder, " detail=%q", finding.Detail)
		}
		builder.WriteByte('\n')
	}
}

func sortedRecords(records []PackageRecord) []PackageRecord {
	copied := append([]PackageRecord(nil), records...)
	sort.Slice(copied, func(i int, j int) bool {
		return copied[i].ImportPath < copied[j].ImportPath
	})
	return copied
}

func sortedStrings(values []string) []string {
	copied := append([]string(nil), values...)
	sort.Strings(copied)
	return copied
}

func dedupViolations(violations []GuardrailFinding) []GuardrailFinding {
	return dedupFindings(violations)
}

func sortedViolations(violations []GuardrailFinding) []GuardrailFinding {
	return sortedFindings(violations)
}

func dedupFindings(findings []GuardrailFinding) []GuardrailFinding {
	seen := make(map[GuardrailFinding]bool, len(findings))
	var deduped []GuardrailFinding
	for _, finding := range findings {
		if seen[finding] {
			continue
		}
		seen[finding] = true
		deduped = append(deduped, finding)
	}
	return deduped
}

func sortedFindings(findings []GuardrailFinding) []GuardrailFinding {
	copied := append([]GuardrailFinding(nil), findings...)
	sort.Slice(copied, func(i int, j int) bool {
		return lessFinding(copied[i], copied[j])
	})
	return copied
}

func lessFinding(left GuardrailFinding, right GuardrailFinding) bool {
	switch {
	case left.Rule != right.Rule:
		return left.Rule < right.Rule
	case left.PackagePath != right.PackagePath:
		return left.PackagePath < right.PackagePath
	case left.ImportPath != right.ImportPath:
		return left.ImportPath < right.ImportPath
	case left.Path != right.Path:
		return left.Path < right.Path
	case left.Reason != right.Reason:
		return left.Reason < right.Reason
	default:
		return left.Detail < right.Detail
	}
}
