package nativecodec

import (
	"sort"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

type selectedChange struct {
	key    output.KeyPath
	change configdocument.Change
}

func topmostChanges(changes []selectedChange) []configdocument.Change {
	sort.Slice(changes, func(left, right int) bool {
		return changes[left].key.String() < changes[right].key.String()
	})
	covered := make([]bool, len(changes))
	result := make([]configdocument.Change, 0, len(changes))
	for index, change := range changes {
		if covered[index] {
			continue
		}
		result = append(result, change.change)
		// The slash's lexicographic successor bounds this pointer's descendants.
		prefix, upper := change.key.String()+"/", change.key.String()+"0"
		first := sort.Search(len(changes), func(index int) bool { return changes[index].key.String() >= prefix })
		last := sort.Search(len(changes), func(index int) bool { return changes[index].key.String() >= upper })
		for descendant := first; descendant < last; descendant++ {
			covered[descendant] = true
		}
	}
	return result
}
