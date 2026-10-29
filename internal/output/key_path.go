package output

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// KeyPath identifies native object keys using RFC 6901 JSON Pointer. Its zero
// value selects the document root, not whole-file mutation authority.
type KeyPath struct {
	pointer string
}

// ParseKeyPath admits a JSON Pointer without filesystem cleaning or URI decoding.
func ParseKeyPath(pointer string) (KeyPath, error) {
	if !utf8.ValidString(pointer) {
		return KeyPath{}, fmt.Errorf("configuration key path must be valid UTF-8")
	}
	if pointer != "" && pointer[0] != '/' {
		return KeyPath{}, fmt.Errorf("configuration key path must be empty or start with /")
	}

	for index := 0; index < len(pointer); index++ {
		if pointer[index] != '~' {
			continue
		}
		if index+1 == len(pointer) || pointer[index+1] != '0' && pointer[index+1] != '1' {
			return KeyPath{}, fmt.Errorf("configuration key path has an invalid escape at byte %d", index)
		}
		index++
	}

	return KeyPath{pointer: pointer}, nil
}

// NewKeyPath constructs a path from literal object keys, including empty keys.
func NewKeyPath(keys ...string) (KeyPath, error) {
	return NewKeyPathWithinBytes(int64(^uint(0)>>1), keys...)
}

// ErrKeyPathLimitExceeded reports a pointer refused before encoding allocation.
var ErrKeyPathLimitExceeded = errors.New("configuration key path exceeds byte limit")

// NewKeyPathWithinBytes bounds the encoded pointer, not decoded key lengths.
// A zero limit permits the root; the caller owns the resource policy.
func NewKeyPathWithinBytes(maximumBytes int64, keys ...string) (KeyPath, error) {
	if maximumBytes < 0 {
		return KeyPath{}, fmt.Errorf("configuration key path byte limit must be non-negative")
	}
	remaining := maximumBytes
	for index, key := range keys {
		if !utf8.ValidString(key) {
			return KeyPath{}, fmt.Errorf("configuration key %d must be valid UTF-8", index)
		}
		if remaining == 0 || int64(len(key)) > remaining-1 {
			return KeyPath{}, ErrKeyPathLimitExceeded
		}
		remaining -= int64(len(key)) + 1
		escapes := int64(strings.Count(key, "~") + strings.Count(key, "/"))
		if escapes > remaining {
			return KeyPath{}, ErrKeyPathLimitExceeded
		}
		remaining -= escapes
	}
	size := maximumBytes - remaining
	if size > int64(^uint(0)>>1) {
		return KeyPath{}, ErrKeyPathLimitExceeded
	}

	var pointer strings.Builder
	pointer.Grow(int(size))
	for _, key := range keys {
		pointer.WriteByte('/')
		pointer.WriteString(keyEscaper.Replace(key))
	}

	return KeyPath{pointer: pointer.String()}, nil
}

var (
	keyEscaper   = strings.NewReplacer("~", "~0", "/", "~1")
	keyUnescaper = strings.NewReplacer("~1", "/", "~0", "~")
)

// String returns the canonical pointer, not terminal-escaped display text.
func (key KeyPath) String() string { return key.pointer }

// IsRoot reports whether the path has no object-key segments.
func (key KeyPath) IsRoot() bool { return key.pointer == "" }

// Segments returns literal keys. Numeric keys do not imply array indexing.
func (key KeyPath) Segments() []string {
	if key.IsRoot() {
		return nil
	}

	segments := strings.Split(key.pointer[1:], "/")
	for index := range segments {
		segments[index] = keyUnescaper.Replace(segments[index])
	}
	return segments
}

// Append adds one literal key without interpreting dots, slashes, or whitespace.
func (key KeyPath) Append(segment string) (KeyPath, error) {
	child, err := NewKeyPath(segment)
	if err != nil {
		return KeyPath{}, err
	}
	return KeyPath{pointer: key.pointer + child.pointer}, nil
}

// Parent returns the enclosing key path. The root has no parent.
func (key KeyPath) Parent() (KeyPath, bool) {
	if key.IsRoot() {
		return KeyPath{}, false
	}
	return KeyPath{pointer: key.pointer[:strings.LastIndexByte(key.pointer, '/')]}, true
}

// Contains reports equality or key-segment ancestry, not a raw name prefix.
func (key KeyPath) Contains(other KeyPath) bool {
	return key.IsRoot() || key == other || strings.HasPrefix(other.pointer, key.pointer+"/")
}

// Overlaps reports whether either path contains the other.
func (key KeyPath) Overlaps(other KeyPath) bool {
	return key.Contains(other) || other.Contains(key)
}
