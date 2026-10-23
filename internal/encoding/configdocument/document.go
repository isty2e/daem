package configdocument

import (
	"context"
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
	if err := limits.validate(); err != nil {
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
	if err := limits.validate(); err != nil {
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
	if document.root.data == nil {
		return Value{}, false, ErrInvalidDocument
	}
	if !document.exists {
		return Value{}, false, nil
	}
	return lookup(document.root, path.Segments())
}

func lookup(value Value, keys []string) (Value, bool, error) {
	for _, key := range keys {
		members, ok := value.data.(objectNode)
		if !ok {
			return Value{}, false, ErrPathConflict
		}
		index, found := memberIndex(members, key)
		if !found {
			return Value{}, false, nil
		}
		value = members[index].value
	}
	return value, true, nil
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

// Leaves expands a fragment root. An empty root contributes nothing.
func (document Document) Leaves() ([]Entry, error) {
	if !document.root.IsObject() {
		return nil, ErrInvalidDocument
	}
	if !document.exists {
		return nil, nil
	}
	var entries []Entry
	var visit func(Value, output.KeyPath) error
	visit = func(value Value, path output.KeyPath) error {
		members, object := value.data.(objectNode)
		if !object || len(members) == 0 && !path.IsRoot() {
			entries = append(entries, Entry{Path: path, Value: value})
			return nil
		}
		for _, member := range members {
			child, err := path.Append(member.key)
			if err != nil {
				return err
			}
			if err := visit(member.value, child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(document.root, output.KeyPath{}); err != nil {
		return nil, err
	}
	return entries, nil
}
