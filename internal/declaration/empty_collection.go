package declaration

import (
	"strings"

	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
)

// RemoveEmptyRootCollections removes empty root placeholders extended by
// table headers in block, preserving comments and unrelated document bytes.
func RemoveEmptyRootCollections(original []byte, block string) []byte {
	roots := make(map[string]byte)
	WalkStructuralLines([]byte(block), func(_ int, trimmed string) bool {
		header, ok := tomlsyntax.ParseTableHeader(trimmed)
		if !ok || len(header.Segments) == 0 {
			return false
		}
		root := header.Segments[0]
		if header.Array && len(header.Segments) == 1 {
			roots[root] = '['
		} else if _, exists := roots[root]; !exists {
			roots[root] = '{'
		}
		return false
	})
	if len(roots) == 0 {
		return original
	}

	content := string(original)
	var changes []DocumentChange
	WalkStructuralLines(original, func(lineStart int, trimmed string) bool {
		if _, table := tomlsyntax.ParseTableHeader(trimmed); table {
			return true
		}
		keyStart := tomlsyntax.SkipHorizontalSpace(content, lineStart, len(content))
		path, equals, ok := tomlsyntax.ReadAssignmentKey(content, keyStart, len(content))
		if !ok || len(path) != 1 {
			return false
		}
		opening, wanted := roots[path[0]]
		if !wanted {
			return false
		}
		valueEnd, ok := tomlsyntax.SkipValue(content, equals+1, len(content), 0)
		if !ok {
			return false
		}
		trivia, empty := emptyCollectionTrivia(content, equals+1, valueEnd, opening)
		if !empty {
			return false
		}

		lineEnd := tomlsyntax.SkipLine(content, valueEnd)
		preserved := content[lineStart:keyStart] + string(trivia) + content[valueEnd:lineEnd]
		var replacement []byte
		for len(preserved) > 0 {
			end := tomlsyntax.SkipLine(preserved, 0)
			line := preserved[:end]
			if strings.Trim(line, " \t\r\n") != "" {
				replacement = append(replacement, line...)
			}
			preserved = preserved[end:]
		}
		changes = append(changes, NewDocumentReplacement(DocumentRange{Start: lineStart, End: lineEnd}, replacement))
		delete(roots, path[0])
		return false
	})
	if len(changes) == 0 {
		return original
	}

	updated, err := NewDocumentChangeSet(changes...).Apply(original)
	if err != nil {
		panic(err)
	}
	return updated
}

func emptyCollectionTrivia(content string, start int, end int, opening byte) ([]byte, bool) {
	valueStart := tomlsyntax.SkipSpaceAndComments(content, start, end)
	if valueStart >= end || content[valueStart] != opening {
		return nil, false
	}
	closing := byte('}')
	if opening == '[' {
		closing = ']'
	}
	valueClose := tomlsyntax.SkipSpaceAndComments(content, valueStart+1, end)
	if valueClose+1 != end || content[valueClose] != closing {
		return nil, false
	}
	return []byte(content[start:valueStart] + content[valueStart+1:valueClose]), true
}
