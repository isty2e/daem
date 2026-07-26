package archguard

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type markdownDocument struct {
	path             string
	anchors          map[string]struct{}
	links            []markdownLink
	deprecatedCLIUse []deprecatedCLILine
}

type deprecatedCLILine struct {
	line   int
	syntax string
}

type markdownLink struct {
	line        int
	destination string
}

type markdownFence struct {
	marker byte
	width  int
}

func loadMarkdownDocuments(root string) ([]markdownDocument, error) {
	var documents []markdownDocument
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && ignoredDocumentationDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		documents = append(documents, parseMarkdownDocument(filepath.ToSlash(relativePath), string(content)))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].path < documents[j].path })
	return documents, nil
}

func ignoredDocumentationDirectory(name string) bool {
	switch name {
	case ".git", ".kata", ".local", ".pi-subagents", ".tickets", "vendor":
		return true
	default:
		return false
	}
}

func parseMarkdownDocument(path string, content string) markdownDocument {
	document := markdownDocument{path: path, anchors: make(map[string]struct{})}
	anchorCounts := make(map[string]int)
	checkDeprecatedCLI := isUserDocumentation(path)
	var fence *markdownFence

	for index, line := range strings.Split(content, "\n") {
		lineNumber := index + 1
		if marker, width, ok := markdownFenceMarker(line); ok {
			if fence == nil {
				fence = &markdownFence{marker: marker, width: width}
			} else if fence.marker == marker && width >= fence.width {
				fence = nil
			}
			continue
		}

		if fence != nil {
			if checkDeprecatedCLI {
				if syntax, ok := deprecatedCUXSyntax(line); ok {
					document.deprecatedCLIUse = append(document.deprecatedCLIUse, deprecatedCLILine{line: lineNumber, syntax: syntax})
				}
			}
			continue
		}

		if checkDeprecatedCLI {
			if syntax, ok := deprecatedCUXSyntax(line); ok {
				document.deprecatedCLIUse = append(document.deprecatedCLIUse, deprecatedCLILine{line: lineNumber, syntax: syntax})
			}
		}

		if heading, ok := markdownATXHeading(line); ok {
			base := githubHeadingSlug(heading)
			if base != "" {
				slug := base
				if count := anchorCounts[base]; count != 0 {
					slug = fmt.Sprintf("%s-%d", base, count)
				}
				anchorCounts[base]++
				document.anchors[slug] = struct{}{}
			}
		}

		inlineCode := markdownInlineCodeSpans(line)
		masked := maskInlineCode(line, inlineCode)
		for _, destination := range markdownInlineDestinations(masked) {
			document.links = append(document.links, markdownLink{line: lineNumber, destination: destination})
		}
		if destination, ok := markdownReferenceDestination(masked); ok {
			document.links = append(document.links, markdownLink{line: lineNumber, destination: destination})
		}
	}
	return document
}

func deprecatedCUXSyntax(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	for _, candidate := range []struct {
		flag    string
		command string
	}{
		{flag: "--lockfile", command: "daem "},
		{flag: "--output", command: "daem import"},
		{flag: "--attempt-delegates", command: "daem apply"},
		{flag: "--inventory", command: "daem status"},
		{flag: "--marketplace", command: "daem add extension"},
		{flag: "--host-source", command: "daem add extension"},
		{flag: "--target-override", command: "daem add hook"},
		{flag: "--status-message", command: "daem add hook"},
	} {
		if strings.HasPrefix(trimmed, "| `"+candidate.flag) ||
			(strings.Contains(line, candidate.command) && strings.Contains(line, candidate.flag)) {
			return candidate.flag, true
		}
	}

	for _, command := range []string{"daem init", "daem import", "daem add ", "daem remove "} {
		if strings.Contains(line, command) && strings.Contains(line, "--yes") {
			return command + " ... --yes", true
		}
	}
	if strings.Contains(line, "daem list --") {
		return "daem list without a resource child", true
	}
	if strings.Contains(line, "daem add skill-group") && strings.Contains(line, "--name") {
		return "daem add skill-group ... --name", true
	}
	if strings.Contains(line, "daem add hook") && (strings.Contains(line, "--event") || strings.Contains(line, "--command")) {
		return "daem add hook with flag-owned operands", true
	}
	if strings.Contains(line, "daem add mcp-server") && strings.Contains(line, "--command") {
		return "daem add mcp-server ... --command", true
	}

	if strings.Contains(line, "daem ") {
		fields := strings.Fields(line)
		for index, field := range fields {
			if (field == "--target" || field == "--scope") && index+1 < len(fields) && strings.Contains(fields[index+1], ",") {
				return field + " comma list", true
			}
		}
	}
	return "", false
}

