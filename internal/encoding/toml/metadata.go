package tomlsyntax

import "slices"

// Key retains decoded literal segments, including quoted dots and empty keys.
type Key []string

func (key Key) String() string { return formatKey(key) }

// Metadata describes the exact decoded source; it owns no unknown-field policy.
type Metadata struct {
	index     Index
	undecoded []Key
	digest    [32]byte
}

func (metadata Metadata) Keys() []Key {
	keys := make([]Key, 0, len(metadata.index.keys))
	for _, key := range metadata.index.keys {
		keys = append(keys, slices.Clone(key.path))
	}
	return keys
}

func (metadata Metadata) Undecoded() []Key {
	keys := make([]Key, 0, len(metadata.undecoded))
	for _, key := range metadata.undecoded {
		keys = append(keys, slices.Clone(key))
	}
	return keys
}

// WithUndecoded records a schema boundary's classification of original paths.
func (metadata Metadata) WithUndecoded(keys []Key) Metadata {
	metadata.undecoded = make([]Key, len(keys))
	for index, key := range keys {
		metadata.undecoded[index] = slices.Clone(key)
	}
	return metadata
}

func (metadata Metadata) IsDefined(path ...string) bool {
	for _, key := range metadata.index.keys {
		if len(key.path) >= len(path) && slices.Equal(key.path[:len(path)], path) {
			return true
		}
	}
	return false
}

// IsArrayTable distinguishes authored table arrays from ordinary array values.
func (metadata Metadata) IsArrayTable(path ...string) bool {
	for _, expression := range metadata.index.expressions {
		if expression.Kind == ArrayTable && slices.Equal(expression.Path, path) {
			return true
		}
	}
	return false
}
