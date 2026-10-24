package output

import (
	"slices"
	"sort"
	"strings"
)

// TopmostKeyPaths returns the sorted, distinct paths not contained by another
// input path. Root remains a selection value, not mutation authority.
func TopmostKeyPaths(paths []KeyPath) []KeyPath {
	keys := slices.Clone(paths)
	slices.SortFunc(keys, func(left, right KeyPath) int {
		return strings.Compare(left.pointer, right.pointer)
	})
	covered := make([]bool, len(keys))
	result := make([]KeyPath, 0, len(keys))
	for index, key := range keys {
		if covered[index] || index > 0 && key == keys[index-1] {
			continue
		}
		result = append(result, key)
		// The slash's lexicographic successor bounds this pointer's descendants.
		prefix, upper := key.pointer+"/", key.pointer+"0"
		first := sort.Search(len(keys), func(index int) bool { return keys[index].pointer >= prefix })
		last := sort.Search(len(keys), func(index int) bool { return keys[index].pointer >= upper })
		for descendant := first; descendant < last; descendant++ {
			covered[descendant] = true
		}
	}
	return result
}
