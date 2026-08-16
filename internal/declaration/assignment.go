package declaration

import (
	"fmt"
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
		end := skipTrailingCommentAndNewline(block, assignment.valueEnd)
		return block[:start] + block[end:], true, nil
	}
}

// InsertRootAssignment writes a root-table assignment before the first nested
// table header, or at the end of the block. It uses the block's existing line
// ending and inserts a separator when the preceding content has none.
func InsertRootAssignment(block string, key string, renderedValue string) (string, error) {
	_, status := findRootAssignment(block, key)
	switch status {
	case assignmentFound:
		return "", fmt.Errorf("root assignment %q already exists", key)
	case assignmentMalformed:
		return "", fmt.Errorf("root assignment %q is not a complete TOML value", key)
	}
	insertAt, ok := rootInsertOffset(block)
	if !ok {
		return "", fmt.Errorf("root assignment %q is not a complete TOML value", key)
	}
	return spliceRootAssignmentLine(block, insertAt, key+" = "+renderedValue), nil
}

func findRootAssignment(block string, key string) (documentAssignment, assignmentLookup) {
	var found documentAssignment
	status := assignmentMissing
	_, malformed := walkRootTable(block, func(keyStart int, valueEnd int, path []string) bool {
		if !sameRootKey(path, key) {
			return false
		}
		found = documentAssignment{keyStart: keyStart, valueEnd: valueEnd}
		status = assignmentFound
		return true
	})
	if malformed {
		return documentAssignment{}, assignmentMalformed
	}
	return found, status
}

func rootInsertOffset(block string) (int, bool) {
	offset, malformed := walkRootTable(block, func(int, int, []string) bool { return false })
	if malformed {
		return 0, false
	}
	return offset, true
}

func walkRootTable(block string, visit func(keyStart int, valueEnd int, path []string) bool) (int, bool) {
	offset := 0
	seenRootHeader := false
	for offset < len(block) {
		offset = skipSpaceAndComments(block, offset, len(block))
		if offset >= len(block) {
			return len(block), false
		}
		if looksLikeTableHeader(block, offset) {
			if !seenRootHeader {
				seenRootHeader = true
				offset = skipLine(block, offset)
				continue
			}
			return offset, false
		}
		keyStart := offset
		path, equals, ok := readAssignmentKey(block, offset, len(block))
		if !ok {
			return offset, true
		}
		valueEnd, valueOK := skipTOMLValue(block, equals+1, len(block), 0)
		if !valueOK {
			return offset, true
		}
		if visit(keyStart, valueEnd, path) {
			return valueEnd, false
		}
		offset = skipTrailingCommentAndNewline(block, valueEnd)
	}
	return len(block), false
}
