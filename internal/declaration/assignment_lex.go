package declaration

import (
	"strings"

	burnttoml "github.com/BurntSushi/toml"
)

const maximumTOMLValueDepth = 64

func readAssignmentKey(block string, start int, limit int) ([]string, int, bool) {
	index := start
	for {
		index = skipKeyWhitespace(block, index, limit)
		if index >= limit || block[index] == '#' {
			return nil, -1, false
		}
		next, ok := skipKeySegment(block, index, limit)
		if !ok {
			return nil, -1, false
		}
		index = skipKeyWhitespace(block, next, limit)
		if index >= limit {
			return nil, -1, false
		}
		if block[index] == '.' {
			index++
			continue
		}
		if block[index] != '=' {
			return nil, -1, false
		}
		path, pathOK := assignmentKeyPath(block[start:index])
		if !pathOK {
			return nil, -1, false
		}
		return path, index, true
	}
}

func skipKeySegment(block string, start int, limit int) (int, bool) {
	if start >= limit {
		return -1, false
	}
	switch block[start] {
	case '"', '\'':
		end, ok := skipTOMLString(block, start, limit, false)
		if !ok {
			return -1, false
		}
		return end, true
	default:
		index := start
		for index < limit && isBareKeyByte(block[index]) {
			index++
		}
		if index == start {
			return -1, false
		}
		return index, true
	}
}

func assignmentKeyPath(keyText string) ([]string, bool) {
	trimmed := strings.TrimSpace(keyText)
	if trimmed == "" {
		return nil, false
	}
	var decoded map[string]any
	if _, err := burnttoml.Decode(trimmed+" = 1\n", &decoded); err != nil {
		return nil, false
	}
	return nestedKeyPath(decoded)
}

func nestedKeyPath(decoded map[string]any) ([]string, bool) {
	if len(decoded) != 1 {
		return nil, false
	}
	for key, value := range decoded {
		nested, ok := value.(map[string]any)
		if !ok {
			return []string{key}, true
		}
		rest, restOK := nestedKeyPath(nested)
		if !restOK {
			return nil, false
		}
		return append([]string{key}, rest...), true
	}
	return nil, false
}

func sameRootKey(path []string, key string) bool {
	return len(path) == 1 && path[0] == key
}

func skipTOMLValue(block string, start int, limit int, depth int) (int, bool) {
	if depth > maximumTOMLValueDepth {
		return -1, false
	}
	index := skipSpaceAndComments(block, start, limit)
	if index >= limit {
		return -1, false
	}
	switch block[index] {
	case '"', '\'':
		return skipTOMLString(block, index, limit, true)
	case '[':
		return skipTOMLArray(block, index, limit, depth+1)
	case '{':
		return skipTOMLInlineTable(block, index, limit, depth+1)
	default:
		return skipTOMLBareValue(block, index, limit)
	}
}

func skipTOMLString(block string, start int, limit int, allowMultiline bool) (int, bool) {
	if start >= limit {
		return -1, false
	}
	quote := block[start]
	if quote != '"' && quote != '\'' {
		return -1, false
	}
	multiline := allowMultiline && start+2 < limit && block[start+1] == quote && block[start+2] == quote
	index := start + 1
	if multiline {
		index = start + 3
		if quote == '"' && index < limit && (block[index] == '\n' || (block[index] == '\r' && index+1 < limit && block[index+1] == '\n')) {
			if block[index] == '\r' {
				index += 2
			} else {
				index++
			}
		}
	}
	escaped := false
	for index < limit {
		char := block[index]
		if escaped {
			escaped = false
			index++
			continue
		}
		if quote == '"' && char == '\\' {
			escaped = true
			index++
			continue
		}
		if !multiline && (char == '\n' || char == '\r') {
			return -1, false
		}
		if char != quote {
			index++
			continue
		}
		if !multiline {
			return index + 1, true
		}
		run := 1
		for index+run < limit && block[index+run] == quote {
			run++
		}
		if run < 3 {
			index += run
			continue
		}
		// TOML allows 3-5 consecutive quotes at close (0-2 content quotes plus delimiter).
		if run > 5 {
			return -1, false
		}
		return index + run, true
	}
	return -1, false
}

