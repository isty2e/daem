package archguard

import (
	"fmt"
	"sort"
	"strings"
)

type findingIdentity struct {
	Rule        string
	PackagePath string
	ImportPath  string
	Path        string
}

// FormatAnalysisReport renders a deterministic report with all guardrail classes.
func FormatAnalysisReport(report Report) string {
	var builder strings.Builder
	if len(report.Violations) == 0 {
		builder.WriteString("archguard: no topology violations reported\n")
	} else {
		writeFindings(&builder, "topology violation", report.Violations)
	}

	if len(report.DensityReviewRequirements) != 0 {
		writeFindings(&builder, "density review required", report.DensityReviewRequirements)
	}

	if len(report.DensityWarnings) != 0 {
		writeFindings(&builder, "density warning", report.DensityWarnings)
	}

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

func dedupRawFindings(findings []rawFinding) []rawFinding {
	seen := make(map[findingIdentity]bool, len(findings))
	var deduped []rawFinding
	for _, finding := range findings {
		key := findingKey(finding.finding)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, finding)
	}
	return deduped
}

func findingKey(finding GuardrailFinding) findingIdentity {
	return findingIdentity{
		Rule:        finding.Rule,
		PackagePath: finding.PackagePath,
		ImportPath:  finding.ImportPath,
		Path:        finding.Path,
	}
}

func sortedRawFindings(findings []rawFinding) []rawFinding {
	copied := append([]rawFinding(nil), findings...)
	sort.Slice(copied, func(i int, j int) bool {
		return lessFinding(copied[i].finding, copied[j].finding)
	})
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

func sortedPackageDensities(densities []PackageDensity) []PackageDensity {
	copied := append([]PackageDensity(nil), densities...)
	sort.Slice(copied, func(i int, j int) bool {
		return copied[i].PackagePath < copied[j].PackagePath
	})
	return copied
}
