package configdocument

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/output"
)

// Change selects one native value, never the entire document.
type Change struct {
	path   output.KeyPath
	value  Value
	remove bool
}

func Set(path output.KeyPath, value Value) (Change, error) {
	if path.IsRoot() || value.data == nil {
		return Change{}, ErrInvalidValue
	}
	return Change{path: path, value: value}, nil
}

func Remove(path output.KeyPath) (Change, error) {
	if path.IsRoot() {
		return Change{}, ErrInvalidValue
	}
	return Change{path: path, remove: true}, nil
}

// Edit validates the whole change set before rendering. A returned document
// must match the expected native result and retain every existing comment.
// Removing a key retains empty ancestor objects; it is not formatting undo.
func Edit(ctx context.Context, document Document, changes []Change) (Document, error) {
	if ctx == nil {
		return Document{}, fmt.Errorf("configuration context is required")
	}
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	if !document.root.IsObject() || document.syntax.validate() != nil {
		return Document{}, ErrInvalidDocument
	}
	tree, err := planChanges(ctx, changes, document.limits.MaximumDepth)
	if err != nil {
		return Document{}, err
	}
	expected, err := updateTree(ctx, document.root, tree)
	if err != nil {
		return Document{}, err
	}
	if expected.Equal(document.root) {
		return document, nil
	}

	var content []byte
	if document.syntax == TOML {
		content, err = editTOML(ctx, document, expected)
	} else {
		content, err = editJSON(ctx, document, tree)
	}
	if err != nil {
		return Document{}, err
	}
	result, err := Parse(ctx, content, document.syntax, document.limits)
	if err != nil {
		return Document{}, err
	}
	if !result.root.Equal(expected) || !sameComments(document.comments, result.comments) {
		return Document{}, fmt.Errorf("%w: edited document failed preservation checks", ErrInvalidDocument)
	}
	return result, nil
}

func sameComments(before, after []string) bool {
	before, after = slices.Clone(before), slices.Clone(after)
	slices.Sort(before)
	slices.Sort(after)
	return slices.Equal(before, after)
}
