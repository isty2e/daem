package archguard

import (
	"fmt"
	"strings"
)

const (
	productStatusSectionHeading  = "## Product Status Labels"
	nonStatusSectionHeading      = "## Non-Status Vocabulary"
	targetMatrixSectionHeading   = "## Target Surface And Operation Matrix"
	routeSummaryHeadingSuffix    = " Route Summary"
	maximumTargetMatrixCellBytes = 80
)

func analyzeFeatureMatrixStatuses(publicMatrix string) []string {
	statuses, statusFindings := featureMatrixBulletVocabulary(
		publicMatrix,
		productStatusSectionHeading,
	)
	nonStatuses, nonStatusFindings := featureMatrixBulletVocabulary(
		publicMatrix,
		nonStatusSectionHeading,
	)
	findings := append(statusFindings, nonStatusFindings...)
	if len(statuses) == 0 || len(nonStatuses) == 0 {
		return findings
	}
	for status := range statuses {
		if _, overlaps := nonStatuses[status]; overlaps {
			findings = append(findings, fmt.Sprintf(
				"term %q appears in both product-status and non-status vocabularies",
				status,
			))
		}
	}

	sections := levelTwoFeatureMatrixSections(publicMatrix)
	targetSections := featureMatrixSectionsByHeading(sections, targetMatrixSectionHeading)
	if len(targetSections) == 0 {
		findings = append(findings, fmt.Sprintf("missing governed section %q", targetMatrixSectionHeading))
	} else if len(targetSections) > 1 {
		findings = append(findings, fmt.Sprintf("governed section %q appears %d times", targetMatrixSectionHeading, len(targetSections)))
	} else {
		tables := featureMatrixTablesWithHeader(targetSections[0].body, []string{
			"Surface", "Codex", "Claude Code", "OpenCode", "Pi", "Antigravity CLI",
		})
		if len(tables) == 0 {
			findings = append(findings, "target surface section has no governed target-status table")
		} else if len(tables) > 1 {
			findings = append(findings, fmt.Sprintf("target surface section has %d governed target-status tables", len(tables)))
		} else {
			findings = append(findings, validateFeatureMatrixStatusColumns(
				"target surface matrix",
				tables[0],
				[]int{1, 2, 3, 4, 5},
				statuses,
				nonStatuses,
			)...)
			findings = append(findings, validateFeatureMatrixCellWidths(
				"target surface matrix",
				tables[0],
				[]int{1, 2, 3, 4, 5},
				maximumTargetMatrixCellBytes,
			)...)
		}
	}

	routeTableCount := 0
	routeHeader := []string{"Route family", "Current product state", "What that means"}
	for _, section := range sections {
		tables := featureMatrixTablesWithHeader(section.body, routeHeader)
		isRouteSummary := strings.HasSuffix(section.heading, routeSummaryHeadingSuffix)
		if len(tables) == 0 && !isRouteSummary {
			continue
		}
		if len(tables) == 0 {
			findings = append(findings, fmt.Sprintf("section %q has no governed route-state table", section.heading))
			continue
		}
		if !isRouteSummary {
			findings = append(findings, fmt.Sprintf("governed route-state table under unexpected heading %q", section.heading))
		}
		if len(tables) > 1 {
			findings = append(findings, fmt.Sprintf("section %q has %d governed route-state tables", section.heading, len(tables)))
			continue
		}
		routeTableCount++
		findings = append(findings, validateFeatureMatrixStatusColumns(
			section.heading,
			tables[0],
			[]int{1},
			statuses,
			nonStatuses,
		)...)
	}
	if routeTableCount == 0 {
		findings = append(findings, "public feature matrix has no governed route summary section")
	}
	return findings
}

func validateFeatureMatrixCellWidths(label string, table featureMatrixTable, columns []int, maximum int) []string {
	var findings []string
	for rowIndex, row := range table.rows {
		for _, column := range columns {
			if column >= len(row) {
				continue
			}
			if len(row[column]) > maximum {
				findings = append(findings, fmt.Sprintf(
					"%s row %d column %d exceeds %d-byte compact-cell limit",
					label,
					rowIndex+1,
					column+1,
					maximum,
				))
			}
		}
	}
	return findings
}

func featureMatrixBulletVocabulary(content string, heading string) (map[string]struct{}, []string) {
	sections := featureMatrixSectionsByHeading(levelTwoFeatureMatrixSections(content), heading)
	if len(sections) == 0 {
		return nil, []string{fmt.Sprintf("public feature matrix is missing section %q", heading)}
	}
	if len(sections) > 1 {
		return nil, []string{fmt.Sprintf("public feature matrix section %q appears %d times", heading, len(sections))}
	}

	values := make(map[string]struct{})
	var findings []string
	for line := range strings.SplitSeq(sections[0].body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		if !strings.HasPrefix(trimmed, "- `") {
			findings = append(findings, fmt.Sprintf(
				"public feature matrix section %q has malformed vocabulary bullet %q",
				heading,
				trimmed,
			))
			continue
		}

		labelStart := len("- `")
		labelEnd := strings.IndexByte(trimmed[labelStart:], '`')
		if labelEnd <= 0 {
			findings = append(findings, fmt.Sprintf(
				"public feature matrix section %q has malformed vocabulary bullet %q",
				heading,
				trimmed,
			))
			continue
		}
		labelEnd += labelStart
		if !strings.HasPrefix(trimmed[labelEnd+1:], ":") {
			findings = append(findings, fmt.Sprintf(
				"public feature matrix section %q has malformed vocabulary bullet %q",
				heading,
				trimmed,
			))
			continue
		}

		label := trimmed[labelStart:labelEnd]
		if _, duplicate := values[label]; duplicate {
			findings = append(findings, fmt.Sprintf(
				"public feature matrix section %q repeats vocabulary term %q",
				heading,
				label,
			))
		}
		values[label] = struct{}{}
	}
	if len(values) == 0 {
		findings = append(findings, fmt.Sprintf(
			"public feature matrix section %q has no vocabulary bullets",
			heading,
		))
	}
	return values, findings
}

func validateFeatureMatrixStatusColumns(
	label string,
	table featureMatrixTable,
	columns []int,
	statuses map[string]struct{},
	nonStatuses map[string]struct{},
) []string {
	findings := append([]string(nil), table.findings...)
	if len(table.rows) == 0 {
		return append(findings, fmt.Sprintf("%s has no data rows", label))
	}
	for rowIndex, row := range table.rows {
		for _, column := range columns {
			if column >= len(row) {
				findings = append(findings, fmt.Sprintf("%s row %d is missing status column %d", label, rowIndex+1, column+1))
				continue
			}
			cell := row[column]
			statusCount := 0
			for _, token := range inlineCodeTokens(cell) {
				if _, ok := statuses[token]; ok {
					statusCount++
				}
			}
			if statusCount == 0 {
				findings = append(findings, fmt.Sprintf("%s row %d column %q has no canonical product status", label, rowIndex+1, table.header[column]))
			}
			for term := range nonStatuses {
				if strings.Contains(cell, term) {
					findings = append(findings, fmt.Sprintf("%s row %d column %q contains non-status term %q", label, rowIndex+1, table.header[column], term))
				}
			}
		}
	}
	return findings
}
