package archguard

import (
	"fmt"
	"slices"
	"strings"
)

type featureMatrixTable struct {
	header   []string
	rows     [][]string
	findings []string
}

type featureMatrixSection struct {
	heading string
	body    string
}

func levelTwoFeatureMatrixSections(content string) []featureMatrixSection {
	lines := visibleFeatureMatrixLines(content)
	var sections []featureMatrixSection
	for index := 0; index < len(lines); {
		heading, ok := featureMatrixLevelTwoHeading(lines[index])
		if !ok {
			index++
			continue
		}
		start := index + 1
		end := start
		for end < len(lines) {
			if _, ok := featureMatrixLevelTwoHeading(lines[end]); ok {
				break
			}
			end++
		}
		sections = append(sections, featureMatrixSection{heading: heading, body: strings.Join(lines[start:end], "\n")})
		index = end
	}
	return sections
}

func visibleFeatureMatrixLines(content string) []string {
	lines := strings.Split(content, "\n")
	var fence *markdownFence
	inComment := false
	for index, line := range lines {
		if fence != nil {
			if marker, width, ok := markdownFenceMarker(line); ok && fence.marker == marker && width >= fence.width {
				fence = nil
			}
			lines[index] = ""
			continue
		}

		line = featureMatrixWithoutHTMLComments(line, &inComment)
		if marker, width, ok := markdownFenceMarker(line); ok {
			fence = &markdownFence{marker: marker, width: width}
			lines[index] = ""
			continue
		}
		lines[index] = line
	}
	return lines
}

func featureMatrixWithoutHTMLComments(line string, inComment *bool) string {
	var visible strings.Builder
	remaining := line
	for {
		if *inComment {
			end := strings.Index(remaining, "-->")
			if end < 0 {
				return visible.String()
			}
			remaining = remaining[end+len("-->"):]
			*inComment = false
		}

		start := strings.Index(remaining, "<!--")
		if start < 0 {
			visible.WriteString(remaining)
			return visible.String()
		}
		visible.WriteString(remaining[:start])
		remaining = remaining[start+len("<!--"):]
		*inComment = true
	}
}

func featureMatrixLevelTwoHeading(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || !strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") {
		return "", false
	}
	return strings.TrimSpace(trimmed), true
}

func featureMatrixSectionsByHeading(sections []featureMatrixSection, heading string) []featureMatrixSection {
	var matches []featureMatrixSection
	for _, section := range sections {
		if section.heading == heading {
			matches = append(matches, section)
		}
	}
	return matches
}

func featureMatrixTablesWithHeader(content string, header []string) []featureMatrixTable {
	var matches []featureMatrixTable
	for _, table := range parseFeatureMatrixTables(content) {
		if slices.Equal(table.header, header) {
			matches = append(matches, table)
		}
	}
	return matches
}

func parseFeatureMatrixTables(content string) []featureMatrixTable {
	lines := strings.Split(content, "\n")
	var tables []featureMatrixTable
	for index := 0; index+1 < len(lines); index++ {
		header, headerOK := splitFeatureMatrixTableRow(lines[index])
		separator, separatorOK := splitFeatureMatrixTableRow(lines[index+1])
		if !headerOK || !separatorOK || len(header) != len(separator) || !isFeatureMatrixSeparator(separator) {
			continue
		}
		table := featureMatrixTable{header: header}
		index += 2
		for ; index < len(lines); index++ {
			trimmed := strings.TrimSpace(lines[index])
			if !strings.HasPrefix(trimmed, "|") {
				break
			}
			row, ok := splitFeatureMatrixTableRow(lines[index])
			if !ok {
				table.findings = append(table.findings, fmt.Sprintf(
					"malformed Markdown table row %d: cannot parse %q",
					index+1,
					trimmed,
				))
				continue
			}
			if len(row) != len(header) {
				table.findings = append(table.findings, fmt.Sprintf(
					"malformed Markdown table row %d: got %d cells, want %d",
					index+1,
					len(row),
					len(header),
				))
				continue
			}
			table.rows = append(table.rows, row)
		}
		tables = append(tables, table)
		index--
	}
	return tables
}

func splitFeatureMatrixTableRow(line string) ([]string, bool) {
	leftTrimmed := strings.TrimLeft(line, " ")
	if len(line)-len(leftTrimmed) > 3 {
		return nil, false
	}
	trimmed := strings.TrimSpace(leftTrimmed)
	if len(trimmed) < 2 || trimmed[0] != '|' || trimmed[len(trimmed)-1] != '|' {
		return nil, false
	}
	inner := trimmed[1 : len(trimmed)-1]
	var cells []string
	start := 0
	codeWidth := 0
	for index := 0; index < len(inner); {
		switch inner[index] {
		case '\\':
			index += 2
			continue
		case '`':
			width := 1
			for index+width < len(inner) && inner[index+width] == '`' {
				width++
			}
			if codeWidth == 0 {
				codeWidth = width
			} else if codeWidth == width {
				codeWidth = 0
			}
			index += width
			continue
		case '|':
			if codeWidth == 0 {
				cells = append(cells, strings.TrimSpace(inner[start:index]))
				start = index + 1
			}
		}
		index++
	}
	if codeWidth != 0 {
		return nil, false
	}
	cells = append(cells, strings.TrimSpace(inner[start:]))
	return cells, true
}

func isFeatureMatrixSeparator(cells []string) bool {
	for _, cell := range cells {
		trimmed := strings.Trim(cell, ":")
		if len(trimmed) < 3 || strings.Trim(trimmed, "-") != "" {
			return false
		}
	}
	return true
}

func inlineCodeTokens(value string) []string {
	var tokens []string
	for index := 0; index < len(value); {
		if value[index] != '`' {
			index++
			continue
		}
		width := 1
		for index+width < len(value) && value[index+width] == '`' {
			width++
		}
		marker := strings.Repeat("`", width)
		contentStart := index + width
		closing := strings.Index(value[contentStart:], marker)
		if closing < 0 {
			break
		}
		tokens = append(tokens, value[contentStart:contentStart+closing])
		index = contentStart + closing + width
	}
	return tokens
}
