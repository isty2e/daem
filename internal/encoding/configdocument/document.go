package configdocument

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/output"
)

// Document retains immutable native data and its original concrete syntax.
// The zero value is invalid; Absent represents a missing destination.
type Document struct {
	syntax   Syntax
	limits   Limits
	exists   bool
	content  []byte
	root     Value
	comments []string
}

// Parse copies its input and validates syntax before exposing native values.
func Parse(ctx context.Context, content []byte, syntax Syntax, limits Limits) (Document, error) {
	if ctx == nil {
		return Document{}, fmt.Errorf("configuration context is required")
	}
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	if err := syntax.validate(); err != nil {
		return Document{}, err
	}
	if err := limits.Validate(); err != nil {
		return Document{}, err
	}
	if int64(len(content)) > limits.MaximumBytes {
		return Document{}, ErrLimitExceeded
	}
	content = slices.Clone(content)
	var root Value
	var comments []string
	var err error
	if syntax == TOML {
		root, comments, err = parseTOML(ctx, content, limits)
	} else {
		root, comments, err = parseJSON(ctx, content, syntax, limits)
	}
	if err != nil {
		return Document{}, err
	}
	return Document{syntax: syntax, limits: limits, exists: true, content: content, root: root, comments: comments}, nil
}

// Absent carries the grammar and bounds needed to create a missing document.
func Absent(syntax Syntax, limits Limits) (Document, error) {
	if err := syntax.validate(); err != nil {
		return Document{}, err
	}
	if err := limits.Validate(); err != nil {
		return Document{}, err
	}
	root, err := newObject(nil)
	if err != nil {
		return Document{}, err
	}
	return Document{syntax: syntax, limits: limits, root: root}, nil
}

func (document Document) Exists() bool { return document.exists }

// Bytes returns an independent copy, not mutable access to the observation.
func (document Document) Bytes() []byte { return slices.Clone(document.content) }

// Comments returns actual lexical comments, including repeated occurrences.
func (document Document) Comments() []string { return slices.Clone(document.comments) }

// Lookup traverses object properties only. An array or scalar ancestor is a
// conflict, not an absent property that an edit may silently replace.
func (document Document) Lookup(path output.KeyPath) (Value, bool, error) {
	resolution, err := document.Resolve(path)
	if err != nil {
		return Value{}, false, err
	}
	if _, blocked := resolution.BlockedBy(); blocked {
		return Value{}, false, ErrPathConflict
	}
	value, present := resolution.Value()
	return value, present, nil
}

func memberIndex(members objectNode, key string) (int, bool) {
	return slices.BinarySearchFunc(members, key, func(member objectMember, key string) int {
		if member.key < key {
			return -1
		}
		if member.key > key {
			return 1
		}
		return 0
	})
}

// Entry identifies one atomic contribution; arrays and nested empty objects
// are values, whereas nonempty objects only supply child paths.
type Entry struct {
	Path  output.KeyPath
	Value Value
}

// Leaves expands a fragment root under a caller-owned total encoded-path byte
// allowance. Values are retained, not serialized. Refusal returns no entries;
// an empty root contributes nothing and needs no path allowance.
func (document Document) Leaves(ctx context.Context, maximumPathBytes int64) ([]Entry, error) {
	if ctx == nil {
		return nil, fmt.Errorf("configuration projection context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maximumPathBytes < 0 {
		return nil, fmt.Errorf("configuration projection path byte limit must be non-negative")
	}
	if !document.root.IsObject() {
		return nil, ErrInvalidDocument
	}
	if !document.exists {
		return nil, nil
	}
	var entries []Entry
	var segments []string
	remaining := maximumPathBytes
	var visit func(Value) error
	visit = func(value Value) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		members, _ := value.data.(objectNode)
		if value.IsAtomic() && len(segments) != 0 {
			path, err := output.NewKeyPathWithinBytes(remaining, segments...)
			if errors.Is(err, output.ErrKeyPathLimitExceeded) {
				return ErrLimitExceeded
			}
			if err != nil {
				return err
			}
			remaining -= int64(len(path.String()))
			entries = append(entries, Entry{Path: path, Value: value})
			return nil
		}
		for _, member := range members {
			segments = append(segments, member.key)
			if err := visit(member.value); err != nil {
				return err
			}
			segments = segments[:len(segments)-1]
		}
		return nil
	}
	if err := visit(document.root); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}
