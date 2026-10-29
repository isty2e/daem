package tomlsyntax

import (
	"crypto/sha256"
	"slices"
)

// KeyPosition returns the first authored key's one-based line and rune column.
// Metadata from different content never supplies a position.
func KeyPosition(content []byte, metadata Metadata, key Key) (line, column int, ok bool) {
	if sha256.Sum256(content) != metadata.digest {
		return 0, 0, false
	}
	for _, candidate := range metadata.index.keys {
		if slices.Equal(candidate.path, key) {
			line, column = sourceCoordinates(content, candidate.offset)
			return line, column, true
		}
	}
	return 0, 0, false
}