func skipTOMLArray(block string, start int, limit int, depth int) (int, bool) {
	if start >= limit || block[start] != '[' {
		return -1, false
	}
	index := start + 1
	expectValue := true
	for index < limit {
		index = skipSpaceAndComments(block, index, limit)
		if index >= limit {
			return -1, false
		}
		if block[index] == ']' {
			return index + 1, true
		}
		if !expectValue {
			if block[index] != ',' {
				return -1, false
			}
			index++
			expectValue = true
			continue
		}
		next, ok := skipTOMLValue(block, index, limit, depth)
		if !ok {
			return -1, false
		}
		index = next
		expectValue = false
	}
	return -1, false
}

func skipTOMLInlineTable(block string, start int, limit int, depth int) (int, bool) {
	if start >= limit || block[start] != '{' {
		return -1, false
	}
	index := start + 1
	expectPair := true
	for index < limit {
		index = skipSpaceAndComments(block, index, limit)
		if index >= limit {
			return -1, false
		}
		if block[index] == '}' {
			return index + 1, true
		}
		if !expectPair {
			if block[index] != ',' {
				return -1, false
			}
			index++
			expectPair = true
			continue
		}
		_, equals, ok := readAssignmentKey(block, index, limit)
		if !ok {
			return -1, false
		}
		next, valueOK := skipTOMLValue(block, equals+1, limit, depth)
		if !valueOK {
			return -1, false
		}
		index = next
		expectPair = false
	}
	return -1, false
}

func skipTOMLBareValue(block string, start int, limit int) (int, bool) {
	if start >= limit || isBareValueDelimiter(block[start]) {
		return -1, false
	}
	index := start
	for index < limit && !isBareValueDelimiter(block[index]) {
		index++
	}
	if index == start {
		return -1, false
	}
	return index, true
}

func isBareKeyByte(char byte) bool {
	return char == '_' || char == '-' ||
		(char >= 'A' && char <= 'Z') ||
		(char >= 'a' && char <= 'z') ||
		(char >= '0' && char <= '9')
}

func isBareValueDelimiter(char byte) bool {
	switch char {
	case ' ', '\t', '\n', '\r', '#', ',', ']', '}':
		return true
	default:
		return false
	}
}

func skipKeyWhitespace(block string, offset int, limit int) int {
	index := offset
	for index < limit {
		switch block[index] {
		case ' ', '\t', '\n', '\r':
			index++
		default:
			return index
		}
	}
	return index
}

func skipHorizontalSpace(block string, offset int, limit int) int {
	index := offset
	for index < limit && (block[index] == ' ' || block[index] == '\t') {
		index++
	}
	return index
}

func skipSpaceAndComments(block string, offset int, limit int) int {
	index := offset
	for index < limit {
		switch block[index] {
		case ' ', '\t', '\n', '\r':
			index++
		case '#':
			index = skipLine(block, index)
		default:
			return index
		}
	}
	return index
}

func skipLine(block string, offset int) int {
	index := offset
	for index < len(block) && block[index] != '\n' {
		index++
	}
	if index < len(block) {
		index++
	}
	return index
}

func skipTrailingComment(block string, offset int) int {
	index := offset
	for index < len(block) && (block[index] == ' ' || block[index] == '\t') {
		index++
	}
	if index < len(block) && block[index] == '#' {
		end := skipLine(block, index)
		if end > 0 && block[end-1] == '\n' {
			end--
			if end > 0 && block[end-1] == '\r' {
				end--
			}
		}
		return end
	}
	return offset
}

func skipTrailingCommentAndNewline(block string, offset int) int {
	end := skipTrailingComment(block, offset)
	if end < len(block) && block[end] == '\r' {
		end++
	}
	if end < len(block) && block[end] == '\n' {
		end++
	}
	return end
}

func looksLikeTableHeader(block string, offset int) bool {
	lineEnd := skipLine(block, offset)
	trimmed := strings.TrimSpace(block[offset:lineEnd])
	_, ok := ParseTableHeader(trimmed)
	return ok
}

func blockNewline(block string) string {
	if strings.Contains(block, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func spliceRootAssignmentLine(block string, insertAt int, line string) string {
	newline := blockNewline(block)
	var builder strings.Builder
	builder.Grow(len(block) + len(line) + 2*len(newline))
	builder.WriteString(block[:insertAt])
	if insertAt > 0 && block[insertAt-1] != '\n' {
		builder.WriteString(newline)
	}
	builder.WriteString(line)
	builder.WriteString(newline)
	builder.WriteString(block[insertAt:])
	return builder.String()
}