func markdownFenceMarker(line string) (byte, int, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 {
		return 0, 0, false
	}
	marker := trimmed[0]
	if marker != '`' && marker != '~' {
		return 0, 0, false
	}
	width := 0
	for width < len(trimmed) && trimmed[width] == marker {
		width++
	}
	return marker, width, width >= 3
}

func markdownATXHeading(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return "", false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || (level < len(trimmed) && trimmed[level] != ' ' && trimmed[level] != '\t') {
		return "", false
	}
	heading := strings.TrimSpace(trimmed[level:])
	for strings.HasSuffix(heading, "#") {
		withoutHash := strings.TrimSpace(strings.TrimSuffix(heading, "#"))
		if withoutHash == heading[:len(heading)-1] {
			break
		}
		heading = withoutHash
	}
	return heading, true
}

func githubHeadingSlug(heading string) string {
	var slug strings.Builder
	pendingSpace := false
	for _, character := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(character) || unicode.IsNumber(character) || character == '_' || character == '-':
			if pendingSpace && slug.Len() != 0 {
				slug.WriteByte('-')
			}
			pendingSpace = false
			slug.WriteRune(character)
		case unicode.IsSpace(character):
			pendingSpace = true
		}
	}
	return slug.String()
}

type markdownInlineCodeSpan struct {
	start   int
	end     int
	content string
	closed  bool
}

func markdownInlineCodeSpans(line string) []markdownInlineCodeSpan {
	var spans []markdownInlineCodeSpan
	for index := 0; index < len(line); {
		if line[index] != '`' {
			index++
			continue
		}
		width := 1
		for index+width < len(line) && line[index+width] == '`' {
			width++
		}
		closing := strings.Index(line[index+width:], strings.Repeat("`", width))
		if closing < 0 {
			spans = append(spans, markdownInlineCodeSpan{start: index, end: len(line)})
			break
		}
		contentStart := index + width
		contentEnd := contentStart + closing
		end := contentEnd + width
		spans = append(spans, markdownInlineCodeSpan{
			start:   index,
			end:     end,
			content: line[contentStart:contentEnd],
			closed:  true,
		})
		index = end
	}
	return spans
}

func maskInlineCode(line string, spans []markdownInlineCodeSpan) string {
	masked := []byte(line)
	for _, span := range spans {
		for cursor := span.start; cursor < span.end; cursor++ {
			masked[cursor] = ' '
		}
	}
	return string(masked)
}

func markdownInlineDestinations(line string) []string {
	var destinations []string
	for searchFrom := 0; searchFrom < len(line); {
		offset := strings.Index(line[searchFrom:], "](")
		if offset < 0 {
			break
		}
		start := searchFrom + offset + 2
		depth := 1
		closing := -1
		for end := start; end < len(line); end++ {
			switch line[end] {
			case '\\':
				end++
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					closing = end
				}
			}
			if closing >= 0 {
				break
			}
		}
		if closing < 0 {
			searchFrom = start
			continue
		}
		if destination := firstMarkdownDestination(line[start:closing]); destination != "" {
			destinations = append(destinations, unescapeMarkdownDestination(destination))
		}
		searchFrom = closing + 1
	}
	return destinations
}

func markdownReferenceDestination(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	separator := strings.Index(trimmed, "]:")
	if separator <= 1 {
		return "", false
	}
	destination := firstMarkdownDestination(trimmed[separator+2:])
	return unescapeMarkdownDestination(destination), destination != ""
}

func unescapeMarkdownDestination(destination string) string {
	var unescaped strings.Builder
	for index := 0; index < len(destination); index++ {
		if destination[index] == '\\' && index+1 < len(destination) && isASCIIPunctuation(destination[index+1]) {
			index++
		}
		unescaped.WriteByte(destination[index])
	}
	return unescaped.String()
}

func isASCIIPunctuation(character byte) bool {
	return character >= '!' && character <= '~' &&
		!('0' <= character && character <= '9') &&
		!('A' <= character && character <= 'Z') &&
		!('a' <= character && character <= 'z')
}

func firstMarkdownDestination(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if trimmed[0] == '<' {
		if end := strings.IndexByte(trimmed, '>'); end > 1 {
			return trimmed[1:end]
		}
		return ""
	}
	for index, character := range trimmed {
		if unicode.IsSpace(character) {
			return trimmed[:index]
		}
	}
	return trimmed
}
