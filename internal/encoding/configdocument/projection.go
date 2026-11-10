package configdocument

import (
	"context"

	"github.com/isty2e/daem/internal/output"
)

// Projection supplies a semantic image within native key areas, not a request
// to reserialize unchanged descendants of those areas.
type Projection struct {
	Document Document
	Areas    []output.KeyPath
}

// Project applies disjoint semantic images through one native edit. Unchanged
// values retain their source syntax; arrays remain atomic values.
func Project(ctx context.Context, document Document, projections []Projection) (Document, error) {
	if err := validateEdit(ctx, document); err != nil {
		return Document{}, err
	}
	var areas []output.KeyPath
	for _, projection := range projections {
		if err := ctx.Err(); err != nil {
			return Document{}, err
		}
		if len(projection.Areas) == 0 || projection.Document.syntax != document.syntax ||
			projection.Document.limits != document.limits || !projection.Document.root.IsObject() {
			return Document{}, ErrInvalidDocument
		}
		for _, area := range projection.Areas {
			if area.IsRoot() {
				return Document{}, ErrInvalidValue
			}
			if len(area.Segments()) > document.limits.MaximumDepth {
				return Document{}, ErrLimitExceeded
			}
			areas = append(areas, area)
		}
	}
	if len(output.TopmostKeyPaths(areas)) != len(areas) {
		return Document{}, ErrOverlappingChange
	}

	tree := &changeTree{}
	for _, projection := range projections {
		for _, area := range projection.Areas {
			before, beforePresent, err := document.Lookup(area)
			if err != nil {
				return Document{}, err
			}
			after, afterPresent, err := projection.Document.Lookup(area)
			if err != nil {
				return Document{}, err
			}
			branch, err := nativeDifference(ctx, before, beforePresent, after, afterPresent)
			if err != nil {
				return Document{}, err
			}
			if branch == nil {
				continue
			}
			if err := tree.attach(area.Segments(), branch); err != nil {
				return Document{}, err
			}
		}
	}
	return editTree(ctx, document, tree)
}

func nativeDifference(ctx context.Context, before Value, beforePresent bool, after Value, afterPresent bool) (*changeTree, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if beforePresent == afterPresent && (!beforePresent || before.Equal(after)) {
		return nil, nil
	}
	if !afterPresent {
		return &changeTree{exact: &valueChange{remove: true}}, nil
	}
	left, leftObject := before.data.(objectNode)
	right, rightObject := after.data.(objectNode)
	if !beforePresent || !leftObject || !rightObject {
		return &changeTree{exact: &valueChange{value: after}, containsSet: true}, nil
	}

	tree := &changeTree{children: make(map[string]*changeTree)}
	for _, member := range left {
		index, present := memberIndex(right, member.key)
		var value Value
		if present {
			value = right[index].value
		}
		branch, err := nativeDifference(ctx, member.value, true, value, present)
		if err != nil {
			return nil, err
		}
		if branch != nil {
			tree.children[member.key] = branch
			tree.containsSet = tree.containsSet || branch.containsSet
		}
	}
	for _, member := range right {
		if _, present := memberIndex(left, member.key); present {
			continue
		}
		branch, err := nativeDifference(ctx, Value{}, false, member.value, true)
		if err != nil {
			return nil, err
		}
		tree.children[member.key] = branch
		tree.containsSet = true
	}
	return tree, nil
}
