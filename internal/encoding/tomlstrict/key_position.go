package tomlstrict

import (
	"context"
	"slices"

	"github.com/BurntSushi/toml"
)

// KeyPosition returns the first authored key expression's one-based line and
// rune column. Metadata must come from decoding these exact content bytes.
// Failed admission or inconsistent key observations return no position.
func KeyPosition(content []byte, metadata toml.MetaData, key toml.Key) (line, column int, ok bool) {
	keys := metadata.Keys()
	ordinal := -1
	for index, candidate := range keys {
		if slices.Equal(candidate, key) {
			ordinal = index
			break
		}
	}
	if ordinal < 0 {
		return 0, 0, false
	}

	count := 0
	offset := -1
	_, err := admitWithKeyObserver(context.Background(), content, StandardLimits(), func(start int) {
		if count == ordinal {
			offset = start
		}
		count++
	})
	if err != nil || count != len(keys) || offset < 0 {
		return 0, 0, false
	}
	line, column = sourceCoordinates(content, offset)
	return line, column, true
}
