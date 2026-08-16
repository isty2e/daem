package declaration

import (
	"fmt"
	"strings"

	burnttoml "github.com/BurntSushi/toml"
)

type assignmentLookup int

const (
	assignmentMissing assignmentLookup = iota
	assignmentFound
	assignmentMalformed
)

type documentAssignment struct {
	keyStart int
	valueEnd int
}

// ReplaceRootAssignment rewrites a root-table assignment using parser-derived
// key-through-value bounds. ok is false when the key is absent. A malformed
// present assignment returns an error and must not be mutated.
func ReplaceRootAssignment(block string, key string, renderedValue string) (string, bool, error) {
	assignment, status := findRootAssignment(block, key)
	switch status {
	case assignmentMissing:
		return block, false, nil
	case assignmentMalformed:
		return block, false, fmt.Errorf("root assignment %q is not a complete TOML value", key)
	default:
		updated := block[:assignment.keyStart] + key + " = " + renderedValue + block[assignment.valueEnd:]
		return updated, true, nil
	}
}

// RemoveRootAssignment deletes a root-table assignment, including its indent,
// trailing comment, and terminating newline. Missing keys return false; a
// malformed present assignment returns an error.
func RemoveRootAssignment(block string, key string) (string, bool, error) {
	assignment, status := findRootAssignment(block, key)
	switch status {
	case assignmentMissing:
		return block, false, nil
	case assignmentMalformed:
		return block, false, fmt.Errorf("root assignment %q is not a complete TOML value", key)
	default:
		start := assignment.keyStart
		for start > 0 && block[start-1] != '\n' {
			start--
		}
		end := skipTrailingComment(block, assignment.valueEnd)
		if end < len(block) && block[end] == '\r' {
			end++
		}
		if end < len(block) && block[end] == '\n' {
			end++
		}
		return block[:start] + block[end:], true, nil
	}
}

func findRootAssignment(block string, key string) (documentAssignment, assignmentLookup) {
	rootEnd := rootTableEnd(block)
	offset := 0
	seenRootHeader := false
	for offset < rootEnd {
		offset = skipSpaceAndComments(block, offset, rootEnd)
		if offset >= rootEnd {
			break
		}
		if looksLikeTableHeader(block, offset) {
			if !seenRootHeader {
				seenRootHeader = true
				offset = skipLine(block, offset)
				continue
			}
			break
		}
		keyStart := offset
		canonical, _, ok := readAssignmentKey(block, offset, rootEnd)
		if !ok {
			return documentAssignment{}, assignmentMalformed
		}
		if canonical != key {
			next := shortestAssignmentEnd(block, keyStart, rootEnd, canonical)
			if next < 0 {
				return documentAssignment{}, assignmentMalformed
			}
			offset = skipTrailingComment(block, next)
			if offset < len(block) && block[offset] == '\r' {
				offset++
			}
			if offset < len(block) && block[offset] == '\n' {
				offset++
			}
			continue
		}
		valueEnd := shortestAssignmentEnd(block, keyStart, rootEnd, key)
		if valueEnd < 0 {
			return documentAssignment{}, assignmentMalformed
		}
		return documentAssignment{keyStart: keyStart, valueEnd: valueEnd}, assignmentFound
	}
	return documentAssignment{}, assignmentMissing
}

func rootTableEnd(block string) int {
	offset := 0
	seenRoot := false
	for offset < len(block) {
		offset = skipSpaceAndComments(block, offset, len(block))
		if offset >= len(block) {
			break
		}
		if !looksLikeTableHeader(block, offset) {
			offset = skipLine(block, offset)
			continue
		}
		if !seenRoot {
			seenRoot = true
			offset = skipLine(block, offset)
			continue
		}
		return offset
	}
	return len(block)
}

func readAssignmentKey(block string, start int, limit int) (string, int, bool) {
	equals := -1
	inString := byte(0)
	escaped := false
	for index := start; index < limit; index++ {
		char := block[index]
		if inString != 0 {
			if escaped {
				escaped = false
				continue
			}
			if inString == '"' && char == '\\' {
				escaped = true
				continue
			}
			if char == inString {
				inString = 0
			}
			continue
		}
		switch char {
		case '"', '\'':
			inString = char
		case '#', '\n':
			return "", -1, false
		case '=':
			equals = index
		}
		if equals >= 0 {
			break
		}
	}
	if equals < 0 {
		return "", -1, false
	}
	keyText := strings.TrimSpace(block[start:equals])
	if keyText == "" {
		return "", -1, false
	}
	var decoded map[string]any
	if _, err := burnttoml.Decode(keyText+" = 1\n", &decoded); err != nil {
		return "", -1, false
	}
	if len(decoded) != 1 {
		return "", -1, false
	}
	for canonical, value := range decoded {
		if _, nested := value.(map[string]any); nested {
			return "", -1, false
		}
		return canonical, equals, true
	}
	return "", -1, false
}

func shortestAssignmentEnd(block string, keyStart int, limit int, key string) int {
	for end := keyStart + 1; end <= limit; end++ {
		snippet := block[keyStart:end]
		var decoded map[string]any
		_, err := burnttoml.Decode(snippet, &decoded)
		if err != nil {
			continue
		}
		if len(decoded) != 1 {
			continue
		}
		if _, ok := decoded[key]; !ok {
			continue
		}
		return end
	}
	return -1
}

func looksLikeTableHeader(block string, offset int) bool {
	lineEnd := skipLine(block, offset)
	trimmed := strings.TrimSpace(block[offset:lineEnd])
	_, ok := ParseTableHeader(trimmed)
	return ok
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
