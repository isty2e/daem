package rootedpath

import (
	"strings"
	"unicode"
)

// BindSelectedControlEntryAuthorityBounded preserves canonical selected control paths.
func BindSelectedControlEntryAuthorityBounded(root *CapturedRoot, selectedRoot, path string, maximumDepth int, budget PhysicalTraversalBudget) (*EntryAuthority, error) {
	if strings.IndexFunc(path, func(character rune) bool {
		return character != '\x00' && unicode.IsControl(character)
	}) >= 0 {
		return BindCanonicalSelectedEntryAuthorityBounded(root, selectedRoot, path, maximumDepth, budget)
	}
	return BindSelectedEntryAuthorityBounded(root, selectedRoot, path, maximumDepth, budget)
}
