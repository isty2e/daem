package nativecodec

import (
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

type selectedChange struct {
	key    output.KeyPath
	change configdocument.Change
}

func topmostChanges(changes []selectedChange) []configdocument.Change {
	keys := make([]output.KeyPath, 0, len(changes))
	byKey := make(map[output.KeyPath]configdocument.Change, len(changes))
	for _, change := range changes {
		keys = append(keys, change.key)
		byKey[change.key] = change.change
	}
	selected := output.TopmostKeyPaths(keys)
	result := make([]configdocument.Change, 0, len(selected))
	for _, key := range selected {
		result = append(result, byKey[key])
	}
	return result
}
